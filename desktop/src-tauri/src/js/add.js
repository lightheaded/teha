// SPDX-License-Identifier: Apache-2.0
//
// One quick add line, put into the quick add field of the web app.
//
// The shell holds no parser. The web app parses the line, writes the task into
// its own store and its own outbox, and syncs it. So a task from the panel or
// from a teha:// URL travels the same path as a task a person types, and the
// two can never disagree about a date.
//
// The Rust side replaces the placeholder below with a JSON string literal, so
// the line arrives as data. It appears once, in the code, because a mention of
// it anywhere else in this file would be replaced as well.
//
// This file is the whole contract with the web app: the quick add field has the
// id "qa", and it clears itself after it adds a task.

(function () {
  'use strict';

  var line = __TEHA_LINE__;
  var tries = 0;

  function attempt() {
    tries += 1;
    var field = document.getElementById('qa');
    if (field) {
      field.value = line;
      field.dispatchEvent(new Event('input', { bubbles: true }));
      field.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      );
      // The web app clears the field after it adds the task. A field that
      // still holds the line means the page is not ready, or the line has no
      // title left after the tags.
      if (field.value !== line) {
        return;
      }
      field.value = '';
    }
    if (tries < 60) {
      window.setTimeout(attempt, 250);
      return;
    }
    console.error('teha: the quick add field never took the line');
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', attempt);
  } else {
    attempt();
  }
})();
