// SPDX-License-Identifier: AGPL-3.0-or-later

// teha web client, proof of concept.
//
// The local copy is the source of truth for the screen. Every edit lands in
// the local state and in an outbox, the screen updates at once, and the outbox
// drains to POST /v1/sync when the network allows it. The natural language
// parse runs here, so a task appears with no round trip.

const S = {
  version: 0,
  tasks: new Map(),
  projects: new Map(),
  sections: new Map(),
  labels: new Map(),
  outbox: [],
  view: { kind: 'filter', q: 'today', title: 'Today' },
  sel: 0,
  undo: null,
  online: true,
  detail: null,
  menu: null,
  // marked holds the ids of the tasks a bulk action will touch. It is not
  // saved: a selection is about the next few seconds, and a stale selection
  // read back from storage would act on rows the user cannot remember picking.
  marked: new Set(),
  // anchor is the row a shift click measures a range from.
  anchor: 0,
  // layout is 'list' or 'board'. It is saved, because a person who arranges a
  // project as a board expects the board again after a reload.
  layout: 'list',
  // drag holds what a pointer is dragging: one task, or one section.
  drag: null,
};

import { parseQuickAdd, newId, iso } from './parse.js';

const $ = (id) => document.getElementById(id);
const uuid = () => (crypto.randomUUID ? crypto.randomUUID() : String(Math.random()).slice(2));
const todayISO = () => new Date().toISOString().slice(0, 10);

// --- local storage ----------------------------------------------------------

function save() {
  try {
    localStorage.setItem('teha', JSON.stringify({
      version: S.version,
      tasks: [...S.tasks.values()],
      projects: [...S.projects.values()],
      sections: [...S.sections.values()],
      labels: [...S.labels.values()],
      outbox: S.outbox,
      layout: S.layout,
    }));
  } catch (e) { /* a full quota must never break the UI */ }
}

function load() {
  try {
    const raw = localStorage.getItem('teha');
    if (!raw) return;
    const d = JSON.parse(raw);
    S.version = d.version || 0;
    (d.tasks || []).forEach((t) => S.tasks.set(t.id, t));
    (d.projects || []).forEach((p) => S.projects.set(p.id, p));
    (d.sections || []).forEach((x) => S.sections.set(x.id, x));
    (d.labels || []).forEach((l) => S.labels.set(l.id, l));
    S.outbox = d.outbox || [];
    S.layout = d.layout === 'board' ? 'board' : 'list';
  } catch (e) { /* start clean on bad data */ }
}

// --- sync -------------------------------------------------------------------

function queue(type, args) {
  S.outbox.push({ uuid: uuid(), type, args });
  save();
  scheduleSync();
}

let syncTimer = null;
function scheduleSync(delay = 120) {
  clearTimeout(syncTimer);
  syncTimer = setTimeout(sync, delay);
}

let syncing = false;
async function sync() {
  if (syncing) return;
  syncing = true;
  const batch = S.outbox.slice(0, 200);
  try {
    const res = await fetch('/v1/sync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ since: S.version, commands: batch }),
    });
    if (res.status === 401) { location.href = '/login'; return; }
    if (!res.ok) throw new Error('sync failed: ' + res.status);
    const d = await res.json();
    // Every command in the batch reached the server, so drop it. A retry is
    // safe anyway, because the server keys on the command uuid.
    S.outbox = S.outbox.slice(batch.length);
    applyDelta(d);
    S.online = true;
  } catch (e) {
    S.online = false;
  } finally {
    syncing = false;
    save();
    render();
    if (S.outbox.length) scheduleSync(2000);
  }
}

function applyDelta(d) {
  S.version = d.version || S.version;
  (d.projects || []).forEach((p) => p.deleted_at ? S.projects.delete(p.id) : S.projects.set(p.id, p));
  (d.sections || []).forEach((x) => x.deleted_at ? S.sections.delete(x.id) : S.sections.set(x.id, x));
  (d.labels || []).forEach((l) => l.deleted_at ? S.labels.delete(l.id) : S.labels.set(l.id, l));
  (d.tasks || []).forEach((t) => t.deleted_at ? S.tasks.delete(t.id) : S.tasks.set(t.id, t));
}

function listenEvents() {
  const es = new EventSource('/v1/events');
  es.addEventListener('version', (e) => {
    if (Number(e.data) > S.version) sync();
  });
  es.onerror = () => { /* EventSource retries on its own */ };
}

// --- actions ----------------------------------------------------------------

function addFromText(text) {
  const p = parseQuickAdd(text);
  if (!p.title) return null;
  const id = newId('t');
  const project = p.project ? findProject(p.project) : null;
  const t = {
    id,
    project_id: project ? project.id : 'inbox',
    order_key: 'm',
    title: p.title,
    description: '',
    priority: p.priority || 4,
    due_date: p.due || undefined,
    due_time: p.time || undefined,
    rrule: p.rrule || undefined,
    state: 'open',
    labels: p.labels,
    v: 0,
  };
  S.tasks.set(id, t);
  queue('task_add', {
    id, title: p.title,
    project_id: t.project_id,
    due_date: p.due || undefined,
    due_time: p.time || undefined,
    priority: p.priority || undefined,
    rrule: p.rrule || undefined,
    labels: p.labels.length ? p.labels : undefined,
  });
  return t;
}

// findProject resolves a name the way the server does: an exact match first,
// then a unique prefix match, so "#Trip" finds "Trip to Setomaa". An ambiguous
// prefix returns nothing, so the hint can say the name is not clear yet.
function findProject(name) {
  const low = name.toLowerCase();
  const all = [...S.projects.values()];
  const exact = all.find((p) => p.name.toLowerCase() === low);
  if (exact) return exact;
  const starts = all.filter((p) => p.name.toLowerCase().startsWith(low));
  return starts.length === 1 ? starts[0] : null;
}

function complete(t) {
  if (!t) return;
  const before = { ...t };
  if (t.rrule) {
    // The server advances the date. Grey it out until the next sync answers.
    t.pending = true;
  } else {
    t.state = 'done';
    t.completed_at = new Date().toISOString();
  }
  queue('task_complete', { id: t.id });
  toast(t.rrule ? 'Moved to its next date' : 'Completed', () => {
    S.tasks.set(before.id, before);
    queue('task_uncomplete', { id: before.id });
    render();
  });
  render();
}

function setPriority(t, p) {
  if (!t) return;
  t.priority = p;
  queue('task_update', { id: t.id, priority: p });
  render();
}

function schedule(t, days) {
  if (!t) return;
  const d = new Date();
  d.setDate(d.getDate() + days);
  t.due_date = iso(d);
  queue('task_update', { id: t.id, due_date: t.due_date });
  render();
}

// --- days -------------------------------------------------------------------

const plusDays = (n) => { const d = new Date(); d.setDate(d.getDate() + n); return iso(d); };

