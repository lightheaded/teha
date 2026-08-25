// Runs the shared quick add corpus against the web parser.
//   node --test internal/webui/assets/parse.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { parseQuickAdd } from './parse.js';

const here = dirname(fileURLToPath(import.meta.url));
const corpus = JSON.parse(readFileSync(join(here, '../../../parser-fixtures/quickadd.json'), 'utf8'));
const today = new Date(corpus.today + 'T09:00:00');

for (const c of corpus.cases) {
  test(c.in, () => {
    const got = parseQuickAdd(c.in, today);
    assert.equal(got.title, c.title, 'title');
    assert.equal(got.due || '', c.due || '', 'due');
    assert.equal(got.time || '', c.time || '', 'time');
    assert.equal(got.priority || 0, c.priority || 0, 'priority');
    assert.equal(got.project || '', c.project || '', 'project');
    assert.equal(got.rrule || '', c.rrule || '', 'rrule');
    assert.deepEqual(got.labels || [], c.labels || [], 'labels');
  });
}
