# Backlog

Everything the build knowingly leaves unfinished, with the reason. A line here
is a decision to stop, not a forgotten task. PLAN.md holds the plan.
DECISIONS.md holds the choices with a lasting consequence.

---

## Reminders and notifications

Added 2026-08-27 with Web Push (D-003, D-009, D-010).

### Out of scope by design

- **Android local reminders.** The phone fires a due-time reminder from Room,
  with `AlarmManager` and no network, per PLAN.md §4. That is a different
  mechanism from Web Push, and it is better for the phone: it works with no
  data, and it needs no subscription and no push service. Web Push therefore
  serves the browser and the installed web app, which is what iOS and every
  desktop needs. The boundary is deliberate. A phone that is also subscribed in
  its browser can receive both, so the phone work must read the reminder rows
  and not add a second source of truth.
- **A reminder in quick add.** `remind me at 8` parses in no client yet. The
  quick add grammar carries a `reminders` field in PLAN.md §6.4 and the parser
  does not fill it. A reminder is set in the task detail sheet for now.
- **Comment and assignment notifications.** Phase 2 needs a second account
  first. The transport is ready for them.
- **UnifiedPush through ntfy.** PLAN.md §4 Phase 2 names it beside Web Push.
  Nothing needs it while one Web Push implementation reaches every client.

### Known limits of what shipped

- **One time zone, the server's own.** A daily digest counts "today" from the
  server clock, the same way `store.Apply` reads today for a recurrence. A
  digest at 08:00 local is right while the server and the person share a zone.
  A second account in another zone needs a zone per account.
- **A digest steps forward by exactly 24 hours.** So a change of daylight
  saving moves the digest by one hour until somebody sets the time again. The
  fix is a clock time and a zone in the reminder row, not a moment.
- **One reminder per task in the web app.** The table holds any number. The
  detail sheet writes one, because two notifications for one chore is noise.
- **No retry after a failure that is not a 429.** A 500 from a push service
  loses that notification, per D-009. A retry needs a delivery attempt table.
- **The scheduler polls every 30 seconds.** So a reminder can arrive up to 30
  seconds late. A person cannot feel that, and a timer per reminder costs a
  wake-up path that a restart must rebuild.
- **Nothing prunes a subscription that never fails.** A dead browser that
  answers 410 goes at once. A subscription that is simply never used stays for
  ever. `last_used_at` is in the table, so a prune is a single query when a
  deployment ever needs one.
- **The MCP server cannot set a reminder.** No tool covers the reminder
  commands yet, so an agent can name a due time but not a nudge.

---

## Older, still open

- **The web app keeps its state in `localStorage`, not in OPFS with SQLite.**
  PLAN.md §8a says the same. It holds thousands of tasks and it must change
  before it holds a decade of history.
- **Litestream restore is not rehearsed.** POC.md says so under "What this
  build does not have". D-009 leans on the restore behaviour, so the rehearsal
  is worth more now than it was.
