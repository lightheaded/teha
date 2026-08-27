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

## Sections and the board layout, 2026-08-27

- **`docs/screenshots/board.png` is not generated.** `scripts/screenshots.mjs`
  captures the board now, and the image is not in `docs/screenshots/`, because
  Docker does not run in the environment that wrote the layout. The screenshots
  workflow fails until somebody runs `scripts/screenshots.sh` once and commits
  the new file. Nothing else in the repository points at the missing image, so
  no README shows a broken picture. The six older images are untouched, and the
  seed change was written to keep them identical.
- **The list layout has no drag.** Only the board does. A list row is sorted by
  the due date, then the priority, then the title, so a drag in the list would
  write an order key that no view reads. Give the list an order-key sort first,
  then reuse the board drag.
- **Quick add does not read `/Section`.** PLAN.md section 6.4 lists a section in
  the parser output. `quickadd` is Apache-2.0 and shared with the phone, so the
  term needs a fixture in `parser-fixtures/quickadd.json` and both parsers, which
  is a change of its own.
- **The phone has no board and no section field.** The Android client keeps the
  same rows in Room under different names, so `section_id` needs a column there
  and a mapping in the sync code.
- **The command line client cannot name a section.** `teha add` and `teha ls`
  pass a filter through, so `/Section` already works in `teha ls`. Writing a
  section from the command line does not.
- **A deleted project keeps its sections live.** `project_delete` is a soft
  delete and it already leaves its tasks alone, so the sections follow the same
  rule. A restore therefore brings the whole project back. Revisit with a real
  hard delete.

## Sync and the store

- **Three clients still write the literal `m` into `order_key`.** The
  fractional index now exists as the `order` package with a property test, and
  D-013 in [DECISIONS.md](DECISIONS.md) records why. No client calls it: the
  web app, the Android repository and the store all write `m`, and the importer
  writes a fixed-width number of the Todoist child order. Until a client adopts
  the package, a list falls back to its secondary sort keys and a drag cannot be
  saved. Adopting it is client work in three places, so it waits for the next
  client milestone.

- **The fractional index only halves a gap, so a key grows.** About one
  character for every six insertions into the same point: 2 000 insertions into
  one gap give a key of 401 characters, and 2 000 appends at the end give 334.
  The order never breaks and no two keys collide, so nothing is lost, and the
  cost is length in a text column. The fix is the integer-prefix form of the
  index, where a key carries a magnitude as well as a fraction and an append
  costs no length at all. It is a rewrite of one function and its property
  test, and it is not needed until a person reorders a long list every day.

- **Two label rows can hold one name.** `setLabels` matches a label by name,
  and `label_add` trusts the id it is given. A `label_add` for a name that a
  quick add created inline, or a `task_update` that names a label after a
  `label_delete`, makes a second row with the same name. Only the importer
  sends `label_add` today, and it sends every label before every task, so the
  path is not reachable from a client. The answer is a unique index on the live
  name, which is a schema change. `TestPropertyCommandsCommute` keeps the two
  name spaces apart on purpose and says why.

- **A `project` row and a `label` row carry no `source_ref`.** Only a task
  does. The importer therefore matches a project and a label by name, so a
  project renamed in Todoist after an import arrives as a second project. The
  answer is a `source_ref` column on both tables, which is a schema change.

## The filter language

- **There is no comment table, so `comment:` searches the description.** A
  comment lives in the description today: the importer folds one in and every
  client writes one there. The term points at the right column and means the
  right thing, and it needs one line of change on the day the table arrives.
  §6.3 of [PLAN.md](PLAN.md) counts "query comment text" as closed only when a
  comment is a row.

- **`with subtasks:` takes one term, not a group.** The lexer splits a query on
  `&`, `|` and the parentheses before a term is read, so the value of a
  `key: value` term cannot hold an operator. `with subtasks: p1 & #Home` reads
  as a family of `p1`, and then `& #Home`, which is the useful reading and not
  the only possible one. A group needs a real unary operator in the grammar,
  and that needs a character the lexer does not already use for a relative date
  such as `after: +5 days`.

- **No client offers the three gap terms in its own view list.** The grammar
  answers them everywhere, and a person has to type them.

## The importer

- **A saved filter is read and never written.** There is no `filter` table. The
  importer names every filter and prints every query in the summary, so a
  person can type them back in, and the grammar is the same grammar. A filter
  table is Phase 1 work in the plan and it needs a schema change.

- **A section is folded into the description.** There is no `section` table.
  The name arrives as the first line of the description and the summary counts
  it. Recorded in [POC.md](POC.md) already.

- **A project comment is counted and dropped.** Our model has no comment on a
  project. The summary says how many.

