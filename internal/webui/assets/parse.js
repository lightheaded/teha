// SPDX-License-Identifier: AGPL-3.0-or-later

// Quick add parser: one line in, structured fields out.
//
// The corpus in parser-fixtures/quickadd.json is the contract. The Kotlin
// client will run the same corpus, so a change here needs a fixture first.

export const A32 = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

// newId makes a short sortable id in the same shape as internal/id on the
// server: 8 characters of millisecond time, 3 characters of counter inside that
// millisecond, then 4 random characters. The counter is what keeps a bulk
// action unique, because randomness alone collides inside one millisecond.
let idLastMs = 0;
let idSeq = 0;

export function newId(prefix) {
  const now = Date.now();
  if (now > idLastMs) { idLastMs = now; idSeq = 0; }
  else if (++idSeq >= 32768) { idLastMs += 1; idSeq = 0; }
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  let rand = '';
  bytes.forEach((b) => { rand += A32[b & 31]; });
  return prefix + '_' + base32(idLastMs, 8) + base32(idSeq, 3) + rand;
}

function base32(value, width) {
  let v = value, out = '';
  for (let i = 0; i < width; i++) { out = A32[v % 32] + out; v = Math.floor(v / 32); }
  return out;
}

export const iso = (d) => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);

// --- quick add parser -------------------------------------------------------
// One line in, structured fields out, plus the spans it consumed so the hint
// can show what it understood. The Kotlin client will share this test corpus.

const WEEKDAYS = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];
const WD_SHORT = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];
const MONTHS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

