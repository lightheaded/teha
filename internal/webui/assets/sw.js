// SPDX-License-Identifier: AGPL-3.0-or-later

// A small offline shell. The app data lives in IndexedDB, which the app reads
// itself, so the worker only needs to keep the shell reachable with no
// network.
const CACHE = 'teha-shell-v9';
// Every file the app needs for a cold start with no network. parse.js and
// passkey.js are modules that app.js imports, so a missing entry here breaks
// the offline start.
const SHELL = ['/', '/app.js', '/parse.js', '/passkey.js', '/md.js', '/filter.js', '/db.js', '/activity.js',
  '/band.js', '/manifest.webmanifest', '/icon.svg'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys().then((keys) =>
    Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.pathname.startsWith('/v1/') || url.pathname.startsWith('/mcp')) return;
  e.respondWith(
    fetch(e.request).then((res) => {
      const copy = res.clone();
      caches.open(CACHE).then((c) => c.put(e.request, copy));
      return res;
    }).catch(() => caches.match(e.request).then((r) => r || caches.match('/')))
  );
});

// --- notifications ----------------------------------------------------------
// The server sends the JSON that internal/push writes: title, body, tag, url,
// task_id and kind. Those field names are the contract between the two.

self.addEventListener('push', (e) => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch (err) { d = {}; }
  // The tag names the notification. Two messages with one tag collapse into
  // one in the tray, so even a duplicate push shows the person one line.
  e.waitUntil(self.registration.showNotification(d.title || 'teha', {
    body: d.body || '',
    tag: d.tag || 'teha',
    renotify: false,
    icon: '/icon.svg',
    badge: '/icon.svg',
    data: { url: d.url || '/', task_id: d.task_id || '' },
  }));
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const data = e.notification.data || {};
  const url = data.url || '/';
  e.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const c of open) {
      if (new URL(c.url).origin !== self.location.origin) continue;
      // The app is open already. Tell it which task to show, then raise the
      // window, rather than loading a second copy of the app.
      c.postMessage({ type: 'open-task', task_id: data.task_id || '', url });
      return c.focus();
    }
    return self.clients.openWindow(url);
  })());
});
