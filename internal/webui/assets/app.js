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
  reminders: new Map(),
  // The talk on a task, by comment id. A comment is a row like any other, so
  // it arrives in the pull and it is saved with the rest.
  comments: new Map(),
  outbox: [],
  view: { kind: 'filter', q: 'today', title: 'Today' },
  sel: 0,
  undo: null,
  online: true,
  detail: null,
  // filterError holds the one sentence a broken query produced, so the view
  // says what is wrong instead of showing an empty list with no reason.
  filterError: '',
  menu: null,
  // marked holds the ids of the tasks a bulk action will touch. It is not
  // saved: a selection is about the next few seconds, and a stale selection
  // read back from storage would act on rows the user cannot remember picking.
  marked: new Set(),
  // anchor is the row a shift click measures a range from.
  anchor: 0,
  // layout is 'list', 'board', 'calendar' or 'shop'. It is saved, because a
  // person who arranges a project as a board expects the board again after a
  // reload.
  layout: 'list',
  // shopClear holds, per project, the moment somebody emptied the basket. A
  // ticked item stays on the screen until then, so a wrong tap is one tap
  // back. It is local: what the basket looks like is this screen's business,
  // and the tick itself is already on the server.
  shopClear: {},
  // cal holds where the calendar looks: 'month' or 'week', and the day inside
  // the period on show. Saved with the layout, for the same reason.
  cal: { mode: 'month', anchor: '' },
  // drag holds what a pointer is dragging: one task, or one section.
  drag: null,
  // The household. me is this account, inbox is its own inbox project, people
  // is everybody in the house, and shares says which list reaches whom. The
  // server answers all four, and a file with one person answers with one.
  me: '',
  inbox: 'inbox',
  people: [],
  shares: {},
};

import { parseQuickAdd, newId, iso } from './parse.js';
import * as pk from './passkey.js';
import * as md from './md.js';
import * as flt from './filter.js';
import * as db from './db.js';
import * as act from './activity.js';
import * as bnd from './band.js';

const $ = (id) => document.getElementById(id);
const uuid = () => (crypto.randomUUID ? crypto.randomUUID() : String(Math.random()).slice(2));
const todayISO = () => new Date().toISOString().slice(0, 10);

// --- local storage ----------------------------------------------------------
// The local copy lives in IndexedDB, one object store per table, plus one
// small record for what belongs to this device. See db.js, and D-020 in
// docs/DECISIONS.md for why the plan said SQLite in OPFS and this is not that.
//
// Nothing on this path waits for the disk. A write is marked, the screen
// draws, and db.js writes the batch behind it.

let cache = new db.Cache(null);

// META is the record of what belongs to this device: the sync watermark, the
// outbox, and how this browser is arranged. Everything else is a row of a
// table and the server can rebuild it.
function metaState() {
  return {
    version: S.version,
    outbox: S.outbox,
    layout: S.layout,
    cal: S.cal,
    shopClear: S.shopClear,
    me: S.me,
    inbox: S.inbox,
    people: S.people,
    shares: S.shares,
  };
}

// save records the device record. A row is marked where it changes, by touch()
// and by applyDelta, so this call never walks the whole account.
function save() {
  cache.markMeta(metaState());
}

// touch marks one row of one table. Every local write passes queue(), which
// knows the command type, and that is what names the table.
function touch(table, id, row) {
  cache.mark(table, id, row);
}

// tableOf maps a table name to the Map that holds it, so one loop serves the
// load and the delta.
const MAPS = {
  tasks: () => S.tasks,
  projects: () => S.projects,
  sections: () => S.sections,
  labels: () => S.labels,
  reminders: () => S.reminders,
  comments: () => S.comments,
};

async function load() {
  cache = new db.Cache(await db.idbBackend() || db.localBackend());
  // A device that used the old single string keeps its account: the move
  // happens once, before the first read.
  await db.carryOver(cache);
  const held = await cache.read();
  for (const table of db.TABLES) {
    const map = MAPS[table]();
    (held.rows[table] || []).forEach((row) => { if (row && row.id) map.set(row.id, row); });
  }
  const m = held.meta || {};
  S.version = m.version || 0;
  S.outbox = m.outbox || [];
  S.layout = ['board', 'calendar', 'shop'].includes(m.layout) ? m.layout : 'list';
  if (m.cal && (m.cal.mode === 'month' || m.cal.mode === 'week')) S.cal = m.cal;
  S.shopClear = m.shopClear || {};
  S.me = m.me || '';
  S.inbox = m.inbox || 'inbox';
  S.people = m.people || [];
  S.shares = m.shares || {};
  rowsChanged();
}

// --- sync -------------------------------------------------------------------

function queue(type, args) {
  S.outbox.push({ uuid: uuid(), type, args });
  // Every local edit goes through here, so this is where the filter index
  // learns that the rows moved, and where the local database learns which row
  // to write. The command type names the table and the arguments name the row.
  rowsChanged();
  const table = db.storeOf(type);
  if (table && args && args.id) touch(table, args.id, MAPS[table]().get(args.id));
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
    if (d.reset) readHousehold();
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
  // The server says who is asking and where their inbox is. A file with one
  // account answers with the fixed id, so nothing changes there.
  if (d.me) S.me = d.me;
  if (d.inbox) S.inbox = d.inbox;
  if (d.reset) {
    // A list stopped being shared. A delta cannot describe a row that went
    // away, so the server asked for a fresh start and this is it. The outbox
    // is untouched: what this device wrote is still owed to the server.
    S.version = 0;
    for (const table of db.TABLES) {
      const map = MAPS[table]();
      [...map.keys()].forEach((id) => touch(table, id, null));
      map.clear();
    }
  }
  S.version = d.version || S.version;
  // One loop for every table. A row the server marks deleted leaves the local
  // copy, here and in the local database, so a reload never brings it back.
  for (const table of db.TABLES) {
    const map = MAPS[table]();
    (d[table] || []).forEach((row) => {
      if (row.deleted_at) map.delete(row.id);
      else map.set(row.id, row);
      touch(table, row.id, map.get(row.id));
    });
  }
  rowsChanged();
}

// readHousehold asks who is in the house and which lists are shared. It runs
// after the first sync and after a change, and it fails quietly: a household
// of one needs none of it, and an offline browser keeps what it had.
async function readHousehold() {
  try {
    const res = await fetch('/v1/household');
    if (!res.ok) return;
    const d = await res.json();
    S.me = d.me || S.me;
    S.inbox = d.inbox || S.inbox;
    S.people = d.people || [];
    S.shares = d.shares || {};
    save();
    render();
  } catch (e) { /* offline: the panel shows what it knows */ }
}

// shared says whether a project reaches anybody else.
function sharedWith(projectId) {
  return (S.shares[projectId] || []).map((id) => S.people.find((p) => p.id === id)).filter(Boolean);
}

// personName says who somebody is, in one word.
function personName(id) {
  if (!id) return '';
  const p = S.people.find((x) => x.id === id);
  if (!p) return 'someone';
  return p.is_me ? 'me' : p.name;
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
    project_id: project ? project.id : S.inbox,
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
  // "remind me at 8" with no day means the next 8 o'clock that has not passed.
  // A reminder needs a due moment to hang from, and a person who names a time
  // means today, or tomorrow when today is over.
  if (p.remindAt && !t.due_date) {
    const at = new Date(todayISO() + 'T' + p.remindAt);
    t.due_date = at.getTime() > Date.now() ? todayISO() : plusDays(1);
  }
  // The day is settled now, so the task can take its place in that day. A new
  // task goes to the end of the band it joins, not above the rows a person
  // arranged by hand. See endKey.
  t.order_key = endKey(t.due_date, t.priority);
  S.tasks.set(id, t);
  queue('task_add', {
    id, title: p.title,
    project_id: t.project_id,
    order_key: t.order_key === 'm' ? undefined : t.order_key,
    due_date: t.due_date || undefined,
    due_time: p.time || undefined,
    priority: p.priority || undefined,
    rrule: p.rrule || undefined,
    labels: p.labels.length ? p.labels : undefined,
  });
  armFromParse(t, p);
  return t;
}

// armFromParse turns the reminder that quick add read into a reminder row.
// A clock time becomes an offset from the due moment, because that is the one
// shape the reminder table holds for a task. A line with no due date and only
// an offset arms nothing: there is no moment to count back from.
function armFromParse(t, p) {
  if (!t.due_date) return;
  if (p.remindAt) {
    const due = new Date(t.due_date + 'T' + (t.due_time || '09:00'));
    const fire = new Date(t.due_date + 'T' + p.remindAt);
    setReminder(t, String(Math.round((due - fire) / 60000)));
  } else if (p.remindBefore) {
    setReminder(t, String(p.remindBefore));
  }
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
  reArm(t);
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
    reArm(t);
    return;
  }
  t.due_date = date;
  const args = { id: t.id, due_date: date };
  if (time) { t.due_time = time; args.due_time = time; }
  queue('task_update', args);
  // The reminder hangs off the due moment, so it moves with it. Every path
  // that writes a due date comes through here or through schedule(), so a
  // reminder can no longer be left pointing at last week.
  reArm(t);
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
  const where = p && p.id !== S.inbox ? '#' + p.name : 'the inbox';
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
    label: p.id === S.inbox ? 'Inbox' : p.name,
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

// The filter language, one evaluator, in internal/webui/assets/filter.js. It
// reads the grammar the server compiles into SQL, and the shared corpus in
// parser-fixtures/filter.json proves that the two agree term by term.
//
// A query that this store cannot answer throws one sentence. S.filterError
// holds it, so the view says what is wrong instead of listing the wrong rows.

function filterEnv() {
  return {
    today: todayISO(),
    inboxId: S.inbox,
    me: S.me,
    accounts: S.people,
    tasks: S.tasks,
    projects: S.projects,
    sections: S.sections,
    comments: S.comments,
  };
}

