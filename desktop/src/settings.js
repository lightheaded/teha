// SPDX-License-Identifier: Apache-2.0
//
// The settings window. It reads the current settings, takes a new set, and
// hands them to the shell. The token travels one way: this page can write it
// into the keychain and can never read it back.

'use strict';

(function () {
  var server = document.getElementById('server');
  var token = document.getElementById('token');
  var shortcut = document.getElementById('shortcut');
  var note = document.getElementById('note');
  var save = document.getElementById('save');

  function invoke(name, args) {
    var core = window.__TAURI__ && window.__TAURI__.core;
    if (!core) {
      return Promise.reject(new Error('the shell did not answer'));
    }
    return core.invoke(name, args || {});
  }

  function say(text, bad) {
    note.textContent = text;
    note.className = bad ? 'hint bad' : 'hint';
  }

  invoke('settings_read')
    .then(function (view) {
      server.value = view.server || '';
      shortcut.value = view.shortcut || view.defaultShortcut;
      shortcut.placeholder = view.defaultShortcut;
      token.placeholder = view.hasToken
        ? 'kept in the keychain, leave empty'
        : 'the device token of the server';
      server.focus();
    })
    .catch(function (err) {
      say(String(err), true);
    });

  function submit() {
    say('Saving', false);
    invoke('settings_write', {
      server: server.value,
      token: token.value,
      shortcut: shortcut.value
    })
      .then(function () {
        token.value = '';
        say('Saved.', false);
      })
      .catch(function (err) {
        say(String(err), true);
      });
  }

  save.addEventListener('click', submit);
  document.addEventListener('keydown', function (event) {
    if (event.key === 'Enter' && !event.isComposing) {
      event.preventDefault();
      submit();
    }
  });
})();
