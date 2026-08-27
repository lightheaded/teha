// SPDX-License-Identifier: AGPL-3.0-or-later

// Passkeys in the browser. Two jobs only: turn the base64url strings the
// server sends into the byte arrays the platform API wants, and turn the
// platform answer back into base64url for the server.
//
// The login page and the app both import this file, so the two screens cannot
// drift apart. No build step: this is a module the browser loads as it is.

// --- base64url ---------------------------------------------------------------

export function fromB64(s) {
  const pad = s.replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(pad + '='.repeat((4 - (pad.length % 4)) % 4));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export function toB64(buf) {
  const bytes = new Uint8Array(buf);
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// supported reports whether this browser can do a passkey at all. An old
// browser still has the token box, so the button hides instead of failing.
export function supported() {
  return !!(window.PublicKeyCredential && navigator.credentials);
}

// --- the wire ----------------------------------------------------------------

async function post(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? '{}' : JSON.stringify(body),
  });
  const text = await res.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch (e) { /* keep the status */ }
  if (!res.ok) {
    const wait = res.headers.get('Retry-After');
    let err;
    if (res.status === 429) {
      err = new Error('Too many failed sign-ins. Wait ' + (wait || '60') + ' seconds.');
    } else {
      err = new Error(data.error || 'the server answered ' + res.status);
    }
    // The caller acts on the status: a 401 on enrolment means the browser
    // holds a session and not the device token.
    err.status = res.status;
    throw err;
  }
  return data;
}

// creationOptions turns the server's JSON into the argument of create().
function creationOptions(json) {
  const o = json.publicKey;
  o.challenge = fromB64(o.challenge);
  o.user.id = fromB64(o.user.id);
  o.excludeCredentials = (o.excludeCredentials || []).map((c) => ({ ...c, id: fromB64(c.id) }));
  return o;
}

// requestOptions turns the server's JSON into the argument of get().
function requestOptions(json) {
  const o = json.publicKey;
  o.challenge = fromB64(o.challenge);
  o.allowCredentials = (o.allowCredentials || []).map((c) => ({ ...c, id: fromB64(c.id) }));
  return o;
}

// --- the two ceremonies ------------------------------------------------------

// enrol adds a passkey. The server guards this with the device token, so this
// call only works in a browser that already holds it.
export async function enrol(name) {
  const options = creationOptions(await post('/v1/passkeys/register/begin'));
  const cred = await navigator.credentials.create({ publicKey: options });
  if (!cred) throw new Error('the browser returned no passkey');
  const body = {
    id: cred.id,
    rawId: toB64(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toB64(cred.response.clientDataJSON),
      attestationObject: toB64(cred.response.attestationObject),
      transports: cred.response.getTransports ? cred.response.getTransports() : [],
    },
  };
  return post('/v1/passkeys/register/finish?name=' + encodeURIComponent(name || 'Passkey'), body);
}

// signIn asks the authenticator to prove the account, then sets the session
// cookie. It needs no user name: the passkey names the account.
export async function signIn() {
  const options = requestOptions(await post('/v1/passkeys/login/begin'));
  const cred = await navigator.credentials.get({ publicKey: options });
  if (!cred) throw new Error('the browser returned no passkey');
  const body = {
    id: cred.id,
    rawId: toB64(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toB64(cred.response.clientDataJSON),
      authenticatorData: toB64(cred.response.authenticatorData),
      signature: toB64(cred.response.signature),
      userHandle: cred.response.userHandle ? toB64(cred.response.userHandle) : '',
    },
  };
  return post('/v1/passkeys/login/finish', body);
}

// --- the list ----------------------------------------------------------------

export async function list() {
  const res = await fetch('/v1/passkeys');
  if (!res.ok) {
    const err = new Error('cannot read the passkeys');
    err.status = res.status;
    throw err;
  }
  const data = await res.json();
  return data.passkeys || [];
}

export async function remove(id) {
  const res = await fetch('/v1/passkeys/' + encodeURIComponent(id), { method: 'DELETE' });
  if (!res.ok) {
    const err = new Error('cannot remove that passkey');
    err.status = res.status;
    throw err;
  }
}

export async function signOut() {
  await post('/v1/logout');
}

// message turns any failure into one line a person can act on. A user who
// cancels the browser prompt is not an error, so that case says nothing.
export function message(err) {
  if (!err) return '';
  if (err.name === 'NotAllowedError' || err.name === 'AbortError') return '';
  if (err.name === 'InvalidStateError') return 'That passkey is enrolled already.';
  return err.message || String(err);
}