// matches answers for one task. It never throws: a broken query matches
// nothing, and render() reports the sentence once instead of per row.
function matches(t, q) {
  try {
    return flt.compileCached(q, todayISO())(t, filterIndex());
  } catch (e) {
    return false;
  }
}

// filterIndex is rebuilt whenever the rows change. Every predicate that looks
// beyond one row reads it: a project family, a section name, a sub-task.
//
// The sidebar counts every view, so one render asks for the index a dozen
// times. rowsChanged() is called by every write path, which is what makes the
// cache safe: a field edit does not change the number of rows, so a count of
// them is not a valid stamp.
let indexCache = null;
let indexGen = -1;
let rowGen = 0;

function rowsChanged() { rowGen++; }

function filterIndex() {
  if (indexCache && indexGen === rowGen) return indexCache;
  indexGen = rowGen;
  indexCache = flt.index(filterEnv());
  return indexCache;
}

// queryRows answers the current view, with no layout in the way. A broken
// query leaves its sentence in S.filterError and returns nothing.
function queryRows() {
  let pred;
  try {
    pred = flt.compileCached(S.view.q, todayISO());
    S.filterError = '';
  } catch (e) {
    S.filterError = e.message;
    return [];
  }
  const env = filterIndex();
  return [...S.tasks.values()].filter((t) => pred(t, env));
}

function visible() {
  // Every layout returns the same flat list of tasks, in the order it draws
  // them. The cursor, the marks, every bulk action and every key therefore
  // work in all three layouts with no second code path.
  if (boardOn()) return boardColumns().flatMap((c) => c.tasks);
  if (shopOn()) return shopAisles().flatMap((a) => a.items).concat(basket());
  if (calendarOn()) {
    const cal = calendarCells();
    return cal.days.flatMap((d) => d.tasks).concat(cal.undated);
  }
  const out = queryRows();
  out.sort(listOrder);
  return out;
}

// listOrder is the sort of a list view: the day, then the priority, then the
// manual order key, then the title. The first three keys are the ORDER BY of
// GET /v1/tasks in internal/store/store.go and of the Room query in
// TehaRepository, so a person who arranges a day on one client sees the same
// order on the other two.
//
// The title only breaks a tie between two equal keys, which is what a row that
// nobody dragged still has. See band.js for what a drag may move.
function listOrder(a, b) {
  const ad = a.due_date || '9999', bd = b.due_date || '9999';
  if (ad !== bd) return ad < bd ? -1 : 1;
  if ((a.priority || 4) !== (b.priority || 4)) return (a.priority || 4) - (b.priority || 4);
  const ak = a.order_key || 'm', bk = b.order_key || 'm';
  if (ak !== bk) return ak < bk ? -1 : 1;
  return (a.title || '').localeCompare(b.title || '');
}

// --- render -----------------------------------------------------------------

