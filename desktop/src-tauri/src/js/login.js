// SPDX-License-Identifier: Apache-2.0
//
// Sign the window in with the device token from the keychain.
//
// The server keeps one device token and hands out a cookie for it at /login.
// This script runs before every page of the server loads. It acts on the login
// page only, and it posts the token the way the login form does.
//
// The Rust side replaces the placeholder below with a JSON string literal. It
// appears once, in the code, because a mention of it anywhere else in this file
// would be replaced as well, and a token has no business in a comment.
//
// The token stays inside this function, so no script of the page can read it,
// and it never reaches the DOM, a log line or the settings file.

(function () {
  'use strict';

  var token = __TEHA_TOKEN__;
  if (!token || window.location.pathname !== '/login') {
    return;
  }
  // bad=1 means the server refused this token. A second try would loop, so the
  // page is left to the person.
  if (window.location.search.indexOf('bad=1') !== -1) {
    return;
  }
  try {
    if (window.sessionStorage.getItem('teha-desktop-signin') === '1') {
      return;
    }
    window.sessionStorage.setItem('teha-desktop-signin', '1');
  } catch (err) {
    // A window without storage signs in once per load. That is still correct.
  }

  window
    .fetch('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'token=' + window.encodeURIComponent(token),
      credentials: 'same-origin',
      redirect: 'follow'
    })
    .then(function () {
      window.location.replace('/');
    })
    .catch(function () {
      console.error('teha: the server did not answer the sign in');
    });
})();
