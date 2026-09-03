// SPDX-License-Identifier: AGPL-3.0-or-later

// The activity log, read from the server one page at a time.
//
// This is the one view that does not work offline, and that is deliberate. The
// server holds the log, the client holds none of it, and a log a client cached
// would be stale the moment somebody else wrote a line. See
// internal/store/activity.go for why the table is outside sync.
//
// The store writes the fact and this file writes the sentence, exactly as
// internal/push does for a notification. See docs/DECISIONS.md D-020.

// VERBS turns an action into what a person reads. A row whose action is not
// here still draws, with the action itself as the words, so a command added to
// the server is never invisible here.
const VERBS = {
  task_add: 'added',
  task_update: 'changed',
  task_complete: 'completed',
  task_wont_do: 'gave up on',
  task_uncomplete: 'opened again',
  task_delete: 'deleted',
  task_restore: 'restored',
  task_move: 'moved',

  project_add: 'made the list',
  project_update: 'changed the list',
  project_delete: 'deleted the list',
  project_restore: 'restored the list',

  section_add: 'added the heading',
  section_update: 'renamed the heading',
  section_move: 'moved the heading',
  section_reorder: 'reordered the heading',
  section_delete: 'deleted the heading',
  section_restore: 'restored the heading',

  comment_add: 'commented on',
  comment_update: 'changed a comment on',
  comment_delete: 'deleted a comment on',

  label_add: 'added the label',
  label_delete: 'deleted the label',

  reminder_add: 'set a reminder on',
  reminder_update: 'changed a reminder on',
  reminder_delete: 'took a reminder off',

  login: 'signed in',
  login_failed: 'a sign-in failed',
  logout: 'signed out',
  passkey_add: 'added the passkey',
  passkey_delete: 'removed the passkey',
  invite_create: 'wrote an invitation for',
  invite_revoke: 'revoked an invitation',
  joined: 'joined the household',
  share: 'shared the list with',
  unshare: 'took the list back from',
};

// UNDO names the command that puts a deleted row back. A comment has none,
// because the server has no comment_restore, so the log does not offer one.
const UNDO = {
  task_delete: 'task_restore',
  project_delete: 'project_restore',
  section_delete: 'section_restore',
};

// SECURITY are the actions that belong to the audit half of the log. They read
// differently: nobody "did something to a task", and the address matters.
const SECURITY = new Set([
  'login', 'login_failed', 'logout', 'passkey_add', 'passkey_delete',
  'invite_create', 'invite_revoke', 'joined',
]);

export function verb(action) {
  return VERBS[action] || action.replace(/_/g, ' ');
}

export function isSecurity(action) {
  return SECURITY.has(action);
}

export function undoOf(action) {
  return UNDO[action] || '';
}

// fields turns "due_date,priority" into words a person reads.
const FIELDS = {
  due_date: 'the due date',
  due_time: 'the time',
  deadline: 'the deadline',
  start_date: 'the start date',
  priority: 'the priority',
  title: 'the title',
  description: 'the note',
  labels: 'the labels',
  assignee_id: 'who does it',
  section_id: 'the heading',
  project_id: 'the list',
  rrule: 'the repeat',
  duration_min: 'the duration',
};

export function detailWords(detail) {
  if (!detail) return '';
  const names = detail.split(',').filter(Boolean).map((f) => FIELDS[f] || f.replace(/_/g, ' '));
  if (!names.length) return '';
  if (names.length === 1) return names[0];
  return names.slice(0, -1).join(', ') + ' and ' + names[names.length - 1];
}