// nextDow returns the coming day of the week, and today when today is that
// day. 0 is Sunday, so Saturday is 6 and Monday is 1.
function nextDow(dow) {
  const d = new Date();
  d.setDate(d.getDate() + ((dow - d.getDay() + 7) % 7));
  return iso(d);
}

// --- bulk reschedule --------------------------------------------------------

// overdueRows returns the overdue tasks of the list on screen. The screen, not
// the whole account: a button in a project view must not touch another project.
function overdueRows() {
  const today = todayISO();
  return visible().filter((t) => t.due_date && t.due_date < today);
}

// rescheduleAll moves a whole list of tasks to one day.
//
// One task_update per task, not one command that says "everything overdue".
// A command names an id and a date, so a replay next week does the same thing.
// A command with a query in it would mean something different every time the
// server ran it, and the outbox exists to be replayed.
//
// date is an ISO day, or null to take the date away.
function rescheduleAll(tasks, date) {
  if (!tasks.length) return;
  const before = tasks.map((t) => ({ id: t.id, due: t.due_date || null, time: t.due_time || null }));
  tasks.forEach((t) => setDue(t, date));
  // Every bulk action drops the selection, this one included. A set that
  // survived its own action was still marked for the next one, so a second
  // action hit tasks the user believed they had already dealt with.
  S.marked.clear();
  const n = tasks.length;
  const what = n === 1 ? '1 task' : n + ' tasks';
  toast(date ? `Moved ${what} to ${whenWord(date)}` : `Took the date off ${what}`, () => {
    before.forEach((b) => {
      const t = S.tasks.get(b.id);
      if (t) setDue(t, b.due, b.time);
    });
    render();
  });
  render();
}

// whenWord names the day inside a sentence. "today" reads as English there,
// and a lowercased locale date does not, so only the two words are lowered.
function whenWord(date) {
  const label = dueLabel(date);
  return (label === 'Today' || label === 'Tomorrow') ? label.toLowerCase() : label;
}

// setDue writes one task's day. time is only for an undo, which has to put
// back what "No date" took away.
//
// The time goes with the date. The server lets a row hold a time and no day,
// and such a row is one no view can print, so "No date" clears both.
function setDue(t, date, time) {
  if (!date) {
    delete t.due_date;
    delete t.due_time;
    queue('task_update', { id: t.id, clear: ['due_date', 'due_time'] });
    return;
  }
  t.due_date = date;
  const args = { id: t.id, due_date: date };
  if (time) { t.due_time = time; args.due_time = time; }
  queue('task_update', args);
}

// dayWord prints one day for a menu hint.
function dayWord(d) {
  return new Date(d + 'T00:00').toLocaleDateString(undefined,
    { weekday: 'short', day: 'numeric', month: 'short' });
}

// popup opens one menu under an anchor element.
//
// items are { label, hint, run }. footer is optional raw HTML for a row the
// caller wires itself, such as the date field on the reschedule menu. The
// return value is the menu element, so the caller can reach that row.
function popup(anchor, items, footer) {
  closeMenu();
  const back = document.createElement('div');
  back.className = 'menu-back';
  const el = document.createElement('div');
  el.className = 'menu';
  el.innerHTML = items.map((it, i) => `<button data-i="${i}"><span>${esc(it.label)}</span>`
    + `<span class="w">${esc(it.hint || '')}</span></button>`).join('') + (footer || '');

  [...el.querySelectorAll('button')].forEach((b) => {
    b.onclick = () => { closeMenu(); items[Number(b.dataset.i)].run(); };
  });
  back.onclick = closeMenu;
  document.body.append(back, el);

  // The menu is placed after it is in the document, because the height is only
  // known once the browser has laid it out. It opens below the anchor when
  // there is room and above it when there is not: the bulk bar sits at the
  // bottom of the window, and a menu that opened downwards from there ran off
  // the screen and hid its own last choices.
  const r = anchor.getBoundingClientRect();
  const h = el.offsetHeight;
  const below = r.bottom + 6;
  const top = below + h <= window.innerHeight - 8 ? below : Math.max(8, r.top - 6 - h);
  el.style.top = Math.round(top) + 'px';
  el.style.left = Math.round(Math.max(8, Math.min(r.left, window.innerWidth - 248))) + 'px';
  S.menu = { back, el };
  const first = el.querySelector('button');
  if (first) first.focus();
  return el;
}

function rescheduleMenu(anchor, tasks) {
  const opts = [
    ['Today', plusDays(0)],
    ['Tomorrow', plusDays(1)],
    ['This weekend', nextDow(6)],
    ['Next week', nextDow(1) === todayISO() ? plusDays(7) : nextDow(1)],
    ['No date', null],
  ];
  const el = popup(
    anchor,
    opts.map(([label, d]) => ({ label, hint: d ? dayWord(d) : '', run: () => rescheduleAll(tasks, d) })),
    `<label class="pick"><span>Pick a day</span><input type="date" value="${todayISO()}"></label>`,
  );
  el.querySelector('.pick input').onchange = (e) => {
    // The value is read before the menu closes. closeMenu detaches the field,
    // and a detached field is not a thing to read state from.
    const day = e.target.value;
    if (day) { closeMenu(); rescheduleAll(tasks, day); }
  };
}

function closeMenu() {
  if (!S.menu) return;
  S.menu.back.remove();
  S.menu.el.remove();
  S.menu = null;
}

// --- many tasks at once -----------------------------------------------------
// D-008: a bulk action is one command per task, never one command that carries
// a query. The outbox is a replay log, so a command that names an id and a
// value does the same thing whenever it runs. "Everything overdue" would mean
// a different set of tasks every time the server read it.

// markedRows returns the marked tasks that are on screen.
//
// On screen, not in the account. A mark survives a change of view, and acting
// on a row the user can no longer see is a change nobody can check.
function markedRows() {
  return visible().filter((t) => S.marked.has(t.id));
}

function toggleMark(t) {
  if (!t) return;
  if (S.marked.has(t.id)) S.marked.delete(t.id);
  else S.marked.add(t.id);
  render();
}

// markRange marks every row between the anchor and one index, so a long run
// takes two clicks instead of twenty.
function markRange(to) {
  const rows = visible();
  const from = Math.min(Math.max(S.anchor, 0), rows.length - 1);
  const [lo, hi] = from <= to ? [from, to] : [to, from];
  for (let i = lo; i <= hi; i++) S.marked.add(rows[i].id);
  render();
}

function markAll() {
  visible().forEach((t) => S.marked.add(t.id));
  render();
}

function clearMarks() {
  if (!S.marked.size) return;
  S.marked.clear();
  render();
}

// countWord keeps "1 task" and "3 tasks" in one place.
function countWord(n) {
  return n === 1 ? '1 task' : n + ' tasks';
}

