// SPDX-License-Identifier: AGPL-3.0-or-later

// The login page. The passkey button is the first choice, and the token box
// below it is the fallback that every native client needs anyway.

import { supported, signIn, message } from './passkey.js';

const $ = (id) => document.getElementById(id);

if (location.search.includes('bad=1')) $('err').textContent = 'That token did not match.';

// The button appears only where the browser can do a passkey. An older browser
// sees the token box alone, and no button that fails when it is pressed.
if (supported()) {
  $('pk').hidden = false;
  $('or').hidden = false;
  const btn = $('pkBtn');
  btn.onclick = async () => {
    $('pkErr').textContent = '';
    btn.disabled = true;
    btn.textContent = 'Waiting for the passkey…';
    try {
      await signIn();
      location.href = '/';
      return;
    } catch (e) {
      $('pkErr').textContent = message(e);
    }
    btn.disabled = false;
    btn.textContent = 'Sign in with a passkey';
  };
}
