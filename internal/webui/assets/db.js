// SPDX-License-Identifier: AGPL-3.0-or-later

// The local copy of the account, on the device.
//
// Why this file exists. The web app kept its whole state in one localStorage
// key: one JSON string, rewritten on every keystroke, inside a quota of about
// five megabytes. That was the one known shortcut in the web app, and it had
// two costs. A decade of history does not fit in the quota, and a household
// now keeps two copies of the shared rows. Rewriting the whole string to
// change one row is the other cost, and it grows with the account.
//
// IndexedDB answers both: it is asynchronous, it holds gigabytes, and it
// writes one row without touching the rest. See D-020 in docs/DECISIONS.md for
// why the plan named SQLite in OPFS and this is not that.
//
// The shape here is one object store per table, keyed by the row id, plus one
// small `meta` record for what belongs to this device: the sync version, the
// outbox, the layout and who is asking. A row of a table is account data that
// the server can rebuild. The outbox is not, so it is written with the same
// call and never later.
//
// A backend answers two calls, so a test can pass one that Node can run:
//
//   readAll(store) -> Promise<rows[]>
//   write(ops)     -> Promise<void>   ops: [{store, put}] or [{store, del}]
//
// localBackend is the fallback for a browser with no IndexedDB, or one that
// refuses to open it, which is what a private window does. It keeps the old
// single string, so a device that cannot hold a real database still works with
// the state it can hold.

// TABLES are the object stores that hold rows. META holds the one record that
// belongs to this device.
export const TABLES = ['tasks', 'projects', 'sections', 'labels', 'reminders', 'comments'];
export const META = 'meta';
const STORES = TABLES.concat([META]);

// KEY is the localStorage key the app used before this file, and the key the
// fallback still uses.
const KEY = 'teha';
const DB_NAME = 'teha';
const DB_VERSION = 1;

// storeOf says which table a command changes. Every local write goes through
// one function in app.js, and the command type is what that function knows, so
// this is the whole of the dirty tracking.
//
// A command that touches rows the client cannot name from its arguments, such
// as a label delete that moves every task carrying the label, is not a gap: the
// server answers with those rows and the next pull marks them.
export function storeOf(type) {
  const at = String(type || '').indexOf('_');
  const head = at < 0 ? type : String(type).slice(0, at);
  switch (head) {
    case 'task': return 'tasks';
    case 'project': return 'projects';
    case 'section': return 'sections';
    case 'label': return 'labels';
    case 'reminder': return 'reminders';
    case 'comment': return 'comments';
    default: return '';
  }
}

// --- the IndexedDB backend --------------------------------------------------

