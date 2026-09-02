// Runs the local storage layer.
//   node --test internal/webui/assets/db.test.mjs
//
// Node has no IndexedDB, so these tests drive db.js through a backend of their
// own: the two calls a backend answers are readAll and write. What that leaves
// untested is the IndexedDB binding itself, and scripts/screenshots.mjs checks
// that in a real browser, against a real server.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Cache, TABLES, META, storeOf, localBackend, carryOver } from './db.js';

// fake is a backend that records every write, so a test can say what landed
// and in how many batches.
function fake(kind = 'indexeddb') {
  const rows = new Map(TABLES.concat([META]).map((s) => [s, new Map()]));
  const batches = [];
  return {
    kind,
    rows,
    batches,
    readAll: async (store) => [...(rows.get(store) || new Map()).values()],
    write: async (ops) => {
      batches.push(ops);
      for (const op of ops) {
        const key = op.store === META ? 'key' : 'id';
        if (op.del !== undefined) rows.get(op.store).delete(op.del);
        else rows.get(op.store).set(op.put[key], op.put);
      }
    },
  };
}

test('a command type names the table it changes', () => {
  assert.equal(storeOf('task_add'), 'tasks');
  assert.equal(storeOf('task_move'), 'tasks');
  assert.equal(storeOf('project_delete'), 'projects');
  assert.equal(storeOf('section_reorder'), 'sections');
  assert.equal(storeOf('label_add'), 'labels');
  assert.equal(storeOf('reminder_update'), 'reminders');
  assert.equal(storeOf('comment_add'), 'comments');
  // A type nobody has written yet must name no table, so a row is never
  // written into the wrong store.
  assert.equal(storeOf('invented_thing'), '');
  assert.equal(storeOf(''), '');
  assert.equal(storeOf(undefined), '');
});

test('one tick of the app is one write, and it holds every row it touched', async () => {
  const back = fake();
  const c = new Cache(back, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'Milk' });
  c.mark('tasks', 't2', { id: 't2', title: 'Bread' });
  c.mark('projects', 'p1', { id: 'p1', name: 'Shopping' });
  c.markMeta({ version: 7, outbox: [] });
  await c.flushNow();

  assert.equal(back.batches.length, 1, 'three rows and the meta record are one write');
  assert.equal(back.rows.get('tasks').size, 2);
  assert.equal(back.rows.get('projects').get('p1').name, 'Shopping');
  assert.equal(back.rows.get(META).get('state').version, 7);
});

test('the last mark of a row wins, and a null mark removes it', async () => {
  const back = fake();
  const c = new Cache(back, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'Milk' });
  c.mark('tasks', 't1', { id: 't1', title: 'Oat milk' });
  await c.flushNow();
  assert.equal(back.rows.get('tasks').get('t1').title, 'Oat milk');
  assert.equal(back.batches[0].length, 1, 'two marks of one row are one operation');

  c.mark('tasks', 't1', null);
  await c.flushNow();
  assert.equal(back.rows.get('tasks').size, 0);
});

test('a read gives back every table and the device record', async () => {
  const back = fake();
  const c = new Cache(back, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'Milk' });
  c.mark('comments', 'cm1', { id: 'cm1', task_id: 't1', body: 'The green one' });
  c.markMeta({ version: 3, outbox: [{ uuid: 'u1', type: 'task_add' }], layout: 'shop' });
  await c.flushNow();

  const held = await new Cache(back, { delay: 0 }).read();
  assert.equal(held.rows.tasks.length, 1);
  assert.equal(held.rows.comments[0].body, 'The green one');
  assert.equal(held.meta.version, 3);
  assert.equal(held.meta.layout, 'shop');
  assert.equal(held.meta.outbox.length, 1, 'the outbox is the one thing the server cannot rebuild');
  // A table with nothing in it reads as an empty list, never as undefined.
  for (const table of TABLES) assert.ok(Array.isArray(held.rows[table]));
});

test('a backend that refuses a write is counted and never throws', async () => {
  const broken = { kind: 'indexeddb', readAll: async () => [], write: async () => { throw new Error('no room'); } };
  const c = new Cache(broken, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1' });
  await c.flushNow();
  assert.equal(c.failures, 1);
});

// A failed write must not throw the rows away. The device record carries the
// outbox, and the outbox is the one thing the server cannot rebuild.
test('a write that failed is tried again with the next one', async () => {
  const back = fake();
  let refuse = true;
  const flaky = {
    kind: 'indexeddb',
    readAll: back.readAll,
    write: async (ops) => {
      if (refuse) throw new Error('no room');
      return back.write(ops);
    },
  };
  const c = new Cache(flaky, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'Milk' });
  c.markMeta({ version: 4, outbox: [{ uuid: 'u1', type: 'task_add' }] });
  await c.flushNow();
  assert.equal(c.failures, 1);

  // The next write carries the batch that failed as well.
  refuse = false;
  c.mark('tasks', 't2', { id: 't2', title: 'Bread' });
  await c.flushNow();

  const held = await c.read();
  assert.equal(held.rows.tasks.length, 2, 'the row from the failed batch is back');
  assert.equal(held.meta.outbox.length, 1, 'the outbox survived the failure');
});

