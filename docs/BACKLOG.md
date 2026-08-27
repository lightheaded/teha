# Backlog

Everything knowingly left unfinished, with the reason. A line leaves this page
when the work is done or when a decision says it will never happen.

## Accounts and passkeys

*Added 2026-08-27, with the passkey work. See [DECISIONS.md](DECISIONS.md) D-009.*

- **A second account, and sharing. This is milestone M6 and it is out of scope
  here.** The server holds one account row, one user handle and one set of
  passkeys. Nothing in the passkey code models two people. M6 needs an account
  id on the session, an invite that is not the owner's device token, a project
  shared between two accounts, and an assignee per task. The `account` table and
  the `credential.account_id` column exist so that work adds rows rather than
  rewrites the schema.
- **Sessions live in memory, so a restart signs every browser out.** A passkey
  login is one tap, and keeping a usable bearer secret out of the database has
  its own value. A session table becomes worthwhile with the second account,
  because a session must then name which account it belongs to.
- **The lockout counters live in memory as well.** A restart clears them, so a
  patient attacker can wait for a deployment. The public entry point has its own
  rate limit in front of this, and the account budget is the second wall.
- **A passkey session cannot enrol another passkey.** The device token is the
  only invitation, which is the strict reading of invite-only. The cost is that
  the owner pastes the token once more to add a second passkey. Revisit when an
  invite exists that is not the token.
- **The device token is one shared string, not one token per device.** PLAN.md
  §6.6 asks for named, listed and revocable per-device tokens, hashed at rest.
  A leaked token is still replaced by an environment variable and a restart.
- **No login notification.** §6.6 asks for one. Nothing in this build sends mail
  or push, so there is nothing to send it with. It arrives with reminders.
- **A login is not written to an activity log.** §6.6 asks for an audit trail in
  `activity`, visible in the UI. There is no `activity` table yet. Every login,
  every refusal and every enrolment goes to the server log today.
- **The raw attestation is not stored.** The library asks an implementer to keep
  it, so a credential can be checked against the FIDO metadata service later.
  This build runs no metadata service and asks for no attestation, so the bytes
  would be dead weight. Storing them is a schema addition, not a rewrite.
- **No CSRF token on the cookie session.** §6.6 asks for one. `SameSite=Lax` on
  both cookies means a browser sends neither on a cross-site POST, which is the
  defence that actually runs today. A token in a header is the belt beside that
  brace, and it belongs with the second account, because that is when a session
  starts naming who it is.
- **Without `TEHA_RP_ID` the relying party follows the `Host` header.** A caller
  that writes any host makes a ceremony pinned to that host. It cannot reach the
  owner's credentials, because an authenticator releases a passkey for one
  relying-party id only, and enrolment needs the device token. The result is a
  row that no real login ever matches, so it is a nuisance and not an entry.
  DEPLOY.md tells an operator to set both values on a public hostname. A future
  build can refuse to start when neither is set and the address is not loopback.
- **A `login/finish` with no ceremony does not count as a failure.** It tests no
  credential, so it costs an attacker nothing and earns nothing. A flood of them
  is a request-rate problem, which §6.6 leaves to the public entry point.
- **No recovery path except the device token.** A lost passkey is recovered by
  signing in with the token and enrolling a new one. There are no recovery
  codes.

## The web app

- **The README screenshots are stale for the header.** The header now carries a
  **Settings** button, so `docs/screenshots/*.png` no longer match the app and
  the `screenshots` job fails until somebody runs `scripts/screenshots.sh` and
  commits the result. The images were left alone on purpose: three branches
  changed the web app at the same time, and regenerating a binary on each one
  conflicts three ways for no gain. Regenerate once, after the merge.
- **No screenshot covers the settings sheet.** D-006 asks for one image per
  screen. Add the capture to `scripts/screenshots.mjs` in the same commit that
  regenerates the images, and wait for the passkey list to answer rather than
  photographing its "Reading…" state.
- **The web app still keeps its state in `localStorage`, not in OPFS with
  SQLite.** This is the one known shortcut in the web app, and it predates the
  passkey work.
||||||| 01c9321

## Reminders and notifications

Added 2026-08-27 with Web Push (D-003, D-010, D-011).

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
  loses that notification, per D-010. A retry needs a delivery attempt table.
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
  build does not have". D-010 leans on the restore behaviour, so the rehearsal
  is worth more now than it was.
