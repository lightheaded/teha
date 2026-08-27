// SPDX-License-Identifier: Apache-2.0
//
// The contract between the shell and the web app.
//
// The shell holds no quick add parser. It puts one line into the quick add
// field of the page and waits for the field to clear. That is a contract with
// the DOM of the web app, and a rename in internal/webui/assets/app.js would
// break quick add in the shell with nothing to catch it.
//
// This test catches it. The fake field below mirrors the two listeners that
// app.js wires on the element with the id "qa": an input listener for the
// hint, and a keydown listener that adds the task on Enter and clears the
// field.
//
//     node --test desktop/tools/contract.test.mjs
//
// A change to app.js that renames the field, or that stops clearing it, makes
// this test fail. Read desktop/README.md and docs/BACKLOG.md for the way out:
// a small hook in the web app that the shell calls instead.

import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ADD_JS = path.join(HERE, '..', 'src-tauri', 'src', 'js', 'add.js');
const APP_JS = path.join(HERE, '..', '..', 'internal', 'webui', 'assets', 'app.js');

/** A minimal stand-in for a DOM event. */
class FakeEvent {
  constructor(type, init = {}) {
    this.type = type;
    Object.assign(this, init);
  }
}

/** The script, with the placeholder replaced the way Rust replaces it. */
function script(line) {
  return fs.readFileSync(ADD_JS, 'utf8').replaceAll('__TEHA_LINE__', JSON.stringify(line));
}

/** A field that behaves as the quick add field of the web app behaves. */
function makeField() {
  const listeners = {};
  const field = {
    value: '',
    counts: { input: 0, enter: 0 },
    addEventListener(type, fn) {
      (listeners[type] ||= []).push(fn);
    },
    dispatchEvent(event) {
      (listeners[event.type] || []).forEach((fn) => fn(event));
      return true;
    }
  };
  field.addEventListener('input', () => {
    field.counts.input += 1;
  });
  field.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      field.counts.enter += 1;
      field.value = '';
    }
  });
  return field;
}

function run(source, field, timeouts) {
  globalThis.document = {
    readyState: 'complete',
    getElementById: (id) => (id === 'qa' && field ? field : null),
    addEventListener() {}
  };
  globalThis.window = {
    setTimeout: () => {
      timeouts.push(1);
    }
  };
  globalThis.Event = FakeEvent;
  globalThis.KeyboardEvent = FakeEvent;
  new Function(source)();
}

test('the line reaches the field, and the field clears', () => {
  const field = makeField();
  const timeouts = [];
  run(script('Book the ferry tomorrow at 9:30 p1 #Trip'), field, timeouts);
  assert.equal(field.counts.input, 1, 'one input event, so the hint updates');
  assert.equal(field.counts.enter, 1, 'one Enter, so one task and never two');
  assert.equal(field.value, '', 'the field ends empty');
  assert.equal(timeouts.length, 0, 'a success needs no retry');
});

test('a page without the field retries instead of throwing', () => {
  const timeouts = [];
  run(script('Buy milk'), null, timeouts);
  assert.equal(timeouts.length, 1, 'a missing field schedules a retry');
});

test('a field that keeps the line retries', () => {
  // The listener of the web app is wired a moment after the element exists.
  // Until then the field holds the line, and the script must try again.
  const field = { value: '', addEventListener() {}, dispatchEvent: () => true };
  const timeouts = [];
  run(script('Buy milk'), field, timeouts);
  assert.equal(timeouts.length, 1, 'a line that stays schedules a retry');
});

test('the web app still names the field and still clears it', () => {
  const app = fs.readFileSync(APP_JS, 'utf8');
  assert.match(app, /\bconst qa = \$\('qa'\)/, 'the field is still id "qa"');
  assert.match(app, /if \(e\.key === 'Enter'\)/, 'Enter still adds the task');
  assert.match(app, /qa\.value = ''/, 'the field still clears itself');
});
