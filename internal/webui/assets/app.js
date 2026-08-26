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
  labels: new Map(),
  outbox: [],
  view: { kind: 'filter', q: 'today', title: 'Today' },
  sel: 0,
  undo: null,
  online: true,
  detail: null,
  menu: null,
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
      labels: [...S.labels.values()],
      outbox: S.outbox,
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
    (d.labels || []).forEach((l) => S.labels.set(l.id, l));
    S.outbox = d.outbox || [];
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

function rescheduleMenu(anchor, tasks) {
  closeMenu();
  const opts = [
    ['Today', plusDays(0)],
    ['Tomorrow', plusDays(1)],
    ['This weekend', nextDow(6)],
    ['Next week', nextDow(1) === todayISO() ? plusDays(7) : nextDow(1)],
    ['No date', null],
  ];
  const back = document.createElement('div');
  back.className = 'menu-back';
  const el = document.createElement('div');
  el.className = 'menu';
  el.innerHTML = opts.map(([label, d], i) => `<button data-i="${i}"><span>${esc(label)}</span>`
    + `<span class="w">${d ? esc(new Date(d + 'T00:00').toLocaleDateString(undefined,
        { weekday: 'short', day: 'numeric', month: 'short' })) : ''}</span></button>`).join('')
    + `<label class="pick"><span>Pick a day</span><input type="date" value="${todayISO()}"></label>`;

  const r = anchor.getBoundingClientRect();
  el.style.top = Math.round(r.bottom + 6) + 'px';
  el.style.left = Math.round(Math.max(8, Math.min(r.left, window.innerWidth - 248))) + 'px';

  [...el.querySelectorAll('button')].forEach((b) => {
    b.onclick = () => { closeMenu(); rescheduleAll(tasks, opts[Number(b.dataset.i)][1]); };
  });
  el.querySelector('input').onchange = (e) => {
    if (e.target.value) { closeMenu(); rescheduleAll(tasks, e.target.value); }
  };
  back.onclick = closeMenu;
  document.body.append(back, el);
  S.menu = { back, el };
  el.querySelector('button').focus();
}

function closeMenu() {
  if (!S.menu) return;
  S.menu.back.remove();
  S.menu.el.remove();
  S.menu = null;
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
  $('count').textContent = rows.length ? rows.length + (rows.length === 1 ? ' task' : ' tasks') : '';
  const pend = S.outbox.length;
  $('status').textContent = S.online ? (pend ? pend + ' to send' : 'v' + S.version) : 'offline · ' + pend + ' queued';

  const list = $('list');
  if (!rows.length) {
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
    [...list.querySelectorAll('.row')].forEach((el, i) => {
      el.querySelector('.box').onclick = (e) => { e.stopPropagation(); complete(rows[i]); };
      // The circle completes the task. Anywhere else in the row opens the
      // detail, because that is what a pointer user expects.
      el.querySelector('.body').onclick = (ev) => { ev.stopPropagation(); S.sel = i; openDetail(rows[i]); };
      el.onclick = () => { S.sel = i; render(); };
    });
    const sel = list.querySelector('.row.sel');
    if (sel) sel.scrollIntoView({ block: 'nearest' });
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
    switch (e.key) {
      case 'q': case 'a': e.preventDefault(); $('qa').focus(); break;
      case 'j': case 'ArrowDown': S.sel = Math.min(S.sel + 1, rows.length - 1); render(); break;
      case 'k': case 'ArrowUp': S.sel = Math.max(S.sel - 1, 0); render(); break;
      case 'x': case 'Enter': complete(cur); break;
      case '1': case '2': case '3': case '4': setPriority(cur, Number(e.key)); break;
      case 't': schedule(cur, 0); break;
      case 'm': schedule(cur, 1); break;
      case 'w': schedule(cur, 7); break;
      case '#': case 'Backspace': case 'Delete': e.preventDefault(); removeTask(cur); break;
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
    <dt>e</dt><dd>open the task detail</dd>
    <dt>u</dt><dd>undo the last action</dd>
    <dt>r</dt><dd>sync now</dd>
    <dt>g</dt><dd>go to Today</dd>
    <dt>?</dt><dd>this list</dd></dl></div>`;
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