// bulkField writes one field on every task, with one undo for the whole set.
//
// say builds the sentence from the count, rather than taking a finished
// sentence. "Moved 3 tasks to #Home" and "Set p1 on 3 tasks" put the count in
// different places, and only the caller knows which reads as English.
function bulkField(tasks, field, value, say) {
  if (!tasks.length) return;
  const before = tasks.map((t) => ({ id: t.id, was: t[field] }));
  tasks.forEach((t) => { t[field] = value; queue('task_update', { id: t.id, [field]: value }); });
  S.marked.clear();
  toast(say(countWord(tasks.length)), () => {
    before.forEach((b) => {
      const t = S.tasks.get(b.id);
      if (!t) return;
      t[field] = b.was;
      queue('task_update', { id: b.id, [field]: b.was });
    });
    render();
  });
  render();
}

function bulkPriority(tasks, p) {
  bulkField(tasks, 'priority', p, (what) => `Set p${p} on ${what}`);
}

function bulkProject(tasks, projectId) {
  const p = S.projects.get(projectId);
  const where = p && p.id !== 'inbox' ? '#' + p.name : 'the inbox';
  bulkField(tasks, 'project_id', projectId, (what) => `Moved ${what} to ${where}`);
}

function bulkComplete(tasks) {
  if (!tasks.length) return;
  const before = tasks.map((t) => ({ ...t }));
  tasks.forEach((t) => {
    // A repeating task moves to its next date instead of closing, and only the
    // server knows that date. Grey it out until the next sync answers.
    if (t.rrule) t.pending = true;
    else { t.state = 'done'; t.completed_at = new Date().toISOString(); }
    queue('task_complete', { id: t.id });
  });
  S.marked.clear();
  toast(`Completed ${countWord(tasks.length)}`, () => {
    before.forEach((b) => { S.tasks.set(b.id, b); queue('task_uncomplete', { id: b.id }); });
    render();
  });
  render();
}

function bulkDelete(tasks) {
  if (!tasks.length) return;
  const before = tasks.map((t) => ({ ...t }));
  tasks.forEach((t) => { S.tasks.delete(t.id); queue('task_delete', { id: t.id }); });
  S.marked.clear();
  toast(`Deleted ${countWord(tasks.length)}`, () => {
    before.forEach((b) => { S.tasks.set(b.id, b); queue('task_restore', { id: b.id }); });
    render();
  });
  render();
}

// projectMenu moves a set of tasks. The inbox reads as "Inbox" and not as a
// project name, because that is what every other screen calls it.
function projectMenu(anchorEl, tasks) {
  const items = [...S.projects.values()].map((p) => ({
    label: p.id === 'inbox' ? 'Inbox' : p.name,
    run: () => bulkProject(tasks, p.id),
  }));
  popup(anchorEl, items);
}

function priorityMenu(anchorEl, tasks) {
  popup(anchorEl, [1, 2, 3, 4].map((n) => ({
    label: 'p' + n,
    hint: ['urgent', 'high', 'medium', 'none'][n - 1],
    run: () => bulkPriority(tasks, n),
  })));
}

function removeTask(t) {
  if (!t) return;
  const before = { ...t };
  S.tasks.delete(t.id);
  queue('task_delete', { id: t.id });
  toast('Deleted', () => {
    S.tasks.set(before.id, before);
    queue('task_restore', { id: before.id });
    render();
  });
  render();
}

let toastTimer = null;
function toast(text, undo) {
  S.undo = { text, undo };
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { S.undo = null; render(); }, 6000);
}

// --- views ------------------------------------------------------------------

const VIEWS = [
  { q: 'today', title: 'Today' },
  { q: 'overdue', title: 'Overdue' },
  { q: 'week', title: 'Next 7 days' },
  { q: '#inbox', title: 'Inbox' },
  { q: 'no date', title: 'No date' },
  { q: 'p1', title: 'Priority 1' },
];

// matches evaluates the same filter grammar as the server, over local rows.
// The subset here covers every built-in view and the common hand-written query.
function matches(t, q) {
  if (t.state !== 'open' && !/done|completed/.test(q)) return false;
  const today = todayISO();
  const or = q.split(/\s*[|,]\s*/);
  if (or.length > 1) return or.some((part) => matches(t, part));
  return q.split(/\s*&\s*/).every((raw) => {
    let term = raw.trim().toLowerCase();
    let neg = false;
    while (term.startsWith('!')) { neg = !neg; term = term.slice(1).trim(); }
    const r = termMatch(t, term, today);
    return neg ? !r : r;
  });
}

function termMatch(t, term, today) {
  if (!term) return true;
  if (term === 'today' || term === 'tod') return !!t.due_date && t.due_date <= today;
  if (term === 'overdue' || term === 'od') return !!t.due_date && t.due_date < today;
  if (term === 'tomorrow') { const d = new Date(); d.setDate(d.getDate() + 1); return t.due_date === iso(d); }
  if (term === 'week' || term === 'next 7 days') { const d = new Date(); d.setDate(d.getDate() + 7); return !!t.due_date && t.due_date <= iso(d); }
  if (term === 'no date') return !t.due_date;
  if (term === 'no section' || term === 'no sections') return !t.section_id;
  if (term === 'recurring') return !!t.rrule;
  if (term === 'subtask') return !!t.parent_id;
  if (term === 'done' || term === 'completed') return t.state === 'done';
  if (term === 'no priority') return (t.priority || 4) === 4;
  if (/^p[1-4]$/.test(term)) return (t.priority || 4) === Number(term[1]);
  if (term.startsWith('#')) {
    const name = term.slice(1);
    if (name === 'inbox') return t.project_id === 'inbox';
    const p = findProject(name);
    return p ? t.project_id === p.id : false;
  }
  if (term.startsWith('/')) {
    return sectionName(t.section_id).toLowerCase() === term.slice(1);
  }
  if (term.startsWith('%') || term.startsWith('@')) {
    return (t.labels || []).some((l) => l.toLowerCase() === term.slice(1));
  }
  if (term.startsWith('search:')) {
    const s = term.slice(7).trim();
    return (t.title + ' ' + (t.description || '')).toLowerCase().includes(s);
  }
  return (t.title || '').toLowerCase().includes(term);
}

function visible() {
  // The board returns the same rows, column after column. One flat list keeps
  // the cursor, the marks and every action key working in both layouts.
  if (boardOn()) return boardColumns().flatMap((c) => c.tasks);
  const q = S.view.q;
  const out = [...S.tasks.values()].filter((t) => matches(t, q));
  out.sort((a, b) => {
    const ad = a.due_date || '9999', bd = b.due_date || '9999';
    if (ad !== bd) return ad < bd ? -1 : 1;
    if ((a.priority || 4) !== (b.priority || 4)) return (a.priority || 4) - (b.priority || 4);
    return (a.title || '').localeCompare(b.title || '');
  });
  return out;
}

// --- render -----------------------------------------------------------------

