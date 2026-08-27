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