function render() {
  const rows = visible();
  if (S.sel >= rows.length) S.sel = Math.max(0, rows.length - 1);

  // sidebar
  const nav = $('nav');
  const projects = [...S.projects.values()].filter((p) => p.id !== S.inbox);
  // "Assigned to me" only means something once two people share a list.
  const views = S.people.length > 1
    ? VIEWS.concat([{ q: 'assigned to: me', title: 'Assigned to me' }])
    : VIEWS;
  nav.innerHTML = views.map((v) => navLink(v.q, v.title)).join('')
    + (projects.length ? '<div class="grp">Projects</div>' : '')
    + projects.map((p) => navLink('#' + p.name, p.name, p)).join('');
  [...nav.querySelectorAll('a')].forEach((a) => {
    a.onclick = (e) => {
      if (e.target.closest('.pmenu')) return;
      S.view = { q: a.dataset.q, title: a.dataset.title };
      S.sel = 0;
      render();
    };
  });
  [...nav.querySelectorAll('.pmenu')].forEach((b) => {
    b.onclick = (e) => { e.stopPropagation(); e.preventDefault(); shareMenu(b, b.dataset.project); };
  });

  $('title').textContent = S.view.title;
  const filt = $('filter');
  if (filt && document.activeElement !== filt) filt.value = S.view.q;
  const lay = $('layout');
  lay.hidden = !currentProject();
  // A layout button says where it goes, not where you are. Highlighting a
  // button that means "leave this layout" reads backwards.
  lay.textContent = S.layout === 'board' ? 'List' : 'Board';
  $('caltoggle').textContent = S.layout === 'calendar' ? 'List' : 'Calendar';
  const shopBtn = $('shoptoggle');
  shopBtn.hidden = !currentProject();
  shopBtn.textContent = S.layout === 'shop' ? 'List' : 'Shop';
  $('count').textContent = rows.length ? rows.length + (rows.length === 1 ? ' task' : ' tasks') : '';
  const pend = S.outbox.length;
  const st = $('status');
  st.textContent = S.online ? (pend ? pend + ' to send' : 'v' + S.version) : 'offline · ' + pend + ' queued';
  // A local database that refuses a write is worth saying once. The screen and
  // the outbox are still right, so the work is not lost while the tab is open,
  // and a reload would lose whatever did not land.
  if (cache.failures) {
    st.textContent += ' · not saved here';
    st.title = 'This browser refused to write the local copy. Your changes are '
      + 'on screen and on their way to the server. Do not close the tab until '
      + 'the count above reads a version.';
  } else {
    st.title = '';
  }

  const list = $('list');
  if (boardOn()) {
    renderBoard(list);
  } else if (shopOn()) {
    renderShop(list);
  } else if (calendarOn()) {
    renderCalendar(list);
  } else if (S.filterError) {
    list.innerHTML = `<div class="empty"><b>That filter does not read.</b><br>${esc(S.filterError)}</div>`;
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

  // One wiring for the list and the board. The rows stand in the same order as
  // rows, whether they came from the day groups or from the board columns.
  // The calendar wires its own chips, because a chip has no completion circle.
  if (!calendarOn()) [...list.querySelectorAll('.row')].forEach((el, i) => {
    el.querySelector('.box').onclick = (e) => { e.stopPropagation(); complete(rows[i]); };
    // The circle completes the task. Anywhere else in the row opens the
    // detail, because that is what a pointer user expects. A held modifier
    // means the user is picking a set instead, which is the same gesture
    // every file manager and mail client uses.
    el.querySelector('.body').onclick = (ev) => {
      // A title can carry a link. A click on it opens the link and nothing
      // else, because a link that opens a dialog as well is a trap.
      if (ev.target.closest('a')) { S.sel = i; return; }
      ev.stopPropagation();
      S.sel = i;
      if (ev.metaKey || ev.ctrlKey) { S.anchor = i; toggleMark(rows[i]); return; }
      if (ev.shiftKey) { markRange(i); return; }
      S.anchor = i;
      openDetail(rows[i]);
    };
    el.onclick = () => { S.sel = i; render(); };
  });
  // The plain list takes a drag on a row, to order the day by hand. The other
  // three layouts each wire their own.
  if (listDrag() && rows.length) wireRowDrag(list, rows);
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

function navLink(q, title, project) {
  const n = [...S.tasks.values()].filter((t) => matches(t, q)).length;
  const on = S.view.q === q ? ' class="on"' : '';
  // A shared list carries a mark and a button. Two people need to see at a
  // glance which lists the other one reads.
  const with_ = project ? sharedWith(project.id) : [];
  const mark = with_.length ? `<span class="sh" title="Shared with ${esc(with_.map((p) => p.name).join(', '))}">&#9679;</span>` : '';
  const menu = project && S.people.length > 1
    ? `<button class="pmenu" data-project="${esc(project.id)}" aria-label="Share ${esc(title)}">&#8943;</button>` : '';
  return `<a${on} data-q="${esc(q)}" data-title="${esc(title)}"><span>${esc(title)}</span>`
    + `${mark}${menu}<span class="c">${n || ''}</span></a>`;
}

// shareMenu lists the household beside one project, with a tick for the people
// who already have it. Only the owner of the list sees a working menu.
function shareMenu(anchor, projectId) {
  const holders = new Set((S.shares[projectId] || []));
  const others = S.people.filter((p) => !p.is_me);
  if (!others.length) return;
  popup(anchor, others.map((p) => ({
    label: (holders.has(p.id) ? '\u2713 ' : '') + p.name,
    hint: holders.has(p.id) ? 'shared' : 'not shared',
    run: () => shareProject(projectId, p.id, !holders.has(p.id)),
  })));
}

// shareProject gives one list to one person, or takes it back.
async function shareProject(projectId, accountId, share) {
  try {
    const res = await fetch('/v1/share', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ project_id: projectId, account_id: accountId, share }),
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok) { toast(d.error || 'That list could not be shared.'); render(); return; }
    await readHousehold();
    toast(share ? 'Shared with ' + personName(accountId) : 'No longer shared');
    sync();
  } catch (e) {
    toast('The server did not answer, so nothing changed.');
    render();
  }
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
  if (p && p.id !== S.inbox) meta.push(`<span class="pr">#${esc(p.name)}</span>`);
  // A note on the task shows as one mark, not as its text. A list is a list.
  if ((t.description || '').trim()) meta.push('<span class="nt" title="This task has a note">&#9998;</span>');
  // The talk on a task shows as a count. The text belongs in the panel.
  const talk = commentsOn(t.id).length;
  if (talk) meta.push(`<span class="cm" title="Comments">&#128172; ${talk}</span>`);
  // Who does it. A task of this person's own carries no name: the list would
  // then say "me" on every row and mean nothing.
  if (t.assignee_id && t.assignee_id !== S.me) {
    meta.push(`<span class="who">${esc(personName(t.assignee_id))}</span>`);
  }
  return `<div class="${cls.join(' ')}"><button class="box" aria-label="Complete"></button>
    <div class="body"><div class="t">${md.inline(t.title)}</div>
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

// --- manual order in a list -------------------------------------------------
// Today is the view a person reads every morning, so it is the view that has
// to take a hand order. The board could already do it and the list could not,
// which made the board the only place to arrange a day, and a day spans more
// than one project.
//
// listOrder above sorts by the day, then the priority, then the order key. A
// drag may therefore only move a row inside a band: the rows of one day at one
// priority. band.js says why, and holds the arithmetic. Press 1 to 4 to leave
// a band.

// bandOf names the band of one row, for band.js. The name holds the two sort
// keys above the order key, and nothing else: the day and the priority. Two
// rows of one band therefore always stand next to each other, because
// listOrder puts them there.
//
// It is not the heading on the screen. The Overdue heading gathers every past
// date under one word, and two of those dates are two bands.
function bandOf(t) {
  return (t.due_date || '') + ' ' + (t.priority || 4);
}

// endKey answers the order key a new task needs, so that it lands at the end
// of the rows it will sit among: the open tasks of the same day at the same
// priority. Without it a new task would stand above every row a person
// dragged, because the default key sorts before every key a drag writes.
//
// A day that nobody arranged carries the default key on every row. The new
// task then keeps that key too, so a list that nobody dragged reads exactly as
// it read before.
function endKey(due, priority) {
  let max = '';
  S.tasks.forEach((t) => {
    if (t.state !== 'open') return;
    if ((t.due_date || '') !== (due || '')) return;
    if ((t.priority || 4) !== (priority || 4)) return;
    const k = t.order_key || 'm';
    if (k > max) max = k;
  });
  return bnd.after(max) || 'm';
}

// listDrag reports whether the view on screen is the plain list. The board,
// the calendar and shopping mode each draw their own order and wire their own
// drag, and a broken filter draws a sentence and no rows at all.
function listDrag() {
  return !boardOn() && !shopOn() && !calendarOn() && !S.filterError;
}

// moveRow puts one row of the list before another and renumbers the band they
// share. Both indexes are indexes of the list on screen. A `to` past the last
// row of the band means the end of it.
//
// One task_update per changed row, which is the shape the board already
// writes: a command per row replays the same way from an outbox tomorrow.
function moveRow(rows, at, to) {
  const band = bnd.bandAt(rows, at, bandOf);
  if (!band) return;
  const moved = rows[at];
  const next = bnd.reorder(band.rows, at - band.from, to - band.from);
  const writes = bnd.rekey(next);
  if (!writes.length) return;
  // The undo puts back every key this move changed, and not only the key of
  // the row that moved. A row that never carried a key has nowhere else to go
  // back to, so the whole band has to travel together.
  const before = writes.map((w) => ({ id: w.id, order: S.tasks.get(w.id).order_key || 'm' }));
  writes.forEach((w) => {
    const t = S.tasks.get(w.id);
    if (!t) return;
    t.order_key = w.order_key;
    queue('task_update', { id: w.id, order_key: w.order_key });
  });
  toast('Moved', () => {
    before.forEach((b) => {
      const t = S.tasks.get(b.id);
      if (!t || t.order_key === b.order) return;
      t.order_key = b.order;
      queue('task_update', { id: b.id, order_key: b.order });
    });
    render();
  });
  keepCursorOn(moved);
}

// nudgeRow is the keyboard form of a drag: one row, one place, inside the
// band. Shift+J and Shift+K are the pair the board already uses.
function nudgeRow(rows, dir) {
  const at = S.sel;
  const band = bnd.bandAt(rows, at, bandOf);
  if (!band) return;
  const next = at + dir;
  if (next < band.from || next > band.to) {
    $('hint').textContent = dir > 0
      ? 'That is the last task of this day at this priority. Press 1 to 4 to change the priority.'
      : 'That is the first task of this day at this priority. Press 1 to 4 to change the priority.';
    return;
  }
  // A move down goes before the row after the neighbour, because "before the
  // neighbour" is where the row already stands.
  moveRow(rows, at, dir > 0 ? at + 2 : at - 1);
}

// wireRowDrag makes every row of the list draggable inside its own band. A row
// of another band takes no drop, so a drag can never change a priority or a
// day by accident. A line above or below the row under the pointer says where
// the drop lands.
function wireRowDrag(list, rows) {
  const els = [...list.querySelectorAll('.row')];
  const clear = () => els.forEach((el) => el.classList.remove('dropbefore', 'dropafter'));
  els.forEach((el, i) => {
    el.draggable = true;
    el.ondragstart = (e) => {
      S.drag = { kind: 'row', at: i };
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', rows[i].id);
      el.classList.add('held');
    };
    el.ondragend = () => { S.drag = null; el.classList.remove('held'); clear(); };
    // A drop lands above the row under the pointer, or below it, whichever
    // half the pointer is in. That is the one rule a pointer user expects.
    const below = (e) => {
      const r = el.getBoundingClientRect();
      return e.clientY > r.top + r.height / 2;
    };
    const inBand = () => {
      const d = S.drag;
      if (!d || d.kind !== 'row') return null;
      const band = bnd.bandAt(rows, d.at, bandOf);
      if (!band || i < band.from || i > band.to) return null;
      return d;
    };
    el.ondragover = (e) => {
      if (!inBand()) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      clear();
      el.classList.add(below(e) ? 'dropafter' : 'dropbefore');
    };
    el.ondragleave = () => el.classList.remove('dropbefore', 'dropafter');
    el.ondrop = (e) => {
      const d = inBand();
      S.drag = null;
      clear();
      if (!d) return;
      e.preventDefault();
      moveRow(rows, d.at, i + (below(e) ? 1 : 0));
    };
  });
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
    <div class="d-note">
      <div class="d-md md" tabindex="0" role="button" aria-label="Notes, Markdown. Press Enter to edit."></div>
      <textarea class="d-desc" rows="6" placeholder="Notes. Markdown works." aria-label="Notes" hidden></textarea>
      <div class="d-hint d-mdhint" hidden>Markdown: <kbd>**bold**</kbd> <kbd>[text](url)</kbd> <kbd>- list</kbd>.
        Paste a link over selected text to make a link. <kbd>Escape</kbd> leaves the note.</div>
    </div>
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
    ${sectionRow(t)}
    ${assigneeRow(t)}
    <div class="d-row"><span class="d-lab">Labels</span>
      <input class="d-labels" value="${esc((t.labels || []).join(', '))}" placeholder="store, call"></div>
    <div class="d-row"><span class="d-lab">Repeats</span>
      <input class="d-rrule" value="${esc(t.rrule || '')}" placeholder="FREQ=WEEKLY;BYDAY=MO"></div>
    <div class="d-row"><span class="d-lab">Remind</span>
      <select class="d-remind"${t.due_date ? '' : ' disabled'}>${remindChoices(t).map(([v, label]) =>
        `<option value="${v}"${String(remindOffset(t) === null ? '' : remindOffset(t)) === v ? ' selected' : ''}>${esc(label)}</option>`).join('')}</select>
      ${t.due_date ? '' : '<span class="d-hint">Set a due date first.</span>'}</div>
    <div class="d-subs">
      <span class="d-lab">Sub-tasks</span>
      ${kids.map((k) => `<div class="d-sub">${esc(k.title)}</div>`).join('')}
      <input class="d-newsub" placeholder="Add a sub-task, then press Enter">
    </div>
    <div class="d-talk">
      <span class="d-lab">Comments</span>
      <div class="d-talklist"></div>
      <input class="d-newcm" placeholder="Say something, then press Enter">
    </div>
    <details class="d-hist">
      <summary>History</summary>
      <div class="aclist"></div>
    </details>
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
      // The reminder hangs off the due moment, so it moves with it.
      if (field === 'due_date' || field === 'due_time') reArm(t);
    };
  };

  q('.d-title').onchange = () => { const v = q('.d-title').value.trim(); if (v) patch({ title: v }); };

  // The note reads as Markdown and edits as text. A click swaps one for the
  // other, so the panel shows the note the way the rest of the app shows it
  // and still edits in one field.
  const view = q('.d-md');
  const editor = q('.d-desc');
  const hint = q('.d-mdhint');
  const drawNote = () => {
    const text = (t.description || '').trim();
    view.innerHTML = text ? md.render(t.description)
      : '<span class="d-hint">Add a note. Markdown works.</span>';
  };
  const editNote = (on) => {
    view.hidden = on;
    editor.hidden = !on;
    hint.hidden = !on;
    if (!on) return;
    editor.value = t.description || '';
    editor.focus();
    editor.setSelectionRange(editor.value.length, editor.value.length);
  };
  const saveNote = () => {
    const v = editor.value;
    if (v !== (t.description || '')) patch({ description: v });
    drawNote();
    editNote(false);
  };
  drawNote();
  view.onclick = (e) => { if (!e.target.closest('a')) editNote(true); };
  view.onkeydown = (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); editNote(true); } };
  editor.onblur = saveNote;
  editor.onkeydown = (e) => {
    if (e.key === 'Escape' || (e.key === 'Enter' && (e.metaKey || e.ctrlKey))) {
      e.preventDefault();
      e.stopPropagation(); // the sheet closes on Escape, and the note is not the sheet
      saveNote();
      q('.d-title').focus();
    }
  };
  md.wirePasteLink(editor);
  md.wirePasteLink(q('.d-title'), () => q('.d-title').dispatchEvent(new Event('change')));
  dateField('.d-due', 'due_date');
  dateField('.d-time', 'due_time');
  dateField('.d-start', 'start_date');
  dateField('.d-deadline', 'deadline');
  q('.d-remind').onchange = () => setReminder(t, q('.d-remind').value);
  const who = q('.d-assignee');
  if (who) {
    who.onchange = () => {
      const v = who.value;
      if (v) patch({ assignee_id: v });
      else { delete t.assignee_id; patch({}, ['assignee_id']); }
    };
  }
  const sec = q('.d-section');
  if (sec) {
    sec.onchange = () => {
      // task_move carries the project and the section together, because a
      // section belongs to one project and the pair must agree.
      const v = sec.value;
      if (v) t.section_id = v; else delete t.section_id;
      queue('task_move', { id: t.id, project_id: t.project_id, section_id: v });
      render();
    };
  }
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
  drawTalk(el, t);
  // The history of one task loads when the reader asks for it. It costs a
  // request, and most of the time nobody opens the panel.
  const hist = q('.d-hist');
  hist.ontoggle = () => {
    if (!hist.open || hist.dataset.loaded) return;
    hist.dataset.loaded = '1';
    act.draw(hist.querySelector('.aclist'), { task: t.id }, logContext());
  };
  q('.d-newcm').onkeydown = (e) => {
    if (e.key !== 'Enter') return;
    const box = q('.d-newcm');
    const body = box.value.trim();
    if (!body) return;
    addComment(t.id, body);
    box.value = '';
    drawTalk(el, t);
  };
  md.wirePasteLink(q('.d-newcm'));

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

// --- comments ---------------------------------------------------------------
// A comment is a row of its own, and it reaches everybody who can see the
// task. Only the author changes their own line, which is what the server
// enforces, so the panel offers the controls to nobody else.

// commentsOn lists the talk on one task, oldest first. The created stamp
// orders it, and the id breaks a tie: an import gives a whole conversation the
// same second.
function commentsOn(taskId) {
  return [...S.comments.values()]
    .filter((c) => c.task_id === taskId && !c.deleted_at)
    .sort((a, b) => (a.created_at || '').localeCompare(b.created_at || '') || a.id.localeCompare(b.id));
}

function addComment(taskId, body) {
  const id = newId('cm');
  S.comments.set(id, {
    id, task_id: taskId, account_id: S.me, body,
    created_at: new Date().toISOString(), v: 0,
  });
  queue('comment_add', { id, task_id: taskId, body });
}

// when says how long ago in the fewest words. A conversation about a chore is
// about "an hour ago", never about a timestamp.
function when(stamp) {
  const then = Date.parse(stamp || '');
  if (!then) return '';
  const mins = Math.round((Date.now() - then) / 60000);
  if (mins < 1) return 'now';
  if (mins < 60) return mins + 'm ago';
  if (mins < 60 * 24) return Math.round(mins / 60) + 'h ago';
  const days = Math.round(mins / (60 * 24));
  if (days < 7) return days + 'd ago';
  return new Date(then).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
}

// drawTalk paints the comments of one task inside an open panel.
function drawTalk(el, t) {
  const box = el.querySelector('.d-talklist');
  if (!box) return;
  const rows = commentsOn(t.id);
  if (!rows.length) {
    box.innerHTML = '<span class="d-hint">Nothing said yet.</span>';
    return;
  }
  box.innerHTML = rows.map((c) => {
    const mine = c.account_id === S.me;
    return `<div class="cmrow" data-id="${esc(c.id)}">
      <div class="cmwho">${esc(mine ? 'me' : personName(c.account_id))}
        <span class="cmwhen">${esc(when(c.created_at))}</span>
        ${mine ? '<button class="cmdel" aria-label="Delete this comment">&times;</button>' : ''}</div>
      <div class="cmbody md"${mine ? ' tabindex="0" role="button" aria-label="Your comment. Press Enter to edit."' : ''}>${md.render(c.body)}</div>
      <textarea class="cmedit" rows="3" hidden aria-label="Edit your comment"></textarea>
    </div>`;
  }).join('');

  rows.forEach((c) => {
    if (c.account_id !== S.me) return;
    const row = box.querySelector(`.cmrow[data-id="${c.id}"]`);
    const body = row.querySelector('.cmbody');
    const edit = row.querySelector('.cmedit');
    const open = (on) => {
      body.hidden = on;
      edit.hidden = !on;
      if (!on) return;
      edit.value = c.body;
      edit.focus();
      edit.setSelectionRange(edit.value.length, edit.value.length);
    };
    // Named commit, and not save: save() is the local database at the top of
    // this file, and a shadow of it here would be a trap for the next reader.
    const commit = () => {
      const v = edit.value.trim();
      if (v && v !== c.body) {
        c.body = v;
        queue('comment_update', { id: c.id, body: v });
      }
      open(false);
      drawTalk(el, t);
    };
    body.onclick = (e) => { if (!e.target.closest('a')) open(true); };
    body.onkeydown = (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(true); } };
    edit.onblur = commit;
    edit.onkeydown = (e) => {
      if (e.key === 'Escape' || (e.key === 'Enter' && (e.metaKey || e.ctrlKey))) {
        e.preventDefault();
        e.stopPropagation(); // Escape leaves the comment, not the whole panel
        commit();
      }
    };
    md.wirePasteLink(edit);
    row.querySelector('.cmdel').onclick = () => {
      S.comments.delete(c.id);
      queue('comment_delete', { id: c.id });
      drawTalk(el, t);
      render();
    };
  });
}

// sectionRow draws the section picker, and nothing at all in a project with no
// sections. The board files a task by dragging it, and this is the way that
// works with one hand: shopping mode has no drag, and a phone has no room for
// one.
function sectionRow(t) {
  const secs = projectSections(t.project_id);
  if (!secs.length) return '';
  const opts = ['<option value="">No section</option>'].concat(secs.map((x) =>
    `<option value="${esc(x.id)}"${t.section_id === x.id ? ' selected' : ''}>${esc(x.name)}</option>`));
  return `<div class="d-row"><span class="d-lab">Section</span>
    <select class="d-section">${opts.join('')}</select></div>`;
}

// assigneeRow draws the assignee picker, and nothing at all when the list
// reaches one person. A field that always says "me" is a field in the way.
function assigneeRow(t) {
  const holders = sharedWith(t.project_id);
  if (!holders.length || S.people.length < 2) return '';
  // Me, and everybody the list is shared with. A list somebody shared with me
  // already holds me among the members, so the set is made unique by id.
  const me = S.people.find((p) => p.is_me);
  const seen = new Set();
  const people = [me].concat(holders).filter((p) => {
    if (!p || seen.has(p.id)) return false;
    seen.add(p.id);
    return true;
  });
  const opts = ['<option value="">Nobody</option>'].concat(people.map((p) =>
    `<option value="${esc(p.id)}"${t.assignee_id === p.id ? ' selected' : ''}>${esc(p.name)}</option>`));
  return `<div class="d-row"><span class="d-lab">Who</span>
    <select class="d-assignee">${opts.join('')}</select></div>`;
}

function closeDetail() {
  const el = document.querySelector('.sheet');
  if (el) el.remove();
  S.detail = null;
}

// --- notifications ----------------------------------------------------------
// Web Push with VAPID, per docs/DECISIONS.md D-003. The browser asks its own
// vendor's push service for an endpoint, the server keeps that endpoint with
// two keys, and internal/push sends to it when a reminder comes due.
//
// Permission is asked inside a button press and never on load. A prompt that
// arrives out of the blue gets "block", and a blocked site cannot ask again.

const PUSH = {
  supported: 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window,
  key: '',         // the server's VAPID public key
  serverOn: false, // the server holds a keypair
  sub: null,       // this browser's subscription, if any
  devices: 0,
};

// urlB64ToBytes turns the base64url public key into the Uint8Array that
// pushManager.subscribe wants.
function urlB64ToBytes(s) {
  const pad = '='.repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

async function readPushState() {
  if (!PUSH.supported) return PUSH;
  try {
    const res = await fetch('/v1/push/key');
    if (res.ok) {
      const d = await res.json();
      PUSH.key = d.key || '';
      PUSH.serverOn = !!d.enabled;
      PUSH.devices = d.devices || 0;
    }
  } catch (e) { /* offline: the panel says what it knows */ }
  try {
    const reg = await navigator.serviceWorker.ready;
    PUSH.sub = await reg.pushManager.getSubscription();
  } catch (e) { PUSH.sub = null; }
  return PUSH;
}

// subscribePush asks for permission and registers this browser. It returns an
// empty string on success, or one sentence a person can act on.
async function subscribePush() {
  if (!PUSH.supported) return 'This browser cannot receive push notifications.';
  if (!PUSH.serverOn) return 'This server holds no VAPID key, so it cannot send notifications.';
  const allowed = await Notification.requestPermission();
  if (allowed !== 'granted') return 'The browser did not allow notifications for this site.';
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlB64ToBytes(PUSH.key),
    });
    const res = await fetch('/v1/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(sub),
    });
    if (!res.ok) return 'The server refused the subscription.';
    PUSH.sub = sub;
    return '';
  } catch (e) {
    return 'The browser could not subscribe: ' + e.message;
  }
}

// unsubscribePush drops this device. The server row goes first, because a row
// with no browser behind it is what makes a push service answer 410.
async function unsubscribePush() {
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (!sub) { PUSH.sub = null; return ''; }
    await fetch('/v1/push/unsubscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint: sub.endpoint }),
    });
    await sub.unsubscribe();
    PUSH.sub = null;
    return '';
  } catch (e) {
    return 'Could not unsubscribe: ' + e.message;
  }
}

async function testPush() {
  try {
    const res = await fetch('/v1/push/test', { method: 'POST' });
    if (!res.ok) return 'The server could not send a test.';
    const d = await res.json();
    return d.sent ? '' : 'No device is subscribed yet.';
  } catch (e) {
    return 'Could not reach the server.';
  }
}

// --- reminders --------------------------------------------------------------
// A reminder is a row like any other: it syncs, and every client sees it. The
// browser owns the time zone, so the browser computes the exact moment and
// sends the server UTC.

function remindersFor(id) {
  return [...S.reminders.values()].filter((r) => r.task_id === id && !r.deleted_at);
}

// remindOffset says how a task's reminder is set, as minutes before the due
// moment. It returns null when the task has none.
function remindOffset(t) {
  const r = remindersFor(t.id)[0];
  if (!r) return null;
  return r.kind === 'before_due' ? (r.offset_min || 0) : 0;
}


// fireMoment turns the due day, the due time and an offset into one moment.
// A task with a day but no time counts as 09:00, because a reminder at
// midnight is a reminder nobody reads.
function fireMoment(t, offsetMin) {
  if (!t.due_date) return null;
  const at = new Date(t.due_date + 'T' + (t.due_time || '09:00'));
  if (isNaN(at.getTime())) return null;
  at.setMinutes(at.getMinutes() - (offsetMin || 0));
  return at.toISOString();
}

const REMIND_CHOICES = [
  ['', 'No reminder'],
  ['0', 'At the due time'],
  ['10', '10 minutes before'],
  ['30', '30 minutes before'],
  ['60', '1 hour before'],
  ['1440', '1 day before'],
];

// remindWord says an offset in minutes the way a person says it.
function remindWord(min) {
  const n = Math.abs(min);
  if (n % 1440 === 0) return (n / 1440) + (n === 1440 ? ' day' : ' days');
  if (n % 60 === 0) return (n / 60) + (n === 60 ? ' hour' : ' hours');
  return n + ' minutes';
}

// remindLabel names one choice in the list.
function remindLabel(min) {
  if (min === 0) return 'At the due time';
  return remindWord(min) + (min > 0 ? ' before' : ' after');
}

// remindChoices is the list for one task. Quick add can write an offset that
// is not on the standard list, and a select that cannot show the value it
// holds is a select that loses it on the next change.
function remindChoices(t) {
  const min = remindOffset(t);
  const list = REMIND_CHOICES.slice();
  if (min !== null && min !== 0 && !list.some(([v]) => v === String(min))) {
    list.push([String(min), remindLabel(min)]);
    list.sort((a, b) => (Number(a[0] === '' ? -1e9 : a[0]) - Number(b[0] === '' ? -1e9 : b[0])));
  }
  return list;
}

// setReminder replaces every reminder of one task. One task carries one
// reminder here: two notifications for one chore is noise, not care.
function setReminder(t, choice) {
  remindersFor(t.id).forEach((r) => {
    S.reminders.delete(r.id);
    queue('reminder_delete', { id: r.id });
  });
  if (choice === '') { render(); return; }
  const min = Number(choice) || 0;
  const at = fireMoment(t, min);
  if (!at) { render(); return; }
  const id = newId('r');
  // A negative offset is a reminder after the due moment. Quick add can write
  // one, for example a task due at 9 and "remind me at 10", so the kind reads
  // "not at the due moment" rather than "before it".
  const kind = min !== 0 ? 'before_due' : 'at_due';
  S.reminders.set(id, { id, task_id: t.id, kind, fire_at: at, offset_min: min || undefined, v: 0 });
  queue('reminder_add', { id, task_id: t.id, kind, fire_at: at, offset_min: min || undefined });
  render();
}

// reArm moves the reminders of a task after its due day or time changed. A
// reminder that still points at last week fires never, and the person would
// not know why.
function reArm(t) {
  remindersFor(t.id).forEach((r) => {
    if (r.kind === 'daily_digest') return;
    if (!t.due_date) {
      S.reminders.delete(r.id);
      queue('reminder_delete', { id: r.id });
      return;
    }
    const at = fireMoment(t, r.kind === 'before_due' ? (r.offset_min || 0) : 0);
    if (!at || at === r.fire_at) return;
    r.fire_at = at;
    queue('reminder_update', { id: r.id, fire_at: at });
  });
}

function digestReminder() {
  return [...S.reminders.values()].find((r) => r.kind === 'daily_digest' && !r.deleted_at) || null;
}

function digestTime() {
  const d = digestReminder();
  if (!d) return '';
  const at = new Date(d.fire_at);
  if (isNaN(at.getTime())) return '';
  return String(at.getHours()).padStart(2, '0') + ':' + String(at.getMinutes()).padStart(2, '0');
}

// setDigest arms one digest at a clock time. The next occurrence of that time
// is the first one, and the server steps it one day forward after each send.
function setDigest(hhmm) {
  [...S.reminders.values()].filter((r) => r.kind === 'daily_digest').forEach((r) => {
    S.reminders.delete(r.id);
    queue('reminder_delete', { id: r.id });
  });
  if (!hhmm) { render(); return; }
  const [h, m] = hhmm.split(':').map(Number);
  const at = new Date();
  at.setHours(h || 0, m || 0, 0, 0);
  if (at.getTime() <= Date.now()) at.setDate(at.getDate() + 1);
  const id = newId('r');
  const fire = at.toISOString();
  S.reminders.set(id, { id, kind: 'daily_digest', fire_at: fire, v: 0 });
  queue('reminder_add', { id, kind: 'daily_digest', fire_at: fire });
  render();
}

// --- settings ---------------------------------------------------------------

// enrolMessage says what to do about a refused enrolment. The server asks for
// the device token here, so a browser that only holds a passkey session has to
// paste the token once.
function enrolMessage(e) {
  if (e && e.status === 401) {
    return 'Paste the device token on the sign-in page first. The token is the invitation into this account.';
  }
  return pk.message(e);
}

async function drawPasskeys(host, errHost) {
  try {
    const rows = await pk.list();
    if (!rows.length) {
      host.innerHTML = '<div class="d-sub">No passkey yet. Add one to sign in without the token.</div>';
      return;
    }
    host.innerHTML = rows.map((r) => `<div class="pk" data-id="${esc(r.id)}">`
      + `<span>${esc(r.name)}</span>`
      + `<span class="w">${r.last_used_at ? 'last used ' + esc(r.last_used_at.slice(0, 10)) : 'never used'}</span>`
      + `<button class="rm">Remove</button></div>`).join('');
    [...host.querySelectorAll('.pk')].forEach((row) => {
      row.querySelector('.rm').onclick = async () => {
        errHost.textContent = '';
        try {
          await pk.remove(row.dataset.id);
          drawPasskeys(host, errHost);
        } catch (e) { errHost.textContent = pk.message(e); }
      };
    });
  } catch (e) {
    host.innerHTML = '<div class="d-sub">Cannot read the passkeys.</div>';
    errHost.textContent = pk.message(e);
  }
}


// openSettings holds the passkey area, the notification controls and the daily
// digest. A passkey signs this browser in without the device token. The token
// stays: the phone, the command line client and MCP all use it, and the server
// asks for it before it enrols a passkey.
// openHistory shows the activity log: the whole household, or one list when a
// project view is open.
//
// It is a sheet and not a view, because the log is not a list of tasks and
// nothing in it can be edited. It also needs the network, which every other
// screen here does not, so it must be a place a person goes on purpose.
function openHistory() {
  const open = document.querySelector('.sheet');
  closeDetail();
  if (open) return;
  const project = currentProject();
  const el = document.createElement('div');
  el.className = 'sheet';
  el.innerHTML = `<div class="card set" role="dialog" aria-label="History">
    <h3>History${project ? ' &middot; ' + esc(project.name) : ''}</h3>
    <div class="note">Who did what. The server holds this log, so it is the one
      screen here that needs the network.</div>
    <div class="aclist"></div>
    <div class="d-actions">
      <span class="d-hint">Escape closes.</span>
      <button class="d-close">Done</button>
    </div>
  </div>`;
  document.body.appendChild(el);
  S.detail = 'history';
  el.querySelector('.d-close').onclick = closeDetail;
  el.onclick = (e) => { if (e.target === el) closeDetail(); };
  act.draw(el.querySelector('.aclist'), { project: project ? project.id : '' }, logContext());
}

// logContext is what activity.js needs from this file and must not own: the
// markup escape, who a person is, and the three things a log line can do.
function logContext() {
  return {
    esc,
    me: S.me,
    personName,
    // known says whether this browser holds the task, which is the test for
    // offering to open it. A deleted row leaves the local copy, so a task the
    // log names and this map does not is a task that went away.
    known: (id) => S.tasks.has(id),
    gone: (action, id) => (action === 'task_delete' ? !S.tasks.has(id)
      : action === 'project_delete' ? !S.projects.has(id)
        : action === 'section_delete' ? !S.sections.has(id) : false),
    restore: (cmd, id) => {
      // The row is not in the local copy, so there is nothing to put back
      // here. The command goes to the server and the next pull brings the row.
      queue(cmd, { id });
      sync();
    },
    open: (id) => {
      const t = S.tasks.get(id);
      if (!t) return;
      closeDetail();
      openDetail(t);
    },
  };
}

async function openSettings() {
  // closeDetail removes any open sheet, the task detail included, and clears
  // the detail flag. A second press of the same button then closes this one.
  const open = document.querySelector('.sheet');
  closeDetail();
  if (open) return;
  const el = document.createElement('div');
  el.className = 'sheet';
  el.innerHTML = `<div class="card set" role="dialog" aria-label="Settings">
    <h3>Settings</h3>
    <div class="grp">Passkeys</div>
    <div class="note">A passkey signs this browser in with a fingerprint, a face or a PIN.
      The device token keeps working, and every native client uses it.</div>
    <div class="d-subs" id="pk-list"><div class="d-sub">Reading&hellip;</div></div>
    <div class="d-row" id="pk-add-row">
      <input id="pk-name" placeholder="Name this passkey, for example Phone" maxlength="60">
      <button id="pk-add">Add</button>
    </div>
    <div class="d-err" id="pk-err"></div>
    <div class="grp">Notifications on this device</div>
    <div class="state" id="set-state">Checking&hellip;</div>
    <div class="row" id="set-row"></div>
    <div class="note" id="set-note">Every device subscribes once, the browser and the installed app alike.</div>
    <div class="grp">Daily digest</div>
    <div class="row">
      <input type="time" id="set-dtime" value="${digestTime() || '08:00'}" aria-label="Digest time">
      <button id="set-don" class="go">${digestReminder() ? 'Change' : 'Turn on'}</button>
      ${digestReminder() ? '<button id="set-doff">Turn off</button>' : ''}
    </div>
    <div class="note">One notification each morning, with what is due that day.</div>
    <div class="grp">The household</div>
    <div class="d-subs" id="hh-list"><div class="d-sub">Reading&hellip;</div></div>
    <div class="d-row" id="hh-add-row">
      <input id="hh-name" placeholder="Invite somebody: their name" maxlength="60">
      <button id="hh-add">Invite</button>
    </div>
    <div class="d-err" id="hh-err"></div>
    <div class="note" id="hh-note">An invitation is one code, good for seven days and for one
      person. Share a list with them from the &#8943; button beside it in the sidebar.</div>
    <div class="d-actions">
      <span class="d-hint">Escape closes.</span>
      <button class="d-del" id="pk-out">Sign out</button>
      <button class="d-close">Done</button>
    </div>
  </div>`;
  document.body.appendChild(el);
  S.detail = 'settings';

  const q = (sel) => el.querySelector(sel);
  q('.d-close').onclick = closeDetail;
  el.onclick = (e) => { if (e.target === el) closeDetail(); };
  q('#set-don').onclick = () => { setDigest(q('#set-dtime').value); closeDetail(); openSettings(); };
  if (q('#set-doff')) q('#set-doff').onclick = () => { setDigest(''); closeDetail(); openSettings(); };

  // --- the passkey half
  if (!pk.supported()) {
    q('#pk-add-row').hidden = true;
    q('#pk-err').textContent = 'This browser cannot make a passkey. Use the device token.';
  }
  q('#pk-add').onclick = async () => {
    const btn = q('#pk-add');
    q('#pk-err').textContent = '';
    btn.disabled = true;
    try {
      await pk.enrol(q('#pk-name').value.trim());
      q('#pk-name').value = '';
      drawPasskeys(q('#pk-list'), q('#pk-err'));
    } catch (e) {
      q('#pk-err').textContent = enrolMessage(e);
    }
    btn.disabled = false;
  };
  q('#pk-out').onclick = async () => {
    try { await pk.signOut(); } catch (e) { /* the cookie is gone either way */ }
    location.href = '/login';
  };
  drawPasskeys(q('#pk-list'), q('#pk-err'));

  // --- the household half
  drawHousehold(q('#hh-list'), q('#hh-err'));
  q('#hh-add').onclick = async () => {
    const btn = q('#hh-add');
    const name = q('#hh-name').value.trim();
    q('#hh-err').textContent = '';
    if (!name) { q('#hh-err').textContent = 'Give the invitation a name, so two of them can be told apart.'; return; }
    btn.disabled = true;
    try {
      const res = await fetch('/v1/invites', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      const d = await res.json();
      if (!res.ok) throw new Error(d.error || 'the server refused');
      q('#hh-name').value = '';
      // The code is shown once and never again. The server keeps its hash.
      q('#hh-note').innerHTML = `Give <b>${esc(name)}</b> this code. It works once, and it is`
        + ` shown here once:<br><code class="code">${esc(d.code)}</code>`;
      await readHousehold();
      drawHousehold(q('#hh-list'), q('#hh-err'));
    } catch (e) {
      q('#hh-err').textContent = String(e.message || e);
    }
    btn.disabled = false;
  };

  // --- the notification half
  const paint = (message) => {
    const state = q('#set-state');
    const row = q('#set-row');
    row.innerHTML = '';
    const button = (label, primary, run) => {
      const b = document.createElement('button');
      b.textContent = label;
      if (primary) b.className = 'go';
      b.onclick = async () => { b.disabled = true; paint(await run()); };
      row.appendChild(b);
    };
    if (!PUSH.supported) {
      state.textContent = 'This browser cannot receive push notifications.';
      return;
    }
    if (!PUSH.serverOn) {
      state.textContent = 'This server holds no VAPID key, so it sends no notifications.';
      q('#set-note').textContent = 'The operator runs teha -vapid-keys once, then sets the two variables.';
      return;
    }
    if (Notification.permission === 'denied') {
      state.className = 'state';
      state.textContent = 'The browser blocked notifications for this site.';
      q('#set-note').textContent = 'Allow them in the browser site settings, then open this panel again.';
      return;
    }
    if (PUSH.sub) {
      state.className = 'state on';
      state.textContent = 'Notifications are on for this device.';
      button('Send a test', true, testPush);
      button('Turn off', false, unsubscribePush);
    } else {
      state.className = 'state';
      state.textContent = 'Notifications are off for this device.';
      // The permission prompt happens inside this click. That is the only
      // moment a browser accepts it, and the only moment a person expects it.
      button('Turn on notifications', true, subscribePush);
    }
    if (message) q('#set-note').textContent = message;
  };

  await readPushState();
  paint('');
}

// drawHousehold lists the people and the invitations that are still open.
// Only the owner can read the invitations, so a member simply sees the people.
async function drawHousehold(host, errHost) {
  const people = S.people.length ? S.people : [{ id: S.me, name: 'you', is_me: true, is_owner: true }];
  const rows = people.map((p) => `<div class="pk"><span>${esc(p.name)}</span>`
    + `<span class="w">${p.is_owner ? 'owner' : 'invited'}${p.is_me ? ', this is you' : ''}</span></div>`);
  host.innerHTML = rows.join('');

  try {
    const res = await fetch('/v1/invites');
    if (!res.ok) return; // a member cannot read them, and does not need to
    const d = await res.json();
    const open = (d.invites || []).filter((i) => !i.used_at);
    if (!open.length) return;
    host.innerHTML = rows.concat(open.map((i) => `<div class="pk"><span>${esc(i.name)}</span>`
      + `<span class="w">invited, not joined yet</span>`
      + `<button class="rm" data-invite="${esc(i.id)}">Revoke</button></div>`)).join('');
    [...host.querySelectorAll('[data-invite]')].forEach((b) => {
      b.onclick = async () => {
        b.disabled = true;
        try {
          await fetch('/v1/invites/revoke', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: b.dataset.invite }),
          });
          drawHousehold(host, errHost);
        } catch (e) {
          if (errHost) errHost.textContent = 'The server did not answer.';
        }
      };
    });
  } catch (e) { /* offline: the people are still right */ }
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
  if (name.toLowerCase() === 'inbox') return S.projects.get(S.inbox) || null;
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

// toggleShop swaps shopping mode for the list. Like the board, it needs a
// project: a shopping list is one list, not a filter over every list.
function toggleShop() {
  if (S.layout !== 'shop' && !currentProject()) {
    $('hint').textContent = 'Shopping mode needs a project view. Pick a list in the sidebar.';
    return;
  }
  S.layout = S.layout === 'shop' ? 'list' : 'shop';
  S.sel = 0;
  save();
  render();
}

// toggleCalendar swaps the calendar for the list. The calendar needs no
// project, because a day holds tasks from every project.
function toggleCalendar() {
  S.layout = S.layout === 'calendar' ? 'list' : 'calendar';
  if (S.layout === 'calendar' && !S.cal.anchor) S.cal.anchor = todayISO();
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
    const key = bnd.keyAt(i);
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
    const key = bnd.keyAt(i);
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
  const order = bnd.keyAt(projectSections(p.id).length);
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
  // The tasks lose their section here and the server does the same, so the
  // local database has to hear about them: a section_delete command names the
  // section and not the tasks it emptied.
  ids.forEach((id) => {
    delete S.tasks.get(id).section_id;
    touch('tasks', id, S.tasks.get(id));
  });
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

// --- shopping mode ----------------------------------------------------------
// A list read in a shop, at arm's length, with one hand, beside the shop's own
// app in split screen. PLAN.md section 4 asks for five things and this is all
// five: items grouped by category, big check targets, recently bought
// suggestions, a quantity in the title, and the basket that clears on request.
//
// No new table. A category is a section of the project, which is already a
// heading a person can rename and drag, and PLAN.md section 4 says loose
// categories and not a store map. The history is the completed tasks of the
// list, which the account already holds.
//
// Live sync needs nothing here either. The event stream wakes a pull and the
// pull calls render, so the other person's tick arrives on its own.

function shopOn() { return S.layout === 'shop' && !!currentProject(); }

// QTY reads a count in front of an item: "2x milk", "2 × milk". The x is
// required. "20 minutes of stretching" is a task and not twenty minutes.
const QTY = /^(\d{1,3})\s*[x\u00d7]\s*(.+)$/i;

function itemParts(title) {
  const m = QTY.exec((title || '').trim());
  return m ? { qty: m[1], name: m[2].trim() } : { qty: '', name: (title || '').trim() };
}

// normal is the key that "Milk", "milk" and "2x milk" share, so the history
// knows them as one item.
function normal(title) {
  return itemParts(title).name.toLowerCase().replace(/\s+/g, ' ');
}

function shopOpen(projectId) {
  return [...S.tasks.values()].filter((t) => t.project_id === projectId
    && t.state === 'open' && !t.deleted_at);
}

// learnedSection guesses the aisle of a new item from what the household
// bought before. The newest task with the same name and a section wins.
//
// This is the whole of "learned from history": no model, no table, and a
// person can always drag the item somewhere else, which teaches the next one.
function learnedSection(projectId, title) {
  const key = normal(title);
  if (!key) return null;
  const past = [...S.tasks.values()]
    .filter((t) => t.project_id === projectId && t.section_id && normal(t.title) === key)
    .sort((a, b) => (b.completed_at || b.id).localeCompare(a.completed_at || a.id));
  return past.length ? past[0].section_id : null;
}

// shopAisles groups what is still to buy. An empty aisle is not drawn: a list
// read at arm's length must hold no headings with nothing under them.
function shopAisles() {
  const p = currentProject();
  if (!p) return [];
  const groups = [{ id: null, name: 'Anything else', items: [] }]
    .concat(projectSections(p.id).map((x) => ({ id: x.id, name: x.name, items: [] })));
  const at = new Map(groups.map((g, i) => [g.id, i]));
  shopOpen(p.id).forEach((t) => {
    const key = t.section_id || null;
    groups[at.has(key) ? at.get(key) : 0].items.push(t);
  });
  groups.forEach((g) => g.items.sort(boardOrder));
  return groups.filter((g) => g.items.length);
}

// basket lists what went in on this trip. A ticked item stays on the screen,
// so a wrong tap is one tap back, until somebody empties the basket.
//
// A trip is hours, not years. Without the window a list that has been used for
// a year would open with a year of shopping in the basket, so the window is
// what the layout means by "the basket" and the clear button is what a person
// means by "done with that".
const TRIP = 12 * 3600 * 1000;

function basket() {
  const p = currentProject();
  if (!p) return [];
  const trip = new Date(Date.now() - TRIP).toISOString();
  const cleared = S.shopClear[p.id] || '';
  const since = cleared > trip ? cleared : trip;
  return [...S.tasks.values()]
    .filter((t) => t.project_id === p.id && t.state !== 'open' && !t.deleted_at
      && (t.completed_at || '') > since)
    .sort((a, b) => (b.completed_at || '').localeCompare(a.completed_at || ''));
}

// recentItems suggests what the household buys and has not got on the list
// now. One row per name, newest first.
function recentItems(projectId, limit = 10) {
  const already = new Set(shopOpen(projectId).map((t) => normal(t.title)));
  const seen = new Set();
  const out = [];
  [...S.tasks.values()]
    .filter((t) => t.project_id === projectId && t.state !== 'open' && t.completed_at)
    .sort((a, b) => (b.completed_at || '').localeCompare(a.completed_at || ''))
    .forEach((t) => {
      const key = normal(t.title);
      if (!key || already.has(key) || seen.has(key)) return;
      seen.add(key);
      out.push(t);
    });
  return out.slice(0, limit);
}

// addItem puts one item on the list of the project on screen, in the aisle the
// history suggests.
function addItem(projectId, text) {
  const title = text.trim();
  if (!title) return;
  const id = newId('t');
  const section = learnedSection(projectId, title);
  S.tasks.set(id, {
    id, project_id: projectId, section_id: section || undefined, order_key: 'm',
    title, priority: 4, state: 'open', labels: [], v: 0,
  });
  const args = { id, project_id: projectId, title };
  if (section) args.section_id = section;
  queue('task_add', args);
}

function clearBasket(projectId) {
  S.shopClear[projectId] = new Date().toISOString();
  S.sel = 0;
  save();
  render();
}

// shopRowHTML draws one item. The quantity is a chip of its own, because a
// hand holding a trolley reads "2" faster than it reads "2x milk".
//
// The markup keeps .row, .box and .body, so the cursor, the marks and every
// key work here through the one wiring that render() does for every layout.
function shopRowHTML(t, i) {
  const cls = ['row', 'shoprow'];
  if (i === S.sel) cls.push('sel');
  if (S.marked.has(t.id)) cls.push('mk');
  if (t.state !== 'open') cls.push('done');
  const { qty, name } = itemParts(t.title);
  const meta = [];
  if (t.assignee_id && t.assignee_id !== S.me) {
    meta.push(`<span class="who">${esc(personName(t.assignee_id))}</span>`);
  }
  const talk = commentsOn(t.id).length;
  if (talk) meta.push(`<span class="cm">&#128172; ${talk}</span>`);
  if ((t.description || '').trim()) meta.push('<span class="nt" title="This item has a note">&#9998;</span>');
  return `<div class="${cls.join(' ')}"><button class="box" aria-label="In the basket"></button>
    <div class="body"><div class="t">${qty ? `<span class="qty">${esc(qty)}</span>` : ''}${md.inline(name)}</div>
    ${meta.length ? `<div class="meta">${meta.join('')}</div>` : ''}</div></div>`;
}