function render() {
  const rows = visible();
  if (S.sel >= rows.length) S.sel = Math.max(0, rows.length - 1);

  // sidebar
  const nav = $('nav');
  const projects = [...S.projects.values()].filter((p) => p.id !== 'inbox');
  nav.innerHTML = VIEWS.map((v) => navLink(v.q, v.title)).join('')
    + (projects.length ? '<div class="grp">Projects</div>' : '')
    + projects.map((p) => navLink('#' + p.name, p.name)).join('');
  [...nav.querySelectorAll('a')].forEach((a) => {
    a.onclick = () => { S.view = { q: a.dataset.q, title: a.dataset.title }; S.sel = 0; render(); };
  });

  $('title').textContent = S.view.title;
  const lay = $('layout');
  lay.hidden = !currentProject();
  lay.textContent = S.layout === 'board' ? 'List' : 'Board';
  $('count').textContent = rows.length ? rows.length + (rows.length === 1 ? ' task' : ' tasks') : '';
  const pend = S.outbox.length;
  $('status').textContent = S.online ? (pend ? pend + ' to send' : 'v' + S.version) : 'offline · ' + pend + ' queued';

  const list = $('list');
  if (boardOn()) {
    renderBoard(list);
  } else if (!rows.length) {
    list.innerHTML = '<div class="empty">Nothing here. Type in the box above to add the first task.</div>';
  } else {
    // Sections make the day readable: overdue work sits above today, and a
    // multi-day view breaks into days instead of one long run.
    let lastGroup = null;
    list.innerHTML = rows.map((t, i) => {
      const g = groupOf(t);
      const head = g === lastGroup ? '' : groupHead(g, rows);
      lastGroup = g;
      return head + rowHTML(t, i);
    }).join('');
    const rb = list.querySelector('.resched');
    if (rb) rb.onclick = (e) => { e.stopPropagation(); rescheduleMenu(rb, overdueRows()); };
  }

  // One wiring for both layouts. The rows stand in the same order as rows,
  // whether they came from the day groups or from the board columns.
  [...list.querySelectorAll('.row')].forEach((el, i) => {
    el.querySelector('.box').onclick = (e) => { e.stopPropagation(); complete(rows[i]); };
    // The circle completes the task. Anywhere else in the row opens the
    // detail, because that is what a pointer user expects. A held modifier
    // means the user is picking a set instead, which is the same gesture
    // every file manager and mail client uses.
    el.querySelector('.body').onclick = (ev) => {
      ev.stopPropagation();
      S.sel = i;
      if (ev.metaKey || ev.ctrlKey) { S.anchor = i; toggleMark(rows[i]); return; }
      if (ev.shiftKey) { markRange(i); return; }
      S.anchor = i;
      openDetail(rows[i]);
    };
    el.onclick = () => { S.sel = i; render(); };
  });
  const sel = list.querySelector('.row.sel');
  if (sel) sel.scrollIntoView({ block: 'nearest' });

  const bar = document.querySelector('.bulk');
  if (bar) bar.remove();
  const picked = markedRows();
  if (picked.length) {
    const el = document.createElement('div');
    el.className = 'bulk';
    el.innerHTML = `<span class="n">${picked.length} selected</span>`
      + `<button data-a="sched">Schedule</button><button data-a="prio">Priority</button>`
      + `<button data-a="proj">Move</button><button data-a="done">Complete</button>`
      + `<button data-a="del">Delete</button><button data-a="clear" class="ghost">Clear</button>`;
    const act = {
      sched: (b) => rescheduleMenu(b, picked),
      prio: (b) => priorityMenu(b, picked),
      proj: (b) => projectMenu(b, picked),
      done: () => bulkComplete(picked),
      del: () => bulkDelete(picked),
      clear: () => clearMarks(),
    };
    [...el.querySelectorAll('button')].forEach((b) => {
      b.onclick = (ev) => { ev.stopPropagation(); act[b.dataset.a](b); };
    });
    document.body.appendChild(el);
  }

  const old = document.querySelector('.toast');
  if (old) old.remove();
  if (S.undo) {
    const el = document.createElement('div');
    el.className = 'toast';
    el.innerHTML = `<span>${esc(S.undo.text)}</span><button>Undo</button>`;
    el.querySelector('button').onclick = () => { const u = S.undo.undo; S.undo = null; u(); };
    document.body.appendChild(el);
  }
  save();
}

function navLink(q, title) {
  const n = [...S.tasks.values()].filter((t) => matches(t, q)).length;
  const on = S.view.q === q ? ' class="on"' : '';
  return `<a${on} data-q="${esc(q)}" data-title="${esc(title)}"><span>${esc(title)}</span><span class="c">${n || ''}</span></a>`;
}

// groupOf names the section a task belongs to. The names match what a person
// says out loud: overdue, today, tomorrow, then the weekday and the date.
function groupOf(t) {
  if (!t.due_date) return 'No date';
  const today = todayISO();
  if (t.due_date < today) return 'Overdue';
  if (t.due_date === today) return 'Today';
  const d1 = new Date(); d1.setDate(d1.getDate() + 1);
  if (t.due_date === iso(d1)) return 'Tomorrow';
  return new Date(t.due_date + 'T00:00').toLocaleDateString(undefined,
    { weekday: 'long', day: 'numeric', month: 'long' });
}

// groupHead prints one section title. The overdue section also carries the
// button that moves the whole section, because the morning after a busy week
// the alternative is a dozen separate edits.
function groupHead(g, rows) {
  if (g !== 'Overdue') return `<div class="grp-head">${esc(g)}</div>`;
  const n = rows.filter((t) => groupOf(t) === 'Overdue').length;
  return `<div class="grp-head"><span>${esc(g)}</span><span class="c">${n}</span>`
    + `<button class="resched">Reschedule</button></div>`;
}

function rowHTML(t, i) {
  const today = todayISO();
  const cls = ['row'];
  if (i === S.sel) cls.push('sel');
  if (S.marked.has(t.id)) cls.push('mk');
  if (t.state === 'done') cls.push('done');
  if ((t.priority || 4) < 4) cls.push('p' + t.priority);
  const meta = [];
  if (t.due_date) {
    const k = t.due_date < today ? 'od' : (t.due_date === today ? 'today' : '');
    meta.push(`<span class="due ${k}">${esc(dueLabel(t.due_date, t.due_time))}</span>`);
  }
  if (t.rrule) meta.push('<span>repeats</span>');
  (t.labels || []).forEach((l) => meta.push(`<span class="lb">@${esc(l)}</span>`));
  const p = S.projects.get(t.project_id);
  if (p && p.id !== 'inbox') meta.push(`<span class="pr">#${esc(p.name)}</span>`);
  return `<div class="${cls.join(' ')}"><button class="box" aria-label="Complete"></button>
    <div class="body"><div class="t">${esc(t.title)}</div>
    ${meta.length ? `<div class="meta">${meta.join('')}</div>` : ''}</div></div>`;
}

