// Runs the shared filter corpus against the web evaluator.
//   node --test internal/webui/assets/filter.test.mjs
//
// The answers in the corpus come from the Go compiler running against real
// SQLite. This test therefore proves that a filter means the same thing in the
// browser as it does on the server, term by term.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { run, compile, CAPS } from './filter.js';

const here = dirname(fileURLToPath(import.meta.url));
const corpus = JSON.parse(readFileSync(join(here, '../../../parser-fixtures/filter.json'), 'utf8'));

// The rows as the browser holds them: a task carries its labels as an array,
// and a field the server left out is missing rather than null.
// A task in the browser names its assignee the way the sync payload does.
const tasks = corpus.tasks.map((t) => ({ ...t, assignee_id: t.assignee }));
const env = {
  today: corpus.today,
  inboxId: corpus.inbox_id,
  me: corpus.me,
  accounts: corpus.accounts,
  tasks,
  projects: corpus.projects.map((p) => ({ ...p, deleted_at: p.deleted ? '2026-08-25T00:00:00Z' : undefined })),
  sections: corpus.sections.map((s) => ({ ...s })),
  comments: corpus.comments.map((c) => ({
    ...c,
    deleted_at: c.deleted ? '2026-08-25T00:00:00Z' : undefined,
  })),
};

for (const c of corpus.cases) {
  test(`query: ${c.q || '(empty)'}`, () => {
    const got = run(c.q, env).map((t) => t.id).sort();
    assert.deepEqual(got, c.want);
  });
}

for (const c of corpus.rejects) {
  test(`rejects: ${c.q}`, () => {
    assert.throws(() => run(c.q, env), undefined, `the evaluator accepted ${c.q}`);
  });
}

// The browser holds no creation date, so it must refuse the term with a
// sentence rather than answer with the wrong rows.
for (const c of corpus.no_created_column) {
  test(`refuses without a creation date: ${c.q}`, () => {
    assert.throws(() => run(c.q, env), /creation date/);
  });
}

// A store that keeps no section table refuses a section term, the way the
// phone does. The browser keeps one, so the capability is what decides.
for (const c of corpus.no_section_table) {
  test(`refuses without a section table: ${c.q}`, () => {
    assert.throws(() => compile(c.q, corpus.today, { ...CAPS, section: false }), /section table/);
  });
}

for (const c of corpus.no_assignee_column) {
  test(`refuses without an assignee column: ${c.q}`, () => {
    assert.throws(() => compile(c.q, corpus.today, { ...CAPS, assignee: false }), /assignee/);
  });
}

// The phone keeps no comment table. A term that reads one must fail there with
// a sentence, and never fall back to the description: that answer is close and
// wrong.
for (const c of corpus.no_comment_table) {
  test(`refuses without a comment table: ${c.q}`, () => {
    assert.throws(() => compile(c.q, corpus.today, { ...CAPS, comment: false }), /comment table/);
  });
}