function renderShop(el) {
  const p = currentProject();
  const aisles = shopAisles();
  const bag = basket();
  const recent = recentItems(p.id);
  let i = 0;
  const aisleHTML = aisles.map((g) => `<div class="aisle">
    <div class="ahead">${esc(g.name)}</div>
    ${g.items.map((t) => shopRowHTML(t, i++)).join('')}</div>`).join('');
  const bagHTML = bag.length ? `<div class="bag">
    <div class="ahead">In the basket <span class="n">${bag.length}</span>
      <button class="clearbag">Clear</button></div>
    ${bag.map((t) => shopRowHTML(t, i++)).join('')}</div>` : '';

  el.innerHTML = `<div class="shop">
    <input class="shopadd" placeholder="Add an item, then press Enter" autocomplete="off" spellcheck="false"
           aria-label="Add an item">
    ${aisles.length ? aisleHTML : '<div class="empty">Nothing to buy. Type in the box above.</div>'}
    ${recent.length ? `<div class="recent"><div class="ahead">Bought before</div>
      ${recent.map((t) => `<button class="sug" data-title="${esc(t.title)}">${esc(itemParts(t.title).name)}</button>`).join('')}
      </div>` : ''}
    ${bagHTML}
  </div>`;

  const box = el.querySelector('.shopadd');
  box.onkeydown = (e) => {
    if (e.key !== 'Enter') return;
    addItem(p.id, box.value);
    box.value = '';
    render();
    // The field keeps the focus: a person in a shop types three items in a
    // row, and a field that loses the caret after each one is a field that
    // costs three taps.
    const again = document.querySelector('.shopadd');
    if (again) again.focus();
  };
  [...el.querySelectorAll('.sug')].forEach((b) => {
    b.onclick = () => { addItem(p.id, b.dataset.title); render(); };
  });
  const clear = el.querySelector('.clearbag');
  if (clear) clear.onclick = () => clearBasket(p.id);
}

