// SPDX-License-Identifier: AGPL-3.0-or-later

// The filter language, evaluated over the local rows.
//
// The server compiles the same grammar into SQL (see filter/filter.go). This
// file reads the same grammar and answers over the objects the browser holds,
// because the browser has no SQL to run and it must answer with no network.
//
// parser-fixtures/filter.json is the contract that holds the two together. The
// Go test writes the answers by running real SQLite, and
// internal/webui/assets/filter.test.mjs demands the same answers here. A term
// that means two things in two clients fails that test.
//
// The structure follows filter.go on purpose: a lexer, then parseOr, parseAnd,
// parseUnary and term. Read the two side by side.

// CAPS says what this store holds. filter.Schema does the same job on the Go
// side: a term that needs a column the store has not got must fail with a
// sentence, and never quietly answer with the wrong rows.
export const CAPS = {
  // The browser keeps the section rows, so a section term answers.
  section: true,
  // The sync payload carries no creation date, so `created:` cannot answer.
  // The phone refuses it for the same reason.
  created: false,
  // The browser holds the assignee of a task and the people of the household,
  // so an assignee term answers.
  assignee: true,
  // The sync payload carries the comments of every task the account may see,
  // so `comment:` answers. The phone holds none and refuses the term.
  comment: true,
};

// --- lexer ------------------------------------------------------------------

const AND = 1, OR = 2, NOT = 3, LPAREN = 4, RPAREN = 5, WORD = 6, EOF = 0;
const OPERATORS = '&|,!()';

function lex(s) {
  const out = [];
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (c === ' ' || c === '\t') { i++; continue; }
    if (c === '&') { out.push({ kind: AND, text: c, pos: i }); i++; continue; }
    if (c === '|' || c === ',') { out.push({ kind: OR, text: c, pos: i }); i++; continue; }
    if (c === '!') { out.push({ kind: NOT, text: c, pos: i }); i++; continue; }
    if (c === '(') { out.push({ kind: LPAREN, text: c, pos: i }); i++; continue; }
    if (c === ')') { out.push({ kind: RPAREN, text: c, pos: i }); i++; continue; }
    const start = i;
    while (i < s.length && !OPERATORS.includes(s[i])) i++;
    const text = s.slice(start, i).trim();
    if (text) out.push({ kind: WORD, text, pos: start });
  }
  out.push({ kind: EOF, text: '', pos: s.length });
  return out;
}

// --- dates ------------------------------------------------------------------

const DAY = 86400000;

function shift(today, n) {
  const d = new Date(today + 'T00:00:00Z');
  return new Date(d.getTime() + n * DAY).toISOString().slice(0, 10);
}

const WEEKDAYS = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];
const MONTHS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

// readDate reads an absolute or a relative date, the way filter.go does.
function readDate(v, today) {
  const low = String(v).trim().toLowerCase();
  if (low === 'today') return today;
  if (low === 'tomorrow') return shift(today, 1);
  if (low === 'yesterday') return shift(today, -1);

  const rel = low.match(/^([+-]?\d+)\s*days?$/);
  if (rel) return shift(today, Number(rel[1]));

  if (/^\d{4}-\d{2}-\d{2}$/.test(low)) return low;

  const dotted = low.match(/^(\d{1,2})\.(\d{1,2})\.(\d{4})$/);
  if (dotted) return ymd(Number(dotted[3]), Number(dotted[2]), Number(dotted[1]));

  const monthFirst = low.match(/^([a-z]{3,9})\s+(\d{1,2})\s+(\d{4})$/);
  if (monthFirst && MONTHS.indexOf(monthFirst[1].slice(0, 3)) >= 0) {
    return ymd(Number(monthFirst[3]), MONTHS.indexOf(monthFirst[1].slice(0, 3)) + 1, Number(monthFirst[2]));
  }
  const dayFirst = low.match(/^(\d{1,2})\s+([a-z]{3,9})\s+(\d{4})$/);
  if (dayFirst && MONTHS.indexOf(dayFirst[2].slice(0, 3)) >= 0) {
    return ymd(Number(dayFirst[3]), MONTHS.indexOf(dayFirst[2].slice(0, 3)) + 1, Number(dayFirst[1]));
  }

  // A weekday name means its next occurrence, never today.
  for (let i = 0; i < WEEKDAYS.length; i++) {
    if (low !== WEEKDAYS[i] && low !== WEEKDAYS[i].slice(0, 3)) continue;
    const dow = new Date(today + 'T00:00:00Z').getUTCDay();
    let delta = (i - dow + 7) % 7;
    if (delta === 0) delta = 7;
    return shift(today, delta);
  }
  throw new Error(`cannot read the date "${v}"`);
}

