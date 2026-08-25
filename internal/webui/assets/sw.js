// SPDX-License-Identifier: AGPL-3.0-or-later

// A small offline shell. The app data lives in localStorage, so the worker
// only needs to keep the shell reachable with no network.
const CACHE = 'teha-shell-v2';
// Every file the app needs for a cold start with no network. parse.js is a
// module that app.js imports, so a missing entry here breaks the offline start.
const SHELL = ['/', '/app.js', '/parse.js', '/manifest.webmanifest', '/icon.svg'];

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
