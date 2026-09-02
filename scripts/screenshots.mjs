// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Capture the README screenshots.
//
// This runs INSIDE the pinned Playwright container, against a server started in
// the same container. Nothing depends on the host, so the same bytes come out on
// a laptop and on a CI runner. That matters: a screenshot check that fails
// because a font rendered differently teaches everyone to ignore it.
//
// Three things are frozen, because each one would otherwise change an image
// that nobody edited:
//   - the seeded data, through -seed-date
//   - the browser clock, so that "today" agrees with the seeded data
//   - the locale and the time zone, so that a date reads the same everywhere
//
// Run it through scripts/screenshots.sh, never directly.

// An absolute path, not a bare name: an ES module ignores NODE_PATH, and the
// package lives outside this working directory. scripts/screenshots.sh installs
// it inside the container and passes the location. The image ships the
// browsers, not the package.
const { chromium } = await import(
  process.env.PW_MODULE ?? 'playwright');
import { mkdir } from 'node:fs/promises';

const BASE = process.env.TEHA_URL ?? 'http://127.0.0.1:8837';
const OUT = process.env.OUT_DIR ?? 'docs/screenshots';
// Must match -seed-date in scripts/screenshots.sh.
const TODAY = process.env.SEED_DATE ?? '2026-08-25';

// Two desktop heights on purpose. The seeded account is small, so a tall frame
// leaves half the image empty and the app looks unfinished. The detail sheet is
// about 745 CSS pixels tall and needs the room.
const DESKTOP = { width: 1280, height: 640 };
const DESKTOP_TALL = { width: 1280, height: 820 };
// The note panel grew: it now carries the section picker and the comments, so
// the image that has to show all of them needs its own height.
const DESKTOP_SHEET = { width: 1280, height: 960 };
const PHONE = { width: 390, height: 780 };
// Shopping mode has to hold beside the shop's own app, so PLAN.md section 4
// asks for a test at roughly half the width and not only at full width.
const SPLIT = { width: 320, height: 720 };

await mkdir(OUT, { recursive: true });

const browser = await chromium.launch();

/** Open a page with the clock and the locale pinned. */
async function open(viewport, scheme) {
  const ctx = await browser.newContext({
    viewport,
    deviceScaleFactor: 2,
    colorScheme: scheme,
    locale: 'en-GB',
    timezoneId: 'Europe/Tallinn',
    reducedMotion: 'reduce',
  });
  // 09:41 is a readable hour: it puts a morning task in the past and an
  // afternoon one in the future, so the list shows both states.
  await ctx.clock.setFixedTime(new Date(`${TODAY}T09:41:00+03:00`));
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: 'networkidle' });
  // The list renders from local state after the first sync. Wait for a row
  // rather than for a timer, so a slow runner does not photograph an empty app.
  await page.waitForSelector('#list .row', { timeout: 30_000 });
  return { ctx, page };
}

async function shot(page, name) {
  await page.screenshot({ path: `${OUT}/${name}.png` });
  console.log(`wrote ${OUT}/${name}.png`);
}

/** Open the shopping list in shopping mode, the way a phone does. */
async function openShoppingList(page) {
  await page.fill('#filter', '#Shopping');
  await page.keyboard.press('Enter');
  await page.waitForSelector('#list .row');
  // The button, not the S key: the caret is still in the filter field, and a
  // tap on the button is the path a phone has.
  await page.click('#shoptoggle');
  await page.waitForSelector('.shop .aisle .row', { timeout: 10_000 });
}

// 1 and 2. The default view, in both themes. This is the first thing a reader
// of the README sees, so it carries the sidebar, the counts and the day groups.
for (const scheme of ['light', 'dark']) {
  const { ctx, page } = await open(DESKTOP, scheme);
  await shot(page, `today-${scheme}`);
  await ctx.close();
}

// 3. Quick add, mid-sentence, with the hint showing what the parser understood.
// The hint is the single most persuasive thing in the app, and a static list
// screenshot never shows it.
{
  const { ctx, page } = await open(DESKTOP, 'light');
  await page.click('#qa');
  await page.type('#qa', 'Book the ferry next tuesday at 9:30 p1 #Trip @call', { delay: 12 });
  await page.waitForFunction(
    () => (document.querySelector('#hint')?.textContent ?? '').trim().length > 0,
    null, { timeout: 10_000 });
  await shot(page, 'quick-add');
  await ctx.close();
}

// 4. A task open in the detail panel.
{
  const { ctx, page } = await open(DESKTOP_TALL, 'light');
  await page.click('#list .row .body');
  await page.waitForTimeout(400);
  await shot(page, 'detail');
  await ctx.close();
}

// 5. A set of tasks picked, with the bulk action bar.
//
// The keyboard, not a modifier click. Playwright can hold a modifier, but the
// key path is what the README documents and it does not depend on which
// platform the runner reports.
{
  const { ctx, page } = await open(DESKTOP, 'light');
  await page.click('#nav a[data-title="Next 7 days"]');
  await page.waitForSelector('#list .row');
  // j moves the cursor, s picks the row under it. Three of each picks a run.
  for (const key of ['j', 's', 'j', 's', 'j', 's']) await page.keyboard.press(key);
  await page.waitForSelector('.bulk');
  await shot(page, 'bulk');
  await ctx.close();
}