function wrap(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

// idbBackend opens the database and returns a backend, or null when the
// browser has no IndexedDB or refuses to open it.
export async function idbBackend(name = DB_NAME) {
  if (typeof indexedDB === 'undefined') return null;
  let db;
  try {
    db = await new Promise((resolve, reject) => {
      const req = indexedDB.open(name, DB_VERSION);
      req.onupgradeneeded = () => {
        for (const store of STORES) {
          if (!req.result.objectStoreNames.contains(store)) {
            req.result.createObjectStore(store, { keyPath: store === META ? 'key' : 'id' });
          }
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
      req.onblocked = () => reject(new Error('the database is blocked by another tab'));
    });
  } catch (e) {
    return null;
  }
  return {
    kind: 'indexeddb',
    readAll: (store) => wrap(db.transaction(store, 'readonly').objectStore(store).getAll()),
    write: (ops) => new Promise((resolve, reject) => {
      if (!ops.length) return resolve();
      // One transaction for the whole batch. A tick of the app can move a
      // task, its project and the outbox, and those three must land together
      // or a reload sees a task in a list that is not there.
      const names = [...new Set(ops.map((o) => o.store))];
      const tx = db.transaction(names, 'readwrite');
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
      for (const op of ops) {
        const os = tx.objectStore(op.store);
        if (op.del !== undefined) os.delete(op.del);
        else os.put(op.put);
      }
    }),
  };
}

// --- the localStorage fallback ----------------------------------------------

// localBackend keeps every store in one string, which is what the app did
// before IndexedDB. It rewrites the whole string on every write, and that is
// the cost of the fallback: it is for a browser that cannot do better.
export function localBackend(storage = typeof localStorage === 'undefined' ? null : localStorage) {
  if (!storage) return null;
  const read = () => {
    try {
      return JSON.parse(storage.getItem(KEY) || '{}') || {};
    } catch (e) {
      return {};
    }
  };
  // LEGACY are the keys an older build wrote at the top level of this same
  // string, before the device record had a store of its own.
  const LEGACY = ['version', 'outbox', 'layout', 'cal', 'shopClear', 'me', 'inbox', 'people', 'shares'];
  return {
    kind: 'localstorage',
    readAll: async (store) => {
      const all = read();
      if (store !== META) return all[store] || [];
      if (Array.isArray(all[META])) return all[META];
      // An older build kept the device record at the top level. Read it there,
      // so a browser that has no IndexedDB keeps what it held.
      const old = {};
      for (const key of LEGACY) if (all[key] !== undefined) old[key] = all[key];
      return Object.keys(old).length ? [{ key: 'state', ...old }] : [];
    },
    write: async (ops) => {
      const all = read();
      for (const op of ops) {
        const rows = all[op.store] || [];
        const key = op.store === META ? 'key' : 'id';
        const id = op.del !== undefined ? op.del : op.put[key];
        const at = rows.findIndex((r) => r[key] === id);
        if (op.del !== undefined) {
          if (at >= 0) rows.splice(at, 1);
        } else if (at >= 0) {
          rows[at] = op.put;
        } else {
          rows.push(op.put);
        }
        all[op.store] = rows;
      }
      // The device record has a store of its own now, so the top-level copy
      // goes. Two records that mean the same thing drift apart.
      if (Array.isArray(all[META])) for (const key of LEGACY) delete all[key];
      try {
        storage.setItem(KEY, JSON.stringify(all));
      } catch (e) { /* a full quota must never break the UI */ }
    },
  };
}

// --- the cache --------------------------------------------------------------

// Cache holds what changed and writes it behind the screen.
//
// Nothing here waits for the disk. A write is marked, the screen draws, and
// the batch lands on a timer. A page that is going away flushes at once, which
// is what flushNow is for.
export class Cache {
  constructor(backend, { delay = 150 } = {}) {
    this.backend = backend;
    this.delay = delay;
    // pending is store -> id -> row, and a null row means the row is gone.
    this.pending = new Map();
    this.timer = null;
    this.writing = null;
    // failures counts what the backend refused, so the app can say so once
    // rather than swallowing every error for ever.
    this.failures = 0;
  }

  get kind() { return this.backend ? this.backend.kind : 'none'; }

  // read returns every row of every table, plus the meta record.
  async read() {
    const out = { meta: {}, rows: {} };
    if (!this.backend) return out;
    for (const table of TABLES) {
      try {
        out.rows[table] = await this.backend.readAll(table);
      } catch (e) {
        out.rows[table] = [];
      }
    }
    try {
      const meta = await this.backend.readAll(META);
      out.meta = meta.find((m) => m.key === 'state') || {};
    } catch (e) { /* a first run has none */ }
    return out;
  }

  // mark records one row. A null row means the app dropped it.
  mark(table, id, row) {
    if (!table || !id) return;
    if (!this.pending.has(table)) this.pending.set(table, new Map());
    this.pending.get(table).set(id, row || null);
    this.schedule();
  }

  // markMeta records the one record that belongs to this device.
  markMeta(state) {
    this.mark(META, 'state', { ...state, key: 'state' });
  }

  schedule() {
    if (this.timer || !this.backend) return;
    this.timer = setTimeout(() => { this.timer = null; this.flush(); }, this.delay);
  }

  // flush writes what is pending. Two calls at once are one write: the second
  // waits for the first, so an op can never be dropped between them.
  flush() {
    if (this.writing) {
      this.again = true;
      return this.writing;
    }
    const ops = this.take();
    if (!ops.length || !this.backend) return Promise.resolve();
    this.writing = this.backend.write(ops)
      .catch((e) => {
        this.failures++;
        // Put back what did not land. The device record carries the outbox,
        // which is the one thing the server cannot rebuild, so a batch that
        // failed must not be dropped on the floor.
        //
        // A newer mark of the same row wins: it is what the app holds now.
        // Nothing is scheduled here on purpose. The next edit flushes these
        // with it, and a timer that retried a broken backend every 150
        // milliseconds would spin for as long as the page is open.
        for (const op of ops) {
          const key = op.store === META ? 'key' : 'id';
          const id = op.del !== undefined ? op.del : op.put[key];
          if (!this.pending.has(op.store)) this.pending.set(op.store, new Map());
          const rows = this.pending.get(op.store);
          if (!rows.has(id)) rows.set(id, op.del !== undefined ? null : op.put);
        }
      })
      .then(() => {
        this.writing = null;
        if (this.again) { this.again = false; return this.flush(); }
        return undefined;
      });
    return this.writing;
  }

  // flushNow writes at once, for a page that is going away.
  flushNow() {
    if (this.timer) { clearTimeout(this.timer); this.timer = null; }
    return this.flush();
  }

  take() {
    const ops = [];
    for (const [table, rows] of this.pending) {
      for (const [id, row] of rows) {
        ops.push(row === null ? { store: table, del: id } : { store: table, put: row });
      }
    }
    this.pending.clear();
    return ops;
  }
}

// --- the move out of localStorage -------------------------------------------

// carryOver moves the old single string into the new database, once.
//
// It runs before the first read, and only when the new database is empty and
// the old key is there. The old key is removed afterwards, because two copies
// of the same account on one device is the bug this file is fixing.
//
// The old shape is one object with a list per table. The new shape is the same
// lists in their own stores, so the move is a copy and never a conversion.
export async function carryOver(cache, storage = typeof localStorage === 'undefined' ? null : localStorage) {
  if (!cache.backend || cache.kind !== 'indexeddb' || !storage) return false;
  let raw;
  try {
    raw = storage.getItem(KEY);
  } catch (e) {
    return false;
  }
  if (!raw) return false;
  const held = await cache.read();
  const already = held.meta.version !== undefined
    || TABLES.some((t) => (held.rows[t] || []).length);
  if (already) {
    // The new database is the truth. Drop the old key so nothing reads it.
    try { storage.removeItem(KEY); } catch (e) { /* nothing to do */ }
    return false;
  }
  let old;
  try {
    old = JSON.parse(raw);
  } catch (e) {
    try { storage.removeItem(KEY); } catch (e2) { /* nothing to do */ }
    return false;
  }
  for (const table of TABLES) {
    for (const row of old[table] || []) {
      if (row && row.id) cache.mark(table, row.id, row);
    }
  }
  const { tasks, projects, sections, labels, reminders, comments, ...meta } = old;
  cache.markMeta(meta);
  await cache.flushNow();
  if (cache.failures) return false;
  try { storage.removeItem(KEY); } catch (e) { /* nothing to do */ }
  return true;
}