// What the app holds now wins over what a failed write was carrying.
test('a newer mark is not overwritten by a failed batch', async () => {
  const back = fake();
  let refuse = true;
  const flaky = {
    kind: 'indexeddb',
    readAll: back.readAll,
    write: async (ops) => {
      if (refuse) throw new Error('no room');
      return back.write(ops);
    },
  };
  const c = new Cache(flaky, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'Old' });
  await c.flushNow();

  refuse = false;
  c.mark('tasks', 't1', { id: 't1', title: 'New' });
  await c.flushNow();
  const held = await c.read();
  assert.equal(held.rows.tasks[0].title, 'New');
});

test('two flushes at once lose no operation', async () => {
  const back = fake();
  const c = new Cache(back, { delay: 0 });
  c.mark('tasks', 't1', { id: 't1', title: 'One' });
  const first = c.flush();
  c.mark('tasks', 't2', { id: 't2', title: 'Two' });
  const second = c.flush();
  await Promise.all([first, second]);
  await c.flushNow();
  assert.equal(back.rows.get('tasks').size, 2);
});

// The fallback keeps the old single string. A browser that refuses IndexedDB,
// which is what a private window does, still holds what it can.
test('the localStorage fallback round-trips a row and a delete', async () => {
  const store = new Map();
  const storage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, v),
    removeItem: (k) => store.delete(k),
  };
  const c = new Cache(localBackend(storage), { delay: 0 });
  assert.equal(c.kind, 'localstorage');
  c.mark('tasks', 't1', { id: 't1', title: 'Milk' });
  c.markMeta({ version: 2 });
  await c.flushNow();

  let held = await c.read();
  assert.equal(held.rows.tasks[0].title, 'Milk');
  assert.equal(held.meta.version, 2);

  c.mark('tasks', 't1', null);
  await c.flushNow();
  held = await c.read();
  assert.equal(held.rows.tasks.length, 0);
});

test('the old single string moves into the database once', async () => {
  const store = new Map([['teha', JSON.stringify({
    version: 11,
    outbox: [{ uuid: 'u1', type: 'task_add' }],
    layout: 'board',
    tasks: [{ id: 't1', title: 'Milk' }, { id: 't2', title: 'Bread' }],
    projects: [{ id: 'p1', name: 'Shopping' }],
  })]]);
  const storage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, v),
    removeItem: (k) => store.delete(k),
  };
  const back = fake();
  const c = new Cache(back, { delay: 0 });

  assert.equal(await carryOver(c, storage), true);
  const held = await c.read();
  assert.equal(held.rows.tasks.length, 2);
  assert.equal(held.meta.version, 11);
  assert.equal(held.meta.outbox.length, 1, 'an unsent command must survive the move');
  assert.equal(held.meta.layout, 'board');
  // The old key is gone: two copies of one account on one device is the bug
  // this move is fixing.
  assert.equal(storage.getItem('teha'), null);

  // A second run has nothing to move.
  assert.equal(await carryOver(c, storage), false);
});

test('a database that already holds the account is never overwritten', async () => {
  const store = new Map([['teha', JSON.stringify({ version: 1, tasks: [{ id: 'old', title: 'Stale' }] })]]);
  const storage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, v),
    removeItem: (k) => store.delete(k),
  };
  const back = fake();
  const c = new Cache(back, { delay: 0 });
  c.mark('tasks', 'new', { id: 'new', title: 'Current' });
  c.markMeta({ version: 40 });
  await c.flushNow();

  assert.equal(await carryOver(c, storage), false);
  const held = await c.read();
  assert.equal(held.meta.version, 40);
  assert.equal(held.rows.tasks.length, 1);
  assert.equal(held.rows.tasks[0].id, 'new');
  // The stale key is dropped, so nothing reads it again.
  assert.equal(storage.getItem('teha'), null);
});

test('the fallback never moves the old string onto itself', async () => {
  const store = new Map([['teha', JSON.stringify({ version: 5, tasks: [{ id: 't1', title: 'Milk' }] })]]);
  const storage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, v),
    removeItem: (k) => store.delete(k),
  };
  const c = new Cache(localBackend(storage), { delay: 0 });
  // The fallback IS that string. A move would read it, write it back and then
  // remove it, which would throw the account away.
  assert.equal(await carryOver(c, storage), false);
  const held = await c.read();
  assert.equal(held.rows.tasks.length, 1);
  assert.equal(held.meta.version, 5);
});