function dueLabel(date, time) {
  const today = todayISO();
  const d = new Date(date + 'T00:00');
  const t1 = new Date(); t1.setDate(t1.getDate() + 1);
  let label;
  if (date === today) label = 'Today';
  else if (date === iso(t1)) label = 'Tomorrow';
  else if (date < today) label = date.slice(5).replace('-', '.');
  else label = d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
  return time ? label + ' ' + time : label;
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// --- task detail ------------------------------------------------------------
// One panel edits every field a task has. Each change goes straight into the
// local state and the outbox, so the panel never waits for the server.

function openDetail(t) {
  if (!t) return;
  closeDetail();
  const kids = [...S.tasks.values()].filter((c) => c.parent_id === t.id && c.state === 'open');
  const projects = [...S.projects.values()];
  const el = document.createElement('div');
  el.className = 'sheet';
  el.innerHTML = `<div class="card" role="dialog" aria-label="Task detail">
    <input class="d-title" value="${esc(t.title)}" aria-label="Title">
    <textarea class="d-desc" rows="3" placeholder="Notes" aria-label="Notes">${esc(t.description || '')}</textarea>
    <div class="d-grid">
      <label>Due<input type="date" class="d-due" value="${t.due_date || ''}"></label>
      <label>Time<input type="time" class="d-time" value="${t.due_time || ''}"></label>
      <label>Starts<input type="date" class="d-start" value="${t.start_date || ''}"></label>
      <label>Deadline<input type="date" class="d-deadline" value="${t.deadline || ''}"></label>
    </div>
    <div class="d-row"><span class="d-lab">Priority</span>
      <div class="d-prio">${[1, 2, 3, 4].map((n) =>
        `<button data-p="${n}" class="${(t.priority || 4) === n ? 'on' : ''}">p${n}</button>`).join('')}</div>
    </div>
    <div class="d-row"><span class="d-lab">Project</span>
      <select class="d-project">${projects.map((p) =>
        `<option value="${esc(p.id)}"${p.id === t.project_id ? ' selected' : ''}>${esc(p.name)}</option>`).join('')}</select>
    </div>
    <div class="d-row"><span class="d-lab">Labels</span>
      <input class="d-labels" value="${esc((t.labels || []).join(', '))}" placeholder="store, call"></div>
    <div class="d-row"><span class="d-lab">Repeats</span>
      <input class="d-rrule" value="${esc(t.rrule || '')}" placeholder="FREQ=WEEKLY;BYDAY=MO"></div>
    <div class="d-subs">
      <span class="d-lab">Sub-tasks</span>
      ${kids.map((k) => `<div class="d-sub">${esc(k.title)}</div>`).join('')}
      <input class="d-newsub" placeholder="Add a sub-task, then press Enter">
    </div>
    <div class="d-actions">
      <button class="d-del">Delete</button>
      <span class="d-hint">Changes save as you type. Escape closes.</span>
      <button class="d-close">Done</button>
    </div>
  </div>`;
  document.body.appendChild(el);

  const q = (sel) => el.querySelector(sel);
  const patch = (fields, clear) => {
    Object.assign(t, fields);
    const args = { id: t.id, ...fields };
    if (clear && clear.length) args.clear = clear;
    queue('task_update', args);
    render();
  };
  const dateField = (sel, field) => {
    q(sel).onchange = () => {
      const v = q(sel).value;
      if (v) patch({ [field]: v });
      else { delete t[field]; patch({}, [field]); }
    };
  };

  q('.d-title').onchange = () => { const v = q('.d-title').value.trim(); if (v) patch({ title: v }); };
  q('.d-desc').onchange = () => patch({ description: q('.d-desc').value });
  dateField('.d-due', 'due_date');
  dateField('.d-time', 'due_time');
  dateField('.d-start', 'start_date');
  dateField('.d-deadline', 'deadline');
  q('.d-project').onchange = () => patch({ project_id: q('.d-project').value });
  q('.d-labels').onchange = () => {
    const labels = q('.d-labels').value.split(',').map((x) => x.trim()).filter(Boolean);
    t.labels = labels;
    queue('task_update', { id: t.id, labels });
    render();
  };
  q('.d-rrule').onchange = () => {
    const v = q('.d-rrule').value.trim();
    if (v) patch({ rrule: v });
    else { delete t.rrule; patch({}, ['rrule']); }
  };
  [...el.querySelectorAll('.d-prio button')].forEach((b) => {
    b.onclick = () => {
      patch({ priority: Number(b.dataset.p) });
      [...el.querySelectorAll('.d-prio button')].forEach((x) => x.classList.toggle('on', x === b));
    };
  });
  q('.d-newsub').onkeydown = (e) => {
    if (e.key !== 'Enter') return;
    const title = q('.d-newsub').value.trim();
    if (!title) return;
    const id = newId('t');
    S.tasks.set(id, { id, project_id: t.project_id, parent_id: t.id, order_key: 'm',
      title, priority: 4, state: 'open', labels: [], v: 0 });
    queue('task_add', { id, title, project_id: t.project_id, parent_id: t.id });
    q('.d-newsub').value = '';
    closeDetail();
    openDetail(t);
  };
  q('.d-del').onclick = () => { closeDetail(); removeTask(t); };
  q('.d-close').onclick = closeDetail;
  el.onclick = (e) => { if (e.target === el) closeDetail(); };
  q('.d-title').focus();
  S.detail = t.id;
}

function closeDetail() {
  const el = document.querySelector('.sheet');
  if (el) el.remove();
  S.detail = null;
}

// --- board layout -----------------------------------------------------------
// A project view draws its sections as columns. The toggle sits in the header
// and on the `b` key, and it survives a reload because it is saved with the
// rest of the local state.
//
// One rule keeps the rest of the app unchanged: visible() returns the same flat
// list of tasks, in board order, column after column. The cursor, the marks,
// every bulk action and every key therefore work on the board with no second
// code path.

function boardOn() {
  return S.layout === 'board' && !!currentProject();
}

// currentProject returns the project of the view, or null when the view is a
// filter. #inbox counts: the inbox is a project and can hold sections.
function currentProject() {
  const q = (S.view.q || '').trim();
  if (!q.startsWith('#') || q.startsWith('##')) return null;
  const name = q.slice(1);
  if (name.toLowerCase() === 'inbox') return S.projects.get('inbox') || null;
  return findProject(name);
}

function projectSections(projectId) {
  return [...S.sections.values()]
    .filter((s) => s.project_id === projectId)
    .sort((a, b) => (a.order_key || '').localeCompare(b.order_key || '')
      || (a.name || '').localeCompare(b.name || ''));
}

function sectionName(id) {
  const s = id ? S.sections.get(id) : null;
  return s ? s.name : '';
}

// boardColumns builds the columns of the current project view. The first column
// holds the tasks with no section, because a task must never be filed before it
// can be seen.
function boardColumns() {
  const p = currentProject();
  if (!p) return [];
  const cols = [{ id: null, name: 'No section', tasks: [] }]
    .concat(projectSections(p.id).map((s) => ({ id: s.id, name: s.name, tasks: [] })));
  const at = new Map(cols.map((c, i) => [c.id, i]));
  [...S.tasks.values()].forEach((t) => {
    if (t.project_id !== p.id || !matches(t, S.view.q)) return;
    // A task that names a section this project does not have goes into the
    // first column, so a stale id can never hide a task.
    const key = t.section_id || null;
    cols[at.has(key) ? at.get(key) : 0].tasks.push(t);
  });
  cols.forEach((c) => c.tasks.sort(boardOrder));
  return cols;
}

// boardOrder sorts a column by the manual order key first. A board is where a
// person arranges work by hand, so the key wins over the date here. That is the
// opposite of the list layout, and it is the point of the layout.
function boardOrder(a, b) {
  const ak = a.order_key || 'm', bk = b.order_key || 'm';
  if (ak !== bk) return ak < bk ? -1 : 1;
  const ad = a.due_date || '9999', bd = b.due_date || '9999';
  if (ad !== bd) return ad < bd ? -1 : 1;
  return (a.title || '').localeCompare(b.title || '');
}

// orderKeyAt spaces the keys ten apart, in the fixed-width text form that the
// importer also writes. A drop renumbers the whole column instead of finding a
// key between two neighbours: a column holds a few rows, and one command per
// row is the shape that D-008 asks for.
function orderKeyAt(i) {
  return 'm' + String((i + 1) * 10).padStart(6, '0');
}

// columnOf finds the column and the row of one flat index.
function columnOf(cols, sel) {
  let seen = 0;
  for (let ci = 0; ci < cols.length; ci++) {
    const n = cols[ci].tasks.length;
    if (sel < seen + n) return { ci, row: sel - seen };
    seen += n;
  }
  return { ci: 0, row: 0 };
}

// flatIndex turns a column and a row back into an index of visible().
function flatIndex(cols, ci, row) {
  let seen = 0;
  for (let i = 0; i < ci; i++) seen += cols[i].tasks.length;
  return seen + row;
}

function toggleLayout() {
  if (S.layout !== 'board' && !currentProject()) {
    $('hint').textContent = 'The board layout needs a project view. Pick a project in the sidebar.';
    return;
  }
  S.layout = S.layout === 'board' ? 'list' : 'board';
  S.sel = 0;
  save();
  render();
}

// --- the board, drawn -------------------------------------------------------

function renderBoard(list) {
  const p = currentProject();
  const cols = boardColumns();
  let i = 0;
  const html = cols.map((c, ci) => {
    const rows = c.tasks.map((t) => rowHTML(t, i++)).join('')
      || '<div class="col-empty">Nothing here yet</div>';
    const head = c.id
      ? `<div class="col-head" draggable="true"><button class="col-name">${esc(c.name)}</button>`
      : `<div class="col-head"><span class="col-name plain">${esc(c.name)}</span>`;
    return `<div class="col" data-col="${ci}">${head}`
      + `<span class="c">${c.tasks.length || ''}</span></div>`
      + `<div class="col-body">${rows}</div></div>`;
  }).join('');
  list.innerHTML = `<div class="board">${html}`
    + '<div class="col add"><input class="col-new" placeholder="Add a section"'
    + ' aria-label="Add a section" autocomplete="off"></div></div>';
  wireBoard(list, cols, p);
}

// wireBoard hangs one drag implementation on both draggable things: a task and
// a column head. S.drag holds what is held, so the drop handler needs one test
// to tell the two apart.
function wireBoard(list, cols, p) {
  const flat = cols.flatMap((c) => c.tasks);
  const newSection = list.querySelector('.col-new');
  if (newSection) {
    newSection.onkeydown = (e) => {
      if (e.key !== 'Enter') return;
      addSection(p, newSection.value);
      // render() rebuilt the board, so the field to focus is the new one.
      const next = document.querySelector('.col-new');
      if (next) next.focus();
    };
  }
  [...list.querySelectorAll('.row')].forEach((el, i) => {
    el.draggable = true;
    el.ondragstart = (e) => {
      S.drag = { kind: 'task', id: flat[i].id };
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', flat[i].id);
    };
    el.ondragend = () => { S.drag = null; };
  });
  [...list.querySelectorAll('.col')].forEach((el) => {
    const ci = Number(el.dataset.col);
    const col = cols[ci];
    if (!col) return; // the last column is the add field
    const name = el.querySelector('button.col-name');
    if (name) name.onclick = () => columnMenu(name, cols, ci);
    const head = el.querySelector('.col-head');
    if (head && col.id) {
      head.ondragstart = (e) => {
        S.drag = { kind: 'section', id: col.id };
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', col.id);
      };
      head.ondragend = () => { S.drag = null; };
    }
    el.ondragover = (e) => {
      if (!S.drag) return;
      if (S.drag.kind === 'section' && !col.id) return; // no column sits left of the first
      e.preventDefault();
      el.classList.add('over');
    };
    el.ondragleave = () => el.classList.remove('over');
    el.ondrop = (e) => {
      e.preventDefault();
      el.classList.remove('over');
      const d = S.drag;
      S.drag = null;
      if (!d) return;
      if (d.kind === 'task') moveTaskTo(S.tasks.get(d.id), col.id, dropIndex(el, e.clientY));
      else reorderSection(d.id, ci - 1);
    };
  });
}

// dropIndex reads the position of the pointer against the rows of one column,
// so a drop lands where the cursor is and not always at the end.
function dropIndex(colEl, y) {
  const rows = [...colEl.querySelectorAll('.row')];
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (y < r.top + r.height / 2) return i;
  }
  return rows.length;
}