// --- calendar layout --------------------------------------------------------
// A month or a week, with the tasks of the current view on their due days.
// Drag a task to another day to move it, and drag one out of the strip below
// to give an undated task a day. The strip is what makes the layout a planning
// tool rather than a picture.
//
// The rule of the board holds here too: visible() returns the same flat list,
// day after day and then the strip, so every key and every bulk action works.

function calendarOn() { return S.layout === 'calendar'; }

// addDays counts in days on the calendar, never in milliseconds. A day is not
// always 24 hours long, and a clock change must not move a due date.
function addDays(dayISO, n) {
  const d = new Date(dayISO + 'T00:00:00Z');
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

function weekdayOf(dayISO) { return new Date(dayISO + 'T00:00:00Z').getUTCDay(); }

// mondayOf returns the Monday on or before a day. The week starts on Monday,
// which is what the two people who use this app read.
function mondayOf(dayISO) {
  return addDays(dayISO, -((weekdayOf(dayISO) + 6) % 7));
}

function calAnchor() { return S.cal.anchor || todayISO(); }

// calWindow says which days the calendar shows, and what to call them.
function calWindow() {
  const anchor = calAnchor();
  if (S.cal.mode === 'week') {
    const start = mondayOf(anchor);
    return { start, weeks: 1, title: `${dayWord(start)} to ${dayWord(addDays(start, 6))}` };
  }
  const first = anchor.slice(0, 8) + '01';
  const start = mondayOf(first);
  const next = addDays(first.slice(0, 8) + '28', 7).slice(0, 8) + '01';
  const days = Math.round((Date.parse(next) - Date.parse(first)) / 86400000);
  const lead = Math.round((Date.parse(first) - Date.parse(start)) / 86400000);
  return {
    start,
    weeks: Math.ceil((lead + days) / 7),
    title: new Date(first + 'T00:00').toLocaleDateString(undefined, { month: 'long', year: 'numeric' }),
    month: first.slice(0, 7),
  };
}

// calendarCells puts the rows of the view on their days. A task due outside
// the window is counted, never hidden without a word.
function calendarCells() {
  const w = calWindow();
  const days = [];
  const at = new Map();
  for (let i = 0; i < w.weeks * 7; i++) {
    const date = addDays(w.start, i);
    const cell = { date, tasks: [], out: w.month ? date.slice(0, 7) !== w.month : false };
    at.set(date, cell);
    days.push(cell);
  }
  const undated = [];
  let outside = 0;
  for (const t of queryRows()) {
    if (!t.due_date) { undated.push(t); continue; }
    const cell = at.get(t.due_date);
    if (cell) cell.tasks.push(t);
    else outside++;
  }
  days.forEach((c) => c.tasks.sort(dayOrder));
  undated.sort(dayOrder);
  return { days, undated, outside, window: w };
}

// dayOrder sorts one day: a task with a time first, in clock order, then the
// rest by priority and title.
function dayOrder(a, b) {
  const at = a.due_time || '99:99', bt = b.due_time || '99:99';
  if (at !== bt) return at < bt ? -1 : 1;
  if ((a.priority || 4) !== (b.priority || 4)) return (a.priority || 4) - (b.priority || 4);
  return (a.title || '').localeCompare(b.title || '');
}

// calStep moves the window by one period, and calToday brings it back.
function calStep(n) {
  const a = calAnchor();
  if (S.cal.mode === 'week') S.cal.anchor = addDays(a, 7 * n);
  else {
    const d = new Date(a.slice(0, 8) + '01T00:00:00Z');
    d.setUTCMonth(d.getUTCMonth() + n);
    S.cal.anchor = d.toISOString().slice(0, 10);
  }
  S.sel = 0;
  save();
  render();
}

function calToday() {
  S.cal.anchor = todayISO();
  S.sel = 0;
  save();
  render();
}

function calMode(mode) {
  S.cal.mode = mode;
  S.sel = 0;
  save();
  render();
}

// --- the calendar, drawn ----------------------------------------------------

const DOW = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

function renderCalendar(list) {
  const cal = calendarCells();
  const today = todayISO();
  let i = 0;

  const head = `<div class="cal-head">
    <button class="cal-nav" data-cal="prev" aria-label="The period before">&#8249;</button>
    <button class="cal-nav" data-cal="today">Today</button>
    <button class="cal-nav" data-cal="next" aria-label="The period after">&#8250;</button>
    <span class="cal-title">${esc(cal.window.title)}</span>
    <span class="cal-modes">
      <button data-cal="month" class="${S.cal.mode === 'month' ? 'on' : ''}">Month</button>
      <button data-cal="week" class="${S.cal.mode === 'week' ? 'on' : ''}">Week</button>
    </span>
    ${cal.outside ? `<span class="cal-out">${cal.outside} outside this ${S.cal.mode}</span>` : ''}
  </div>`;

  const dows = DOW.map((d) => `<div class="cal-dow">${d}</div>`).join('');
  const cells = cal.days.map((c) => {
    const cls = ['cal-day'];
    if (c.out) cls.push('out');
    if (c.date === today) cls.push('now');
    const chips = c.tasks.map((t) => chipHTML(t, i++)).join('');
    return `<div class="${cls.join(' ')}" data-day="${c.date}">
      <div class="cal-num">${Number(c.date.slice(8))}</div>${chips}</div>`;
  }).join('');

  const strip = `<div class="cal-un" data-day="">
    <div class="cal-unhead">No date<span class="c">${cal.undated.length || ''}</span></div>
    <div class="cal-unrow">${cal.undated.map((t) => chipHTML(t, i++)).join('')
      || '<span class="cal-unempty">Every task in this view has a day.</span>'}</div></div>`;

  list.innerHTML = `<div class="cal cal-${S.cal.mode}">${head}
    <div class="cal-grid">${dows}${cells}</div>${strip}</div>`;
  wireCalendar(list, cal);
}

// chipHTML draws one task on a day. It carries the same classes a row does, so
// the cursor, the pick and the priority colour all read the same.
function chipHTML(t, i) {
  const cls = ['row', 'chip'];
  if (i === S.sel) cls.push('sel');
  if (S.marked.has(t.id)) cls.push('mk');
  if (t.state === 'done') cls.push('done');
  if ((t.priority || 4) < 4) cls.push('p' + t.priority);
  const time = t.due_time ? `<span class="chip-at">${esc(t.due_time)}</span>` : '';
  return `<div class="${cls.join(' ')}" title="${esc(t.title)}">${time}`
    + `<span class="t">${md.inline(t.title)}</span></div>`;
}

function wireCalendar(list, cal) {
  const flat = cal.days.flatMap((c) => c.tasks).concat(cal.undated);

  [...list.querySelectorAll('.cal-head button')].forEach((b) => {
    const act = {
      prev: () => calStep(-1), next: () => calStep(1), today: calToday,
      month: () => calMode('month'), week: () => calMode('week'),
    }[b.dataset.cal];
    b.onclick = (e) => { e.stopPropagation(); act(); };
  });

  [...list.querySelectorAll('.chip')].forEach((el, i) => {
    const t = flat[i];
    el.draggable = true;
    el.ondragstart = (e) => {
      S.drag = { kind: 'task', id: t.id };
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', t.id);
    };
    el.ondragend = () => { S.drag = null; };
    el.onclick = (ev) => {
      ev.stopPropagation();
      S.sel = i;
      if (ev.metaKey || ev.ctrlKey) { S.anchor = i; toggleMark(t); return; }
      if (ev.shiftKey) { markRange(i); return; }
      if (ev.target.closest('a')) { render(); return; }
      S.anchor = i;
      openDetail(t);
    };
  });

  [...list.querySelectorAll('[data-day]')].forEach((el) => {
    el.ondragover = (e) => {
      if (!S.drag || S.drag.kind !== 'task') return;
      e.preventDefault();
      el.classList.add('over');
    };
    el.ondragleave = () => el.classList.remove('over');
    el.ondrop = (e) => {
      e.preventDefault();
      el.classList.remove('over');
      const d = S.drag;
      S.drag = null;
      if (!d || d.kind !== 'task') return;
      const t = S.tasks.get(d.id);
      if (!t) return;
      const to = el.dataset.day;
      // The strip has no day, so a drop there takes the date off.
      if ((t.due_date || '') === to) { render(); return; }
      const was = { due: t.due_date || null, time: t.due_time || null };
      setDue(t, to || null, to ? t.due_time : null);
      toast(to ? `Moved to ${whenWord(to)}` : 'Took the date off', () => {
        setDue(t, was.due, was.time);
        render();
      });
      render();
    };
  });
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
    if (p.remindAt) bits.push('reminder <b>' + esc(p.remindAt) + '</b>');
    if (p.remindBefore) bits.push('reminder <b>' + remindWord(p.remindBefore) + ' before</b>');
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
  // The filter field takes any query the grammar knows, the same grammar the
  // server compiles and the phone runs.
  const filt = $('filter');
  filt.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      const q = filt.value.trim();
      S.view = { q, title: q || 'Today' };
      if (!q) S.view = { q: 'today', title: 'Today' };
      S.sel = 0;
      render();
    } else if (e.key === 'Escape') {
      filt.value = S.view.q;
      filt.blur();
    }
  });

  $('fab').onclick = () => qa.focus();
  $('gear').onclick = () => openSettings();
  $('layout').onclick = toggleLayout;
  $('caltoggle').onclick = toggleCalendar;
  $('shoptoggle').onclick = toggleShop;
  $('histtoggle').onclick = openHistory;

  document.addEventListener('keydown', (e) => {
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') {
      if (e.key === 'Escape') { closeDetail(); e.target.blur(); }
      return;
    }
    if (S.menu) {
      if (e.key === 'Escape') closeMenu();
      return;
    }
    const sheet = document.querySelector('.sheet');
    if (sheet && !S.detail) {
      if (e.key === 'Escape') sheet.remove();
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
    if (e.key === 'c') { toggleCalendar(); return; }
    if (e.key === 'S') { toggleShop(); return; }
    if (boardOn() && boardKey(e, cur)) return;
    // Shift+J and Shift+K move the row inside its band, the same pair the
    // board uses to move a card inside its column. A browser reports the upper
    // case letter while shift is down, and a synthesised event can carry the
    // lower case one, so both are read.
    if (listDrag()) {
      if (e.key === 'J' || (e.key === 'j' && e.shiftKey)) { nudgeRow(rows, 1); return; }
      if (e.key === 'K' || (e.key === 'k' && e.shiftKey)) { nudgeRow(rows, -1); return; }
    }
    if (calendarOn()) {
      if (e.key === '[') { calStep(-1); return; }
      if (e.key === ']') { calStep(1); return; }
    }

    // A selection changes what every action key means. One key does the same
    // thing to one task or to twenty, and the bar at the bottom says which.
    const many = S.marked.size > 0;
    const set = many ? markedRows() : [];

    switch (e.key) {
      case 'q': case 'a': e.preventDefault(); $('qa').focus(); break;
      case '/': e.preventDefault(); $('filter').select(); break;
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
      case ',': openSettings(); break;
      case '?': showKeys(); break;
      case ',': e.preventDefault(); openSettings(); break;
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
    <dt>/</dt><dd>focus the filter field</dd>
    <dt>b / c / S</dt><dd>board layout, calendar layout, shopping mode</dd>
    <dt>[ / ]</dt><dd>the period before and after, in the calendar</dd>
    <dt>j / k</dt><dd>move down and up</dd>
    <dt>J / K</dt><dd>move this task down or up inside its day and priority</dd>
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
    <dt>,</dt><dd>settings and passkeys</dd>
    <dt>g</dt><dd>go to Today</dd>
    <dt>,</dt><dd>settings and notifications</dd>
    <dt>?</dt><dd>this list</dd></dl>
    <h3>Order a day by hand</h3><dl>
    <dt>drag</dt><dd>a task above or below another task of the same day and priority</dd>
    <dt>J / K</dt><dd>the same move, from the keyboard</dd>
    <dt>1..4</dt><dd>change the priority, to leave the band a drag can move inside</dd>
    <dt>u</dt><dd>put the order back</dd></dl>
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

wire();
// The read is asynchronous now, so the first paint waits for the local
// database rather than for the network. That is one tick, and it is what buys
// the room for a decade of history.
load().then(() => {
  render();
  return sync();
}).then(() => { readHousehold(); listenEvents(); });

// A page that is going away flushes at once. Without this the last 150
// milliseconds of typing would be lost on a close, which is exactly the
// promise that the outbox exists to keep.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') cache.flushNow();
});
window.addEventListener('pagehide', () => cache.flushNow());
window.addEventListener('online', () => { S.online = true; sync(); });
window.addEventListener('offline', () => { S.online = false; render(); });
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(() => {});
  // A click on a notification reaches an open window through the worker. The
  // worker raises the window, and the app opens the task the reminder named.
  navigator.serviceWorker.addEventListener('message', (e) => {
    const m = e.data || {};
    if (m.type === 'open-task' && m.task_id) showTask(m.task_id);
  });
}

// showTask opens one task by id, whether it is in the current view or not.
function showTask(id) {
  const t = S.tasks.get(id);
  if (!t) { sync(); return; }
  const rows = visible();
  const at = rows.findIndex((r) => r.id === id);
  if (at >= 0) S.sel = at;
  render();
  openDetail(t);
}

// A notification opens /?task=<id> when no window was open. Clear the query
// afterwards, so a reload does not open the sheet again.
const opening = new URLSearchParams(location.search).get('task');
if (opening) {
  history.replaceState(null, '', location.pathname);
  setTimeout(() => showTask(opening), 0);
}