function ymd(y, m, d) {
  const pad = (n) => String(n).padStart(2, '0');
  // A month or a day out of range is not a date. The Go parser refuses it too.
  if (m < 1 || m > 12 || d < 1 || d > 31) throw new Error('that is not a date');
  return `${y}-${pad(m)}-${pad(d)}`;
}

// --- LIKE -------------------------------------------------------------------

// likeToRe turns a SQL LIKE pattern into a regular expression, so a search
// term and a label name behave here exactly as they do in SQLite. `%` and `_`
// are the wildcards, and a backslash escapes one.
function likeToRe(pattern) {
  let re = '';
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === '\\') { re += escapeRe(pattern[++i] ?? '\\'); continue; }
    if (c === '%') { re += '[\\s\\S]*'; continue; }
    if (c === '_') { re += '[\\s\\S]'; continue; }
    re += escapeRe(c);
  }
  return new RegExp('^' + re + '$');
}

function escapeRe(c) {
  return c.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// --- rows -------------------------------------------------------------------

// A field the payload leaves out is missing, not empty. The SQL asks IS NULL,
// so an empty string must read as a value and not as a null.
const val = (x) => (x === undefined || x === null ? null : x);
const low = (x) => String(x ?? '').toLowerCase();

function list(rows) {
  if (!rows) return [];
  return typeof rows.values === 'function' ? [...rows.values()] : [...rows];
}

// index builds what a predicate needs beyond one row: the projects by id, the
// live sections, and the children of every task.
export function index(env) {
  const projects = list(env.projects);
  const sections = list(env.sections);
  const tasks = list(env.tasks);
  const accounts = list(env.accounts);
  const children = new Map();
  // The comments of one task, by task. A deleted line is out: the server drops
  // it from a `comment:` answer, and this file must agree.
  const comments = new Map();
  for (const c of list(env.comments)) {
    if (val(c.deleted_at)) continue;
    if (!comments.has(c.task_id)) comments.set(c.task_id, []);
    comments.get(c.task_id).push(c);
  }
  for (const t of tasks) {
    if (!val(t.parent_id)) continue;
    if (!children.has(t.parent_id)) children.set(t.parent_id, []);
    children.get(t.parent_id).push(t);
  }
  return {
    today: env.today,
    inboxId: env.inboxId || 'inbox',
    caps: env.caps || CAPS,
    projects,
    sections,
    tasks,
    accounts,
    // me is the account that is asking. `assigned to: me` needs it.
    me: env.me || '',
    byId: new Map(tasks.map((t) => [t.id, t])),
    children,
    comments,
  };
}

// --- parser -----------------------------------------------------------------

class Parser {
  constructor(query, today, caps) {
    this.toks = lex(query);
    this.at = 0;
    this.today = today;
    this.caps = caps;
    this.saidState = false;
    this.next();
  }

  next() {
    this.tok = this.toks[this.at] || { kind: EOF, text: '', pos: 0 };
    if (this.at < this.toks.length) this.at++;
  }

  parseOr() {
    let left = this.parseAnd();
    while (this.tok.kind === OR) {
      this.next();
      const right = this.parseAnd();
      const a = left;
      left = (t, e) => a(t, e) || right(t, e);
    }
    return left;
  }

  parseAnd() {
    let left = this.parseUnary();
    while (this.tok.kind === AND) {
      this.next();
      const right = this.parseUnary();
      const a = left;
      left = (t, e) => a(t, e) && right(t, e);
    }
    return left;
  }

  parseUnary() {
    if (this.tok.kind === NOT) {
      this.next();
      const inner = this.parseUnary();
      return (t, e) => !inner(t, e);
    }
    if (this.tok.kind === LPAREN) {
      this.next();
      const inner = this.parseOr();
      if (this.tok.kind !== RPAREN) throw new Error('missing closing parenthesis');
      this.next();
      return inner;
    }
    if (this.tok.kind !== WORD) throw new Error(`expected a term, found "${this.tok.text}"`);
    const word = this.tok.text;
    this.next();
    return this.term(word);
  }

  // term reads one leaf of the query. The order of the cases follows
  // filter.go, so the two files can be compared line by line.
  term(word) {
    const key = word.trim().toLowerCase();
    const day = (n) => shift(this.today, n);

    switch (key) {
      case 'today': case 'tod':
        return (t) => val(t.due_date) !== null && t.due_date <= day(0);
      case 'tomorrow': case 'tom':
        return (t) => val(t.due_date) === day(1);
      case 'yesterday':
        return (t) => val(t.due_date) === day(-1);
      case 'overdue': case 'od': case 'over due':
        return (t) => val(t.due_date) !== null && t.due_date < day(0);
      case 'no date': case 'no due date': case 'nodate':
        return (t) => val(t.due_date) === null;
      case 'no time':
        return (t) => val(t.due_time) === null;
      case 'has time':
        return (t) => val(t.due_time) !== null;
      case 'recurring':
        return (t) => val(t.rrule) !== null && t.rrule !== '';
      case 'subtask':
        return (t) => val(t.parent_id) !== null;
      case 'no parent': case 'top level':
        return (t) => val(t.parent_id) === null;
      case 'no priority':
        return (t) => (t.priority || 4) === 4;
      case 'no deadline':
        return (t) => val(t.deadline) === null;
      case 'deadline':
        return (t) => val(t.deadline) !== null;
      case 'no section': case 'no sections':
        this.needSections();
        return (t) => val(t.section_id) === null;
      case 'no label': case 'no labels':
        return (t) => (t.labels || []).length === 0;
      case 'assigned':
        this.needAssignee();
        return (t) => !!val(t.assignee_id);
      case 'unassigned': case 'not assigned': case 'no assignee':
        this.needAssignee();
        return (t) => !val(t.assignee_id);
      case 'assigned to me': case 'mine':
        return this.assignedTo('me');
      case 'started':
        return (t) => val(t.start_date) === null || t.start_date <= day(0);
      case 'not started': case 'deferred':
        return (t) => val(t.start_date) !== null && t.start_date > day(0);
      case 'done': case 'completed':
        this.saidState = true;
        return (t) => t.state === 'done';
      case 'wont do': case 'wont-do': case "won't do": case 'skipped':
        this.saidState = true;
        return (t) => t.state === 'wont_do';
      case 'open': case 'active':
        this.saidState = true;
        return (t) => t.state === 'open';
      case 'any state': case 'all states':
        this.saidState = true;
        return () => true;
      case 'week': case 'next 7 days': case '7 days':
        return (t) => val(t.due_date) !== null && t.due_date <= day(7);
      default:
        break;
    }

    if (/^p[1-4]$/.test(key)) {
      const n = Number(key[1]);
      return (t) => (t.priority || 4) === n;
    }

    if (word.startsWith('##')) {
      const name = word.slice(2).trim();
      return (t, e) => projectFamily(e, name).has(t.project_id);
    }
    if (word.startsWith('#')) {
      const name = word.slice(1).trim();
      if (name.toLowerCase() === 'inbox') return (t, e) => t.project_id === e.inboxId;
      const test = nameTest(name);
      return (t, e) => e.projects.some((p) => !val(p.deleted_at) && test(low(p.name)) && p.id === t.project_id);
    }
    if (word.startsWith('/')) {
      this.needSections();
      const test = nameTest(word.slice(1).trim());
      return (t, e) => val(t.section_id) !== null
        && e.sections.some((x) => !val(x.deleted_at) && test(low(x.name)) && x.id === t.section_id);
    }
    if (word.startsWith('%') || word.startsWith('@')) {
      // Todoist moved filters to %label and retires @ through 2026. Both read
      // here, so an imported filter and a habit both keep working.
      const test = nameTest(word.slice(1).trim());
      return (t) => (t.labels || []).some((l) => test(low(l)));
    }

    const colon = key.indexOf(':');
    if (colon >= 0) {
      const k = key.slice(0, colon).trim();
      const v = word.slice(colon + 1).trim();
      switch (k) {
        case 'search':
          return searchTerm(v);
        case 'comment': case 'note': {
          // A comment is a row of its own, so the term reads the comments the
          // pull carried. Section 6.3 of docs/PLAN.md counts the gap closed.
          if (!this.caps.comment) {
            throw new Error('comment: needs a comment table, and this client keeps none');
          }
          const re = likeToRe('%' + low(v) + '%');
          return (t, e) => (e.comments.get(t.id) || []).some((c) => re.test(low(c.body)));
        }
        case 'with subtasks': case 'with sub-tasks': case 'family': {
          const inner = this.term(v);
          return (t, e) => inner(t, e)
            || (val(t.parent_id) !== null && e.byId.has(t.parent_id) && inner(e.byId.get(t.parent_id), e))
            || (e.children.get(t.id) || []).some((c) => inner(c, e));
        }
        case 'assigned to': case 'assignee':
          return this.assignedTo(v);
        case 'date': case 'due': {
          const d = readDate(v, this.today);
          return (t) => val(t.due_date) === d;
        }
        case 'before': case 'date before': case 'due before': {
          const d = readDate(v, this.today);
          return (t) => val(t.due_date) !== null && t.due_date < d;
        }
        case 'after': case 'date after': case 'due after': {
          const d = readDate(v, this.today);
          return (t) => val(t.due_date) !== null && t.due_date > d;
        }
        case 'deadline': {
          const d = readDate(v, this.today);
          return (t) => val(t.deadline) === d;
        }
        case 'deadline before': {
          const d = readDate(v, this.today);
          return (t) => val(t.deadline) !== null && t.deadline < d;
        }
        case 'created': case 'created before': case 'created after':
          readDate(v, this.today); // a bad date is still a bad date
          if (!this.caps.created) {
            throw new Error('created: needs a creation date, and this client keeps none');
          }
          break;
        default:
          break;
      }
    }

    // A bare word searches the title, which is what a person expects.
    return searchTerm(key);
  }

  needSections() {
    if (!this.caps.section) {
      throw new Error('a section term needs a section table, and this client keeps none');
    }
  }

  needAssignee() {
    if (!this.caps.assignee) {
      throw new Error('an assignee term needs an assignee column, and this client keeps none');
    }
  }

  // assignedTo matches the person who does a task. Three values, and the last
  // one is the useful one in a household: me, nobody, or a name.
  assignedTo(v) {
    this.needAssignee();
    const want = String(v).trim().toLowerCase();
    switch (want) {
      case 'me': case 'myself':
        return (t, e) => {
          if (!e.me) throw new Error('assigned to: me needs to know who is asking, and this client does not');
          return val(t.assignee_id) === e.me;
        };
      case 'nobody': case 'none': case 'no one': case 'noone':
        return (t) => !val(t.assignee_id);
      case 'anyone': case 'somebody': case 'someone':
        return (t) => !!val(t.assignee_id);
      default:
        break;
    }
    return (t, e) => !!val(t.assignee_id) && e.accounts.some((a) => a.id === t.assignee_id
      && (low(a.name) === want || low(a.display_name) === want));
  }
}

// nameTest matches a project, a section or a label name. A trailing star is a
// prefix match, and the rest is an exact one. Both ignore case.
function nameTest(raw) {
  const name = raw.toLowerCase();
  if (name.endsWith('*')) {
    const re = likeToRe(name.slice(0, -1) + '%');
    return (candidate) => re.test(candidate);
  }
  return (candidate) => candidate === name;
}

function searchTerm(text) {
  const re = likeToRe('%' + low(text) + '%');
  return (t) => re.test(low(t.title)) || re.test(low(t.description));
}

// projectFamily returns the id of a project and of every project below it.
// The SQL walks the same tree with a recursive query.
function projectFamily(env, name) {
  const want = name.toLowerCase();
  const ids = new Set();
  for (const p of env.projects) {
    if (!val(p.deleted_at) && low(p.name) === want) ids.add(p.id);
  }
  // Walk down until nothing new arrives. A cycle therefore cannot hang this.
  for (let grew = true; grew;) {
    grew = false;
    for (const p of env.projects) {
      if (ids.has(p.id) || !val(p.parent_id) || !ids.has(p.parent_id)) continue;
      ids.add(p.id);
      grew = true;
    }
  }
  return ids;
}

// --- the public surface -----------------------------------------------------

// compile reads a query and returns the predicate. It throws one sentence when
// the query is broken, or when a term needs a column this store has not got.
export function compile(query, today, caps = CAPS) {
  const p = new Parser(query || '', today, caps);
  if (p.tok.kind === EOF) {
    // An empty query means every OPEN task, not every task.
    return (t) => t.state === 'open';
  }
  const pred = p.parseOr();
  if (p.tok.kind !== EOF) {
    throw new Error(`unexpected "${p.tok.text}" at position ${p.tok.pos}`);
  }
  // A filter shows open tasks unless the query named a state itself. The test
  // is what the PARSER saw: the text "search: done", and a project called
  // Done, both hold the word and say nothing about state.
  if (p.saidState) return pred;
  return (t, e) => pred(t, e) && t.state === 'open';
}

// A compiled query is reused. A view counts every task in the sidebar, so the
// same handful of queries runs many times per frame.
const cache = new Map();

export function compileCached(query, today, caps = CAPS) {
  const key = [today, caps.created ? 1 : 0, caps.section ? 1 : 0, caps.assignee ? 1 : 0, query].join(' ');
  if (cache.has(key)) return cache.get(key);
  const pred = compile(query, today, caps);
  if (cache.size > 200) cache.clear();
  cache.set(key, pred);
  return pred;
}

// run answers one query over one account. env carries today, the rows and the
// id of the inbox. It throws the same sentence compile throws.
export function run(query, env) {
  const e = index(env);
  const pred = compileCached(query, e.today, e.caps);
  return e.tasks.filter((t) => !val(t.deleted_at) && pred(t, e));
}