// columnMenu holds every action on one column. The head is a button, so a Tab
// and an Enter reach this menu with no pointer.
function columnMenu(anchorEl, cols, ci) {
  const col = cols[ci];
  const sec = S.sections.get(col.id);
  if (!sec) return;
  popup(anchorEl, [
    { label: 'Rename', run: () => renameSection(anchorEl, sec) },
    { label: 'Move left', hint: '<', run: () => reorderSection(sec.id, ci - 2) },
    { label: 'Move right', hint: '>', run: () => reorderSection(sec.id, ci) },
    { label: 'Delete', hint: 'the tasks stay', run: () => deleteSection(sec) },
  ]);
}

// --- the writes -------------------------------------------------------------

// moveTaskTo moves one task into a column, at one position.
//
// The project and the section travel in one task_move command, so the pair can
// never disagree for a round trip. The order keys of the target column follow,
// one task_update per row.
function moveTaskTo(t, sectionId, index) {
  const p = currentProject();
  if (!t || !p) return;
  const target = boardColumns().find((c) => (c.id || null) === (sectionId || null));
  if (!target) return;
  const was = { section: t.section_id || null, order: t.order_key || 'm' };
  const order = target.tasks.filter((x) => x.id !== t.id);
  order.splice(Math.min(Math.max(index, 0), order.length), 0, t);

  if (sectionId) t.section_id = sectionId;
  else delete t.section_id;
  queue('task_move', { id: t.id, project_id: p.id, section_id: sectionId || '' });
  order.forEach((x, i) => {
    const key = orderKeyAt(i);
    if (x.order_key === key) return;
    x.order_key = key;
    queue('task_update', { id: x.id, order_key: key });
  });
  S.marked.clear();
  // The undo puts the one task back where it was. The neighbours keep their new
  // keys, which changes no order that a person can see.
  toast(`Moved to ${target.name}`, () => {
    const back = S.tasks.get(t.id);
    if (!back) return;
    if (was.section) back.section_id = was.section;
    else delete back.section_id;
    back.order_key = was.order;
    queue('task_move', { id: back.id, project_id: p.id, section_id: was.section || '' });
    queue('task_update', { id: back.id, order_key: was.order });
    render();
  });
  render();
}

