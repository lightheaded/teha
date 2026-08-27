// SPDX-License-Identifier: Apache-2.0
//
// The quick add panel.
//
// This page holds no parser. It sends one line to the shell, and the shell
// puts that line into the quick add field of the web app, which parses it.
// One parser, one set of fixtures, no drift.

'use strict';

(function () {
  var field = document.getElementById('line');
  var hint = document.getElementById('hint');
  var READY = 'Enter adds it. Escape closes.';

  function invoke(name, args) {
    var core = window.__TAURI__ && window.__TAURI__.core;
    if (!core) {
      return Promise.reject(new Error('the shell did not answer'));
    }
    return core.invoke(name, args || {});
  }

  function say(text, bad) {
    hint.textContent = text;
    hint.className = bad ? 'hint bad' : 'hint';
  }

  // The shell calls this when it opens the panel, and the focus event calls it
  // as well. An empty field every time is what makes the panel feel new.
  window.tehaOpen = function () {
    field.value = '';
    say(READY, false);
    field.focus();
  };

  window.addEventListener('focus', function () {
    window.tehaOpen();
  });

  field.addEventListener('keydown', function (event) {
    if (event.key === 'Escape') {
      event.preventDefault();
      invoke('panel_close');
      return;
    }
    if (event.key !== 'Enter' || event.isComposing) {
      return;
    }
    event.preventDefault();
    var line = field.value.trim();
    if (!line) {
      invoke('panel_close');
      return;
    }
    say('Adding', false);
    invoke('panel_submit', { line: line }).catch(function (err) {
      // The panel stays open on a failure, so the line is not lost.
      say(String(err), true);
    });
  });

  window.tehaOpen();
})();