- **The archive never arrives.** A Todoist full sync sends the count of
  completed tasks per project and never the tasks. §4 of the plan lists
  "completed history" as part of the import, and what arrives is a completed
  task that is still in the active payload plus a count of the rest. Reading
  the real archive needs the completed-items endpoint and its own paging, and
  the fixture cannot rehearse it until that code exists.

## The quick add parser

- **A date phrase inside quotes does not stay literal.** §6.4 of
  [PLAN.md](PLAN.md) states the rule and neither parser has it, so
  `Read "tomorrow never dies"` loses the words from the title and takes a due
  date nobody asked for. The fix is a mask over every quoted span before the
  patterns run, in the Go parser and in `parse.js` together, plus a fixture in
  `parser-fixtures/quickadd.json` so the two cannot drift. It is small and it
  touches the web asset, which another milestone is editing.

- **`/section` is not parsed at all.** §6.4 names `#`, `@` and `/`. There is no
  section table, so a parsed `/section` would have nothing to resolve against.

- **A `#` or an `@` is consumed with no account to check it against.** §6.4
  says a token is consumed only when it matches something in the account. The
  parser has no account, so it always consumes, and each caller decides what an
  unknown name means. All three callers now agree that it means the inbox or a
  clear refusal, and `TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere`
  locks that.

## A project name that holds a filter operator reaches no client

**Found:** 2026-08-27 · **Where:** `filter/filter.go`, every client

A view of a project is the query `#Name`. The grammar reads `&`, `|`, `,`, `!`,
`(` and `)` as operators, so a project named `Home & Garden` compiles to `#Home`
AND the word `Garden`, and the view is empty or wrong. The browser builds the
same string and has the same gap.

**Why it waits.** The fix is a quoted name in the grammar, `#"Home & Garden"`.
That is three parsers, not one: the Go compiler, the JavaScript evaluator in the
web app, and the corpus that holds them together. The phone would drift from the
browser until all three land. No project in the account carries such a name
today.

**What closes it.** A quoted form in the lexer, the same form in
`internal/webui/assets/app.js`, and cases in `filter/filter_test.go`. A view of a
project could then also carry the project id instead of the name, which is exact.

---

## The browser reads a subset of the filter grammar

**Found:** before 2026-08-27 · **Where:** `internal/webui/assets/app.js`

The browser evaluates the filter in JavaScript over the rows in
`localStorage`. It knows fewer terms than the server, and a term it does not know
becomes a title search, so a view can quietly show the wrong rows.
`docs/USAGE.md` holds the table of what it knows.

The phone no longer has this gap. It calls the shared Go compiler through the
gomobile binding, so it reads every term the server reads.

**Why it waits.** The web app needs the compiler, not a second evaluator. Two
ways in: the shared packages compiled to WebAssembly, or a `POST /v1/query` that
answers with ids. Both are bigger than a filter fix, and the second one only
works online.

---

## The phone refuses a section term

*Added 2026-08-28, when the sections work and the two filter dialects met.*

The section table landed in the browser and on the server. Room holds no
`section` table and no `sectionId` column, so `filter.RoomSchema` leaves both
names empty and `/Winter` or `no section` fails on the phone with a sentence.
The failure is deliberate: a compiled statement that names a table Room never
declared is a crash at run time, and a sentence is not.

The phone therefore reaches every built-in view and every project, and it
cannot reach a section. Closing this needs a `section` entity in Room, a
`sectionId` on `TaskEntity`, a Room migration, and the section rows in the sync
pull. The board layout itself is a separate job after that.

## The phone refuses `created:`

**Found:** 2026-08-27 · **Where:** `filter/schema.go`, `RoomSchema`

The Room database keeps no creation date, so the term cannot be answered there.
The compiler fails with a sentence that says so. It does not name a column that
the database has not got.

**Why it waits.** The column is one field on `TaskEntity` and one line in the
sync mapping, and no view needs it yet. A schema change costs one full pull, so
it travels with the next change that needs one.

---

## The phone keeps no order inside a day

A view sorts by the day, then the priority, then the order key. The server holds
a fractional order key per project, and the phone never edits it, so a task
cannot be dragged into place on the phone. Nothing is lost, and the browser
cannot reorder either.

## Older, still open

- **The web app keeps its state in `localStorage`, not in OPFS with SQLite.**
  PLAN.md §8a says the same. It holds thousands of tasks and it must change
  before it holds a decade of history.
- **Litestream restore is not rehearsed.** POC.md says so under "What this
  build does not have". D-010 leans on the restore behaviour, so the rehearsal
  is worth more now than it was.