// moveTaskColumn is the keyboard form of a drag between two columns.
function moveTaskColumn(t, cols, at, dir) {
  const ci = at.ci + dir;
  if (!t || ci < 0 || ci >= cols.length) return;
  moveTaskTo(t, cols[ci].id, cols[ci].tasks.length);
  keepCursorOn(t);
}

// moveTaskRow is the keyboard form of a drag inside one column.
function moveTaskRow(t, cols, at, dir) {
  const row = at.row + dir;
  if (!t || row < 0 || row > cols[at.ci].tasks.length - 1) return;
  moveTaskTo(t, cols[at.ci].id, row);
  keepCursorOn(t);
}

// keepCursorOn holds the cursor on a task that just moved, so a second key
// press acts on the same row.
function keepCursorOn(t) {
  const i = visible().findIndex((x) => x.id === t.id);
  if (i < 0) return;
  S.sel = i;
  render();
}

// reorderSection puts one section at one position and renumbers the rest.
function reorderSection(id, toIndex) {
  const p = currentProject();
  if (!p) return;
  const secs = projectSections(p.id);
  const from = secs.findIndex((s) => s.id === id);
  if (from < 0) return;
  const before = secs.map((s) => ({ id: s.id, order: s.order_key }));
  const moved = secs.splice(from, 1)[0];
  secs.splice(Math.min(Math.max(toIndex, 0), secs.length), 0, moved);
  let changed = 0;
  secs.forEach((s, i) => {
    const key = orderKeyAt(i);
    if (s.order_key === key) return;
    s.order_key = key;
    queue('section_reorder', { id: s.id, order_key: key });
    changed++;
  });
  if (!changed) return;
  toast(`Moved the section ${moved.name}`, () => {
    before.forEach((b) => {
      const s = S.sections.get(b.id);
      if (!s || s.order_key === b.order) return;
      s.order_key = b.order;
      queue('section_reorder', { id: b.id, order_key: b.order });
    });
    render();
  });
  render();
}

// moveColumn is the keyboard form of a column drag.
function moveColumn(cols, ci, dir) {
  const col = cols[ci];
  if (!col || !col.id) return;
  reorderSection(col.id, ci - 1 + dir);
}

function addSection(p, rawName) {
  const name = (rawName || '').trim();
  if (!p || !name) return;
  const id = newId('s');
  const order = orderKeyAt(projectSections(p.id).length);
  S.sections.set(id, { id, project_id: p.id, name, order_key: order, v: 0 });
  queue('section_add', { id, project_id: p.id, name, order_key: order });
  render();
}

// renameSection swaps the column head for a field, the way the detail sheet
// edits a field: the change lands in the local state and in the outbox at once.
function renameSection(headEl, sec) {
  const input = document.createElement('input');
  input.className = 'col-rename';
  input.value = sec.name;
  input.setAttribute('aria-label', 'Section name');
  headEl.replaceWith(input);
  input.focus();
  input.select();
  let closed = false;
  const done = (keep) => {
    if (closed) return;
    closed = true;
    const name = input.value.trim();
    if (keep && name && name !== sec.name) {
      sec.name = name;
      queue('section_update', { id: sec.id, name });
    }
    render();
  };
  input.onkeydown = (e) => {
    if (e.key === 'Enter') done(true);
    else if (e.key === 'Escape') done(false);
  };
  input.onblur = () => done(true);
}

// deleteSection removes a heading and keeps the work.
//
// The server clears the section of every task that was in it, so the tasks move
// to the first column instead of leaving with the heading. The undo restores
// the heading and then files each task back, because the server cannot know
// which tasks were there.
function deleteSection(sec) {
  const ids = [...S.tasks.values()].filter((t) => t.section_id === sec.id).map((t) => t.id);
  const p = S.projects.get(sec.project_id);
  ids.forEach((id) => { delete S.tasks.get(id).section_id; });
  S.sections.delete(sec.id);
  queue('section_delete', { id: sec.id });
  toast(`Deleted the section ${sec.name}. ${countWord(ids.length)} stayed`, () => {
    S.sections.set(sec.id, sec);
    queue('section_restore', { id: sec.id });
    ids.forEach((id) => {
      const t = S.tasks.get(id);
      if (!t) return;
      t.section_id = sec.id;
      queue('task_move', { id, project_id: p ? p.id : t.project_id, section_id: sec.id });
    });
    render();
  });
  render();
}

// boardKey handles the keys that only the board has, and reports whether it
// took the key. A layout that only a pointer can drive fails the bar that
// PLAN.md section 7 sets.
function boardKey(e, cur) {
  const cols = boardColumns();
  const at = columnOf(cols, S.sel);
  // A browser reports the upper case letter while shift is down, and a
  // synthesised event can carry the lower case one, so both are read.
  const shift = (letter) => e.key === letter || (e.key === letter.toLowerCase() && e.shiftKey);
  if (shift('H')) { moveTaskColumn(cur, cols, at, -1); return true; }
  if (shift('L')) { moveTaskColumn(cur, cols, at, 1); return true; }
  if (shift('J')) { moveTaskRow(cur, cols, at, 1); return true; }
  if (shift('K')) { moveTaskRow(cur, cols, at, -1); return true; }
  if (e.key === '<' || (e.key === ',' && e.shiftKey)) { moveColumn(cols, at.ci, -1); return true; }
  if (e.key === '>' || (e.key === '.' && e.shiftKey)) { moveColumn(cols, at.ci, 1); return true; }
  if (e.key === 'h' || e.key === 'ArrowLeft') return jumpColumn(cols, at, -1);
  if (e.key === 'l' || e.key === 'ArrowRight') return jumpColumn(cols, at, 1);
  if (e.key === 'n') {
    const el = document.querySelector('.col-new');
    if (!el) return false;
    e.preventDefault();
    el.focus();
    return true;
  }
  return false;
}