// reProtect matches a span that no other pattern may read: a Markdown link, a
// code span, a quoted phrase and a bare URL. A URL carries dots and digits
// that look like a date, and a person who quotes a phrase means the words, not
// the date inside them. Section 6.4 of docs/PLAN.md states the quoting rule.
const RE_PROTECT = /\[[^\]\n]*\]\([^)\s]*\)|`[^`\n]+`|"[^"\n]*"|(?:https?:\/\/|mailto:)\S+/gi;

// mask covers every protected span with a character that no pattern matches.
// One character replaces one character, so a match index in the masked line
// points at the same place in the real line.
function mask(s) {
  return s.replace(RE_PROTECT, (m) => '\u0001'.repeat(m.length));
}

export function parseQuickAdd(text, today = new Date()) {
  const out = { title: '', labels: [], priority: 0, project: '', due: '', time: '', rrule: '',
    remindAt: '', remindBefore: 0, parsed: [] };
  let rest = ` ${text} `;
  // masked holds the same characters with every protected span blanked out.
  // Every pattern matches against masked, and every string comes out of rest.
  let masked = mask(rest);

  const eat = (re, fn) => {
    const m = masked.match(re);
    if (!m) return false;
    // The groups must come from the real line, not from the masked one.
    const real = rest.slice(m.index, m.index + m[0].length);
    // A protected span never matches, so no group ever falls inside one.
    const hit = [real, ...m.slice(1)];
    hit.index = m.index;
    if (fn(hit) === false) return false;
    rest = rest.slice(0, m.index) + ' ' + rest.slice(m.index + m[0].length);
    masked = masked.slice(0, m.index) + ' ' + masked.slice(m.index + m[0].length);
    out.parsed.push(real.trim());
    return true;
  };

  // recurrence first: "every day", "every week", "every monday", "every 3 days"
  eat(/\bevery\s+(day|week|month|year|weekday|(\d+)\s*(day|week|month)s?|[a-z]{3,9}day)\b/i, (m) => {
    const w = m[1].toLowerCase();
    if (w === 'day') out.rrule = 'FREQ=DAILY';
    else if (w === 'week') out.rrule = 'FREQ=WEEKLY';
    else if (w === 'month') out.rrule = 'FREQ=MONTHLY';
    else if (w === 'year') out.rrule = 'FREQ=YEARLY';
    else if (w === 'weekday') out.rrule = 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR';
    else if (m[2]) {
      const unit = { day: 'DAILY', week: 'WEEKLY', month: 'MONTHLY' }[m[3].toLowerCase()];
      out.rrule = `FREQ=${unit};INTERVAL=${m[2]}`;
    } else {
      const i = WEEKDAYS.indexOf(w);
      if (i < 0) return false;
      out.rrule = `FREQ=WEEKLY;BYDAY=${['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'][i]}`;
      if (!out.due) out.due = nextWeekday(today, i);
    }
    return true;
  });

  // a reminder, before the clock: "remind me at 8" is not a due time.
  eat(/\bremind(?:er)?(?:\s+me)?\s+(?:at\s+)?([01]?\d|2[0-3]):([0-5]\d)\b/i, (m) => {
    out.remindAt = `${String(m[1]).padStart(2, '0')}:${m[2]}`; return true;
  }) || eat(/\bremind(?:er)?(?:\s+me)?\s+at\s+([01]?\d|2[0-3])\s*(am|pm)?\b/i, (m) => {
    let h = Number(m[1]);
    if (m[2] && m[2].toLowerCase() === 'pm' && h < 12) h += 12;
    out.remindAt = `${String(h).padStart(2, '0')}:00`; return true;
  }) || eat(/\bremind(?:er)?(?:\s+me)?\s+(\d+)\s*(minutes|minute|mins|min|hours|hour|hrs|hr|days|day|m|h|d)\s+(?:before|early|ahead)\b/i, (m) => {
    const unit = m[2].toLowerCase()[0];
    out.remindBefore = Number(m[1]) * (unit === 'h' ? 60 : unit === 'd' ? 1440 : 1);
    return true;
  });

  // priority: p1..p4 or !!1
  eat(/\s(?:p|!!)([1-4])\b/i, (m) => { out.priority = Number(m[1]); return true; });

  // project: #Name (letters, digits, dash, underscore)
  eat(/\s#([\w\-åäöõüšž]+)/i, (m) => { out.project = m[1]; return true; });

  // labels: @name, repeatable
  let guard = 0;
  while (guard++ < 10 && eat(/\s@([\w\-åäöõüšž]+)/i, (m) => { out.labels.push(m[1]); return true; })) { /* keep eating */ }

  // time: "at 9", "9:30", "18:00"
  eat(/\s(?:at\s+)?([01]?\d|2[0-3]):([0-5]\d)\b/, (m) => {
    out.time = `${String(m[1]).padStart(2, '0')}:${m[2]}`; return true;
  }) || eat(/\sat\s+([01]?\d|2[0-3])\s*(am|pm)?\b/i, (m) => {
    let h = Number(m[1]);
    if (m[2] && m[2].toLowerCase() === 'pm' && h < 12) h += 12;
    out.time = `${String(h).padStart(2, '0')}:00`; return true;
  });

  // dates
  const d0 = new Date(today);
  const plus = (n) => { const d = new Date(d0); d.setDate(d.getDate() + n); return iso(d); };
  if (!out.due) {
    eat(/\b(today|tod|tonight)\b/i, () => { out.due = iso(d0); return true; }) ||
    eat(/\b(tomorrow|tom|tmr)\b/i, () => { out.due = plus(1); return true; }) ||
    eat(/\bin\s+(\d+)\s*(day|days|week|weeks)\b/i, (m) => {
      out.due = plus(Number(m[1]) * (m[2].startsWith('week') ? 7 : 1)); return true;
    }) ||
    eat(/\bnext\s+week\b/i, () => { out.due = plus(7); return true; }) ||
    eat(/\bnext\s+([a-z]{3,9})\b/i, (m) => {
      const i = weekdayIndex(m[1]);
      if (i < 0) return false;
      out.due = nextWeekday(d0, i, true); return true;
    }) ||
    eat(/\b(mon|tue|wed|thu|fri|sat|sun)(?:day|sday|nesday|rsday|urday)?\b/i, (m) => {
      const i = weekdayIndex(m[1]);
      if (i < 0) return false;
      out.due = nextWeekday(d0, i); return true;
    }) ||
    eat(/\b(\d{1,2})\.(\d{1,2})(?:\.(\d{2,4}))?\b/, (m) => { // 24.12 or 24.12.2026
      const y = m[3] ? Number(m[3].length === 2 ? '20' + m[3] : m[3]) : d0.getFullYear();
      const d = new Date(y, Number(m[2]) - 1, Number(m[1]));
      if (isNaN(d)) return false;
      if (!m[3] && d < d0) d.setFullYear(y + 1);
      out.due = iso(d); return true;
    }) ||
    eat(/\b(\d{1,2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b/i, (m) => {
      const d = new Date(d0.getFullYear(), MONTHS.indexOf(m[2].toLowerCase()), Number(m[1]));
      if (d < d0) d.setFullYear(d0.getFullYear() + 1);
      out.due = iso(d); return true;
    });
  }

  out.title = rest.replace(/\s+/g, ' ').trim();
  return out;
}

function weekdayIndex(word) {
  const w = word.toLowerCase();
  let i = WEEKDAYS.indexOf(w);
  if (i >= 0) return i;
  return WD_SHORT.indexOf(w.slice(0, 3));
}

function nextWeekday(from, idx, forceNext = false) {
  const d = new Date(from);
  let delta = (idx - d.getDay() + 7) % 7;
  if (delta === 0 && forceNext) delta = 7;
  if (delta === 0) delta = 0; // today counts, "monday" on a Monday means today
  if (forceNext && delta < 7 && delta === 0) delta = 7;
  d.setDate(d.getDate() + delta);
  return iso(d);
}
