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
const PHONE = { width: 390, height: 780 };

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

// 5. Phone width. The Android app is not released, so the installed web app is
// what a phone runs today, and the README must show that it is not a squeezed
// desktop.
{
  const { ctx, page } = await open(PHONE, 'light');
  await shot(page, 'phone');
  await ctx.close();
}

await browser.close();