// jumpColumn moves the cursor to the next column that holds a task. An empty
// column has no row to stand on, so the cursor passes over it.
function jumpColumn(cols, at, dir) {
  for (let ci = at.ci + dir; ci >= 0 && ci < cols.length; ci += dir) {
    const n = cols[ci].tasks.length;
    if (!n) continue;
    S.sel = flatIndex(cols, ci, Math.min(at.row, n - 1));
    render();
    return true;
  }
  return true;
}

// --- input ------------------------------------------------------------------

function wire() {
  const qa = $('qa');
  qa.addEventListener('input', () => {
    const p = parseQuickAdd(qa.value);
    const bits = [];
    if (p.due) bits.push('due <b>' + dueLabel(p.due, p.time) + '</b>');
    if (p.rrule) bits.push('repeats <b>' + p.rrule.replace('FREQ=', '').toLowerCase() + '</b>');
    if (p.priority) bits.push('priority <b>p' + p.priority + '</b>');
    if (p.project) {
      const hit = findProject(p.project);
      const many = [...S.projects.values()].filter((x) => x.name.toLowerCase().startsWith(p.project.toLowerCase()));
      bits.push('project <b>#' + esc(hit ? hit.name : p.project) + '</b>'
        + (hit ? '' : (many.length > 1 ? ' (matches ' + many.length + ' projects, type more)' : ' (no such project, goes to the inbox)')));
    }
    p.labels.forEach((l) => bits.push('label <b>@' + esc(l) + '</b>'));
    $('hint').innerHTML = qa.value.trim()
      ? (bits.length ? bits.join(' · ') : 'no date or tag found, the whole line becomes the title')
      : 'Press <kbd>q</kbd> to focus, <kbd>?</kbd> for keys.';
  });
  qa.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      const t = addFromText(qa.value);
      if (t) { qa.value = ''; $('hint').innerHTML = 'Added.'; S.sel = 0; render(); }
    } else if (e.key === 'Escape') { qa.value = ''; qa.blur(); $('hint').innerHTML = ''; }
  });
  $('fab').onclick = () => qa.focus();
  $('layout').onclick = toggleLayout;

  document.addEventListener('keydown', (e) => {
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') {
      if (e.key === 'Escape') { closeDetail(); e.target.blur(); }
      return;
    }
    if (S.menu) {
      if (e.key === 'Escape') closeMenu();
      return;
    }
    if (S.detail) {
      if (e.key === 'Escape') closeDetail();
      return;
    }
    const rows = visible();
    const cur = rows[S.sel];
    // Shift+T moves the whole overdue section, and plain t moves one task, so
    // the two must never be confused. A browser reports the upper case letter
    // while shift is down, but a synthesised event can carry the lower case
    // one, so the modifier is read as well as the letter.
    if (e.key === 'T' || (e.key === 't' && e.shiftKey)) {
      rescheduleAll(overdueRows(), todayISO());
      return;
    }
    if (e.key === 'A' || (e.key === 'a' && e.shiftKey)) { markAll(); return; }
    if (e.key === 'b') { toggleLayout(); return; }
    if (boardOn() && boardKey(e, cur)) return;

    // A selection changes what every action key means. One key does the same
    // thing to one task or to twenty, and the bar at the bottom says which.
    const many = S.marked.size > 0;
    const set = many ? markedRows() : [];

    switch (e.key) {
      case 'q': case 'a': e.preventDefault(); $('qa').focus(); break;
      case 'j': case 'ArrowDown': S.sel = Math.min(S.sel + 1, rows.length - 1); render(); break;
      case 'k': case 'ArrowUp': S.sel = Math.max(S.sel - 1, 0); render(); break;
      case 's': S.anchor = S.sel; toggleMark(cur); break;
      case 'Escape': clearMarks(); break;
      case 'x': case 'Enter': if (many) bulkComplete(set); else complete(cur); break;
      case '1': case '2': case '3': case '4':
        if (many) bulkPriority(set, Number(e.key)); else setPriority(cur, Number(e.key));
        break;
      case 't': if (many) rescheduleAll(set, plusDays(0)); else schedule(cur, 0); break;
      case 'm': if (many) rescheduleAll(set, plusDays(1)); else schedule(cur, 1); break;
      case 'w': if (many) rescheduleAll(set, plusDays(7)); else schedule(cur, 7); break;
      case '#': case 'Backspace': case 'Delete':
        e.preventDefault();
        if (many) bulkDelete(set); else removeTask(cur);
        break;
      case 'u': if (S.undo) { const u = S.undo.undo; S.undo = null; u(); } break;
      case 'e': case 'o': e.preventDefault(); openDetail(cur); break;
      case '?': showKeys(); break;
      case 'g': S.view = { q: 'today', title: 'Today' }; S.sel = 0; render(); break;
      case 'r': sync(); break;
    }
  });
}

function showKeys() {
  const el = document.createElement('div');
  el.className = 'keys';
  el.innerHTML = `<div class="card"><h3>Keys</h3><dl>
    <dt>q</dt><dd>focus quick add</dd>
    <dt>j / k</dt><dd>move down and up</dd>
    <dt>x</dt><dd>complete the selected task</dd>
    <dt>1..4</dt><dd>set priority</dd>
    <dt>t / m / w</dt><dd>due today, tomorrow, next week</dd>
    <dt>T</dt><dd>move every overdue task in this view to today</dd>
    <dt>s</dt><dd>pick this task for a bulk action</dd>
    <dt>A</dt><dd>pick every task in this view</dd>
    <dt>Escape</dt><dd>drop the selection</dd>
    <dt>&#8984; / Ctrl + click</dt><dd>pick one task</dd>
    <dt>Shift + click</dt><dd>pick a run of tasks</dd>
    <dt>e</dt><dd>open the task detail</dd>
    <dt>u</dt><dd>undo the last action</dd>
    <dt>r</dt><dd>sync now</dd>
    <dt>g</dt><dd>go to Today</dd>
    <dt>?</dt><dd>this list</dd></dl>
    <h3>Board layout</h3><dl>
    <dt>b</dt><dd>swap the list and the board, in a project view</dd>
    <dt>h / l</dt><dd>move to the column on the left or the right</dd>
    <dt>H / L</dt><dd>move this task one column left or right</dd>
    <dt>J / K</dt><dd>move this task down or up inside its column</dd>
    <dt>&lt; / &gt;</dt><dd>move this column left or right</dd>
    <dt>n</dt><dd>add a section</dd>
    <dt>Tab, then Enter</dt><dd>open the menu of a column: rename, move, delete</dd>
    <dt>drag</dt><dd>a task to another column, or a column head to reorder</dd></dl></div>`;
  el.onclick = () => el.remove();
  document.body.appendChild(el);
}

// --- start ------------------------------------------------------------------

load();
render();
wire();
sync().then(listenEvents);
window.addEventListener('online', () => { S.online = true; sync(); });
window.addEventListener('offline', () => { S.online = false; render(); });
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
