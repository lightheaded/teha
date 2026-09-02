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

// --- joining ------------------------------------------------------------------
// An invitation code is the second way in. The server turns it into an account
// with its own inbox, sets the cookies and answers with a device token, which
// the person needs for the phone app and for an agent.

$('joinForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const btn = $('joinBtn');
  const code = $('joinCode').value.trim();
  const name = $('joinName').value.trim();
  $('joinErr').textContent = '';
  if (!code) { $('joinErr').textContent = 'Type the code you were sent.'; return; }
  btn.disabled = true;
  btn.textContent = 'Joining…';
  try {
    const res = await fetch('/v1/join', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, name }),
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(d.error || 'that code did not work');
    location.href = '/';
    return;
  } catch (err) {
    $('joinErr').textContent = String(err.message || err);
  }
  btn.disabled = false;
  btn.textContent = 'Join';
});