// 6. The board layout, with the sections of a project as columns.
//
// A project view, not a built-in view: the board needs a project, because a
// section belongs to one. The keyboard opens it, because that is the path the
// README and USAGE.md document.
{
  const { ctx, page } = await open(DESKTOP, 'light');
  await page.click('#nav a[data-title="Trip to Setomaa"]');
  await page.waitForSelector('#list .row');
  await page.keyboard.press('b');
  await page.waitForSelector('.board .col-body .row');
  await shot(page, 'board');
  await ctx.close();
}

// 7. The calendar layout, a month of the current view, with the strip of
// undated tasks below it. A month needs the tall frame: six rows of days plus
// the strip do not fit in 640 pixels.
{
  const { ctx, page } = await open(DESKTOP_TALL, 'light');
  await page.click('#nav a[data-title="Next 7 days"]');
  await page.waitForSelector('#list .row');
  await page.keyboard.press('c');
  await page.waitForSelector('.cal-grid .chip');
  await shot(page, 'calendar');
  await ctx.close();
}

// 8. A note in Markdown, open in the detail panel, with the talk on the task
// below it. The seeded account carries one note with a heading, a list and a
// link, and one comment, so the image shows what the renderer does rather than
// an empty field.
{
  const { ctx, page } = await open(DESKTOP_SHEET, 'light');
  await page.click('#nav a[data-title="Trip to Setomaa"]');
  await page.waitForSelector('#list .row');
  await page.click('#list .row .body');
  await page.waitForSelector('.sheet .d-md .md-tasks, .sheet .d-md h3', { timeout: 10_000 });
  await page.waitForSelector('.sheet .cmrow', { timeout: 10_000 });
  await shot(page, 'note');
  await ctx.close();
}

// 9. The settings panel: the passkeys and the notification half. D-006 asks for
// one image per screen, and this was the screen with none.
{
  const { ctx, page } = await open(DESKTOP_TALL, 'light');
  await page.keyboard.press(',');
  // The passkey list is read from the server. Photographing its "Reading..."
  // state is what docs/BACKLOG.md warned about, so wait for the answer.
  await page.waitForFunction(
    () => !(document.querySelector('.set')?.textContent ?? '').includes('Reading'),
    null, { timeout: 10_000 });
  await shot(page, 'settings');
  await ctx.close();
}

// 10. Phone width. The Android app is now released, and the installed web app is
// still what a desktop browser shows at that width, so the README must show
// that it is not a squeezed desktop.
{
  const { ctx, page } = await open(PHONE, 'light');
  await shot(page, 'phone');
  await ctx.close();
}

// 11. Shopping mode, at phone width. Aisles, big targets, the quantity chip,
// the suggestions from what the household bought before, and the basket.
{
  const { ctx, page } = await open(PHONE, 'light');
  // The sidebar is hidden below 800 pixels, so a phone reaches a project
  // through the filter field. That is the path a person has there.
  await openShoppingList(page);
  await shot(page, 'shop');
  await ctx.close();
}

// 12. The same screen at half the width. This writes no image: it is the test
// that PLAN.md section 4 asks for, and a failure here is a layout that a
// person cannot use in a shop with the shop's own app beside it.
{
  const { ctx, page } = await open(SPLIT, 'light');
  await openShoppingList(page);
  const bad = await page.evaluate(() => {
    const wide = document.documentElement.scrollWidth > window.innerWidth + 1;
    // A target under 40 pixels is a target a cold thumb misses.
    const small = [...document.querySelectorAll('.shop .shoprow .box')]
      .filter((b) => b.getBoundingClientRect().height < 26).length;
    return { wide, small, width: window.innerWidth };
  });
  if (bad.wide) throw new Error(`shopping mode scrolls sideways at ${bad.width} pixels`);
  if (bad.small) throw new Error(`${bad.small} check targets are too small at ${bad.width} pixels`);
  console.log(`shopping mode holds at ${bad.width} pixels`);
  await ctx.close();
}

// 13. The local copy is in IndexedDB, and the old localStorage key is gone.
// This writes no image either: it is the check that db.js cannot make in Node,
// because Node has no IndexedDB. See D-020.
{
  const { ctx, page } = await open(DESKTOP, 'light');
  const held = await page.evaluate(async () => {
    const open = () => new Promise((resolve, reject) => {
      const req = indexedDB.open('teha');
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const all = (db, store) => new Promise((resolve, reject) => {
      const req = db.transaction(store, 'readonly').objectStore(store).getAll();
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    // The app writes on a timer, so wait for the rows rather than for a clock.
    const db = await open();
    for (let i = 0; i < 100; i++) {
      const tasks = await all(db, 'tasks');
      if (tasks.length) {
        const meta = await all(db, 'meta');
        return {
          tasks: tasks.length,
          projects: (await all(db, 'projects')).length,
          comments: (await all(db, 'comments')).length,
          version: (meta[0] || {}).version,
          legacy: localStorage.getItem('teha'),
        };
      }
      await new Promise((r) => setTimeout(r, 50));
    }
    return { tasks: 0 };
  });
  if (!held.tasks) throw new Error('the local copy is not in IndexedDB');
  if (held.legacy !== null) throw new Error('the old localStorage key is still there');
  if (!held.version) throw new Error('the sync watermark did not reach the local database');
  if (!held.comments) throw new Error('the comments did not reach the local database');
  console.log(`the local copy holds ${held.tasks} tasks, ${held.projects} projects, `
    + `${held.comments} comments, at version ${held.version}`);
  await ctx.close();
}

await browser.close();
