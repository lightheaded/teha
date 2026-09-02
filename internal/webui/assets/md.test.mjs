// Runs the shared Markdown corpus against the web renderer.
//   node --test internal/webui/assets/md.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { render, inline, linkPaste, isURL, href } from './md.js';

const here = dirname(fileURLToPath(import.meta.url));
const corpus = JSON.parse(readFileSync(join(here, '../../../parser-fixtures/markdown.json'), 'utf8'));

for (const c of corpus.inline) {
  test(`inline: ${c.in}`, () => assert.equal(inline(c.in), c.html));
}

for (const c of corpus.blocks) {
  test(`block: ${JSON.stringify(c.in)}`, () => assert.equal(render(c.in), c.html));
}

for (const c of corpus.paste) {
  test(`paste: ${c.paste} over ${JSON.stringify(c.value.slice(c.start, c.end))}`, () => {
    const got = linkPaste(c.value, c.start, c.end, c.paste);
    if (c.out === null) { assert.equal(got, null); return; }
    assert.equal(got.value, c.out);
    assert.equal(got.caret, c.caret);
  });
}

// The two guards that keep a note from reaching the page as markup.
test('no target with an unsafe scheme survives', () => {
  for (const bad of ['javascript:x', 'JavaScript:x', ' javascript:x', 'data:text/html,x', 'vbscript:x']) {
    assert.equal(href(bad), '', bad);
    assert.equal(isURL(bad), false, bad);
  }
});

test('a block renders no tag that the note wrote', () => {
  const html = render('<img src=x onerror=alert(1)>\n\n<b>no</b>');
  assert.ok(!html.includes('<img'), html);
  assert.ok(!html.includes('<b>'), html);
});