// ago says how long ago in the fewest words, the same way the comment panel
// does. A history is read as "this morning", never as a timestamp.
export function ago(stamp) {
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

/**
 * page asks the server for one page of the log.
 *
 * scope is {project, task}, and before is the cursor: the smallest seq of the
 * page just read. It throws on a failure, and the caller says so on the screen:
 * an empty list would read as "nothing happened", which is a different fact.
 */
export async function page(scope, before, limit) {
  const p = new URLSearchParams();
  if (scope.project) p.set('project', scope.project);
  if (scope.task) p.set('task', scope.task);
  if (before) p.set('before', String(before));
  if (limit) p.set('limit', String(limit));
  const res = await fetch('/v1/activity?' + p.toString());
  if (!res.ok) throw new Error('the server answered ' + res.status);
  return res.json();
}

/**
 * draw paints the log into one element, and keeps painting as the reader asks
 * for more.
 *
 * ctx carries what this module must not own: esc for the markup, personName
 * for an account id, me for the account this browser is, restore to send the
 * undo command, gone to ask whether a row is still deleted, and open to show a
 * task.
 */
export function draw(box, scope, ctx) {
  let cursor = 0;
  let rows = [];

  const paint = (state) => {
    if (state === 'loading' && !rows.length) {
      box.innerHTML = '<span class="d-hint">Reading the history…</span>';
      return;
    }
    if (state === 'failed') {
      box.innerHTML = '<span class="d-hint">The history needs the network. '
        + 'Everything else here works offline.</span>';
      return;
    }
    if (!rows.length) {
      box.innerHTML = '<span class="d-hint">Nothing happened yet.</span>';
      return;
    }
    box.innerHTML = rows.map((r) => line(r, ctx)).join('')
      + (state === 'more' ? '<button class="acmore">Show more</button>' : '');

    const more = box.querySelector('.acmore');
    if (more) more.onclick = () => load();

    [...box.querySelectorAll('.acundo')].forEach((b) => {
      b.onclick = () => {
        ctx.restore(b.dataset.cmd, b.dataset.row);
        b.remove();
      };
    });
    [...box.querySelectorAll('.acopen')].forEach((b) => {
      b.onclick = () => ctx.open(b.dataset.task);
    });
  };

  const load = async () => {
    paint('loading');
    try {
      const d = await page(scope, cursor, 25);
      rows = rows.concat(d.activity || []);
      if (rows.length) cursor = rows[rows.length - 1].seq;
      paint(d.more ? 'more' : 'done');
    } catch (e) {
      paint(rows.length ? 'done' : 'failed');
    }
  };

  load();
}

// line draws one row. It never says "me did something": the first person reads
// as "you", which is how the comment panel already writes it.
function line(r, ctx) {
  const who = r.account_id === ctx.me ? 'You' : (r.actor || ctx.personName(r.account_id) || 'Someone');
  const what = ctx.esc(r.title || '');
  const undo = undoOf(r.action);
  const rowId = r.action.startsWith('task_') ? r.task_id : r.project_id;

  let tail = '';
  if (isSecurity(r.action)) {
    // A security line names the device and the address, and nothing else. It
    // has no task to open and nothing to restore.
    tail = [what, r.detail ? ctx.esc(r.detail) : '', r.addr ? ctx.esc(r.addr) : '']
      .filter(Boolean).map((x) => `<span class="acwhat">${x}</span>`).join(' ');
  } else if (r.task_id && ctx.known(r.task_id)) {
    tail = `<button class="acopen" data-task="${ctx.esc(r.task_id)}">${what}</button>`;
  } else {
    tail = `<span class="acwhat">${what}</span>`;
  }

  const fields = r.action === 'task_update' || r.action === 'project_update'
    ? detailWords(r.detail) : '';

  return `<div class="acrow">
    <span class="acwho">${ctx.esc(who)}</span>
    <span class="acverb">${ctx.esc(verb(r.action))}</span>
    ${tail}
    ${fields ? `<span class="acfields">— ${ctx.esc(fields)}</span>` : ''}
    <span class="acwhen">${ctx.esc(ago(r.at))}</span>
    ${undo && rowId && ctx.gone(r.action, rowId)
      ? `<button class="acundo" data-cmd="${undo}" data-row="${ctx.esc(rowId)}">Restore</button>` : ''}
  </div>`;
}
