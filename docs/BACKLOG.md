# Backlog

Everything knowingly left unfinished, with the reason. A line leaves this page
when the work is done or when a decision says it will never happen.

## Accounts and passkeys

*Added 2026-08-27 with the passkey work. Most of it closed on 2026-08-31 with
the household: see [DECISIONS.md](DECISIONS.md) D-016 and D-017.*

- **A passkey session cannot enrol another passkey.** The device token is the
  only invitation into an account that already exists, which is the strict
  reading of invite-only. The cost is that a person pastes the token once more
  to add a second passkey. An invitation makes a new account, so it is not the
  answer here.
- **An invited person cannot enrol a passkey from the join screen.** Joining
  sets the token cookie, so the settings panel does let them add one after the
  first screen. One more step than it needs.
- **The device token is one string per account, not one per device.** PLAN.md
  §6.6 asks for named, listed and revocable per-device tokens. A leaked token
  is replaced by an environment variable and a restart for the owner, and there
  is no way at all to replace an invited person's token yet.
- **No login notification.** §6.6 asks for one. Push now exists, so this is a
  small job that nobody has done.
- **A login is not written to an activity log.** §6.6 asks for an audit trail
  in `activity`, visible in the UI. There is no `activity` table. Every login,
  every refusal, every enrolment and every invitation goes to the server log.
- **The raw attestation is not stored.** The library asks an implementer to
  keep it, so a credential can be checked against the FIDO metadata service
  later. This build runs no metadata service and asks for no attestation.
- **The lockout counters live in memory.** A restart clears them, so a patient
  attacker can wait for a deployment. The public entry point has its own rate
  limit in front of this. The counters guard the passkey login and the join
  route together, which is deliberate: both are guesses at a secret.
- **No CSRF token on the cookie session.** §6.6 asks for one. `SameSite=Lax` on
  both cookies means a browser sends neither on a cross-site POST, which is the
  defence that runs today. A token in a header is the belt beside that brace.
- **Without `TEHA_RP_ID` the relying party follows the `Host` header.** A caller
  that writes any host makes a ceremony pinned to that host. It cannot reach the
  owner's credentials, because an authenticator releases a passkey for one
  relying-party id only. DEPLOY.md tells an operator to set both values on a
  public hostname.
- **No recovery path except the device token.** A lost passkey is recovered by
  signing in with the token and enrolling a new one. There are no recovery
  codes. An invited person who loses both has to be invited again.

## The household

*Added 2026-08-31 with the second account. See D-016.*

- **A label is shared vocabulary, and anybody can delete one.** The `label`
  table is not scoped to an account, so two people draw from one set of names
  and `label_delete` passes the gate for either of them. A name only ever shows
  on a task, so nobody reads a label they have no task for, and no client sends
  `label_delete` today: only the importer writes labels by command. Scoping the
  table is a column and a migration.
- **A member cannot make a section in a shared list, but can move a task into
  one.** `section_add` needs the project, and the gate allows it, so this in
  fact works. What a member cannot do is rename or delete the list itself, and
  that is deliberate.
- **Unsharing costs the other client a full pull.** The delta cannot describe a
  row that went away, so the server answers `reset` and the client starts
  again. For an account with ten thousand tasks that is a 1.4 MB pull. A
  tombstone list per account would be smaller and it needs its own table.
- **The phone cannot share a list, and it cannot invite.** It joins a
  household, reads `reset`, assigns a task and files one under a heading since
  2026-09-02. Writing an invitation and sharing a list are the owner's jobs and
  they are in the browser only.
- **The phone has no comments and no shopping layout.** `comment:` fails there
  with a sentence, because Room keeps no comment table. Shopping mode is a
  layout, and the phone draws the list.
- **A task can be assigned to somebody who then loses the list.** Nothing
  clears the assignee when a share is taken back. The name simply stops
  resolving, and the row shows "someone". A new assignment to somebody out of
  reach is refused since 2026-09-02, so this is only what an unshare leaves
  behind.
- **The event stream and the health route leak activity, not data.**
  `/v1/events` carries one version number, so a second account is woken by the
  first account's writes and pulls nothing. `/v1/health` reports the same
  number with no token at all. Neither says what changed or whose it was. A
  version per account would fix both, and it costs the single ordering that
  makes the scoped pull simple.
- **An invited person cannot be removed.** There is no way to delete an account
  or to revoke a device token that has been used. Revoking an unused invitation
  works.
- **The owner cannot be a member of somebody else's list.** Sharing runs one
  way, from the owner of a list outwards, and any account may own a list. So
  this works, and what does not exist is a way for two accounts to co-own one.

## The web app

- **A comment cannot be found by the full-text index.** `task_fts` is written
  when a task changes, so it holds titles and descriptions. `comment:` reads
  the comment table with a LIKE instead. Putting comment text into the index
  means rewriting the row of a task whenever anybody says anything on it, and
  deciding what `search:` then means. See D-020.
- **A comment carries no attachment and no reaction.** §4 Phase 2 names both.
  An attachment needs a file store, which no part of this build has.
- **Shopping mode has no drag.** An item moves to another aisle through the
  **Section** field of the detail sheet. The board is where a pointer drags,
  and a drag in a cold aisle with one hand is not the gesture to design for.
- **Shopping mode has no quantity of its own.** `2x milk` is a count drawn from
  the title, and the title is what the server holds. A column would have to
  reach every client and every parser for a thing a person types in the title
  anyway.
- **A note renders Markdown and cannot be ticked in place.** A task list inside
  a note draws its boxes and they are a picture of the state, not a control.
  Ticking one has to write back into the text of the note.
- **A Markdown image is drawn as a link.** The content security policy allows
  `img-src 'self' data:`, so a remote picture would be a broken frame. A link
  says what it is and it opens.
- **The calendar shows the tasks of the current view only.** A task that the
  query excludes is not on the calendar, and a task due outside the window is
  counted rather than shown. There is no drag from one month into the next: a
  drop target has to exist on the screen.
- **The calendar has no time grid.** The week view is seven day columns, not
  hours down the side, so a task with a time sorts to the top of its day and
  does not sit at its hour. Time blocking is Phase 3 and it needs the grid.
- **The list layout has no drag.** Only the board does. A list row is sorted by
  the due date, then the priority, then the title, so a drag in the list would
  write an order key that no view reads. Give the list an order-key sort first,
  then reuse the board drag.
- **A project name that holds a filter operator still reaches no client.** A
  project named `Home & Garden` compiles to `#Home` AND the word `Garden`. The
  browser now behaves exactly as the server does, so the gap is one gap rather
  than two, and the fix is a quoted name in the grammar: `#"Home & Garden"`. It
  is three places now, not two: `filter/filter.go`, `filter.js` and the corpus.
- **Quick add does not read `/Section`.** PLAN.md §6.4 lists a section in the
  parser output. `quickadd` is Apache-2.0 and shared with the phone, so the
  term needs a fixture in `parser-fixtures/quickadd.json` and both parsers.
- **The phone has no board.** It holds the sections and it files a task with
  the **Section** field, and it draws no columns. A board is a pointer layout.
- **The command line client cannot name a section.** `teha add` and `teha ls`
  pass a filter through, so `/Section` already works in `teha ls`. Writing a
  section from the command line does not.
- **A deleted project keeps its sections live.** `project_delete` is a soft
  delete and it already leaves its tasks alone, so the sections follow the same
  rule. A restore therefore brings the whole project back.

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
- **A reminder in quick add reaches the browser and the command line, not the
  phone.** `remind me at 8` and `remind me 30m before` parse in both shared
  parsers, and `mobile.ParseQuickAdd` hands the fields to the Android client,
  which has no reminder row to write them into.
- **A notification for an assignment or a comment is sent and forgotten.** Both
  run since 2026-09-02. A reminder is claimed in the database first, because it
  fires from a timer that can run twice, and an event has already happened once
  inside one transaction. So a push that fails is a notification the person
  does not get, and nothing retries it. See D-020.
- **Nothing notifies a person that a list was shared with them.** The rows
  arrive on the next pull with no word about it.
- **UnifiedPush through ntfy.** PLAN.md §4 Phase 2 names it beside Web Push.
  Nothing needs it while one Web Push implementation reaches every client.

### Known limits of what shipped

- **One time zone, the server's own.** A daily digest counts "today" from the
  server clock, the same way `store.Apply` reads today for a recurrence. A
  digest at 08:00 local is right while the server and the person share a zone.
  Two accounts in two zones need a zone per account, and the household now
  makes that easy to hit.
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
  commands yet, so an agent can name a due time but not a nudge. It cannot
  assign a task or share a list either: the tools read and write tasks and
  projects, and the household is not among them. Every tool is scoped to the
  account whose token drove the call.

---

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

- **`comment:` is a LIKE over the comment table, not an index.** The gap §6.3
  of [PLAN.md](PLAN.md) names is closed since 2026-09-02: a comment is a row
  and the term reads it. What it is not is indexed, and `search:` therefore no
  longer finds a task by what somebody said on it. See D-020.

- **`with subtasks:` takes one term, not a group.** The lexer splits a query on
  `&`, `|` and the parentheses before a term is read, so the value of a
  `key: value` term cannot hold an operator. `with subtasks: p1 & #Home` reads
  as a family of `p1`, and then `& #Home`, which is the useful reading and not
  the only possible one. A group needs a real unary operator in the grammar,
  and that needs a character the lexer does not already use for a relative date
  such as `after: +5 days`.

- **No client offers the three gap terms in its own view list.** The grammar
  answers them everywhere, and a person has to type them. The browser now has a
  filter field, so typing one is at least quick.

- **The filter corpus writes its own answers.** `go test ./filter -update` runs
  every query against real SQLite and saves what came back. That is the right
  authority, and it means a reviewer has to read the diff: a wrong answer that
  both sides agree on is a wrong answer that passes.

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

- **A protected span covers a quoted phrase, a URL, a code span and a Markdown
  link, and nothing else.** Both parsers blank those spans before any pattern
  runs, so `Read "tomorrow never dies"` keeps its words and
  `https://example.org/v1.2` keeps its number. A phrase in single quotes is not
  protected, because an apostrophe is a comma character in ordinary English and
  half a line would go silent.

- **`/section` is not parsed at all.** §6.4 names `#`, `@` and `/`. There is no
  section table, so a parsed `/section` would have nothing to resolve against.

- **A `#` or an `@` is consumed with no account to check it against.** §6.4
  says a token is consumed only when it matches something in the account. The
  parser has no account, so it always consumes, and each caller decides what an
  unknown name means. All three callers now agree that it means the inbox or a
  clear refusal, and `TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere`
  locks that.

## The browser refuses `created:`

*The subset closed on 2026-08-31. See [DECISIONS.md](DECISIONS.md) D-018. The
project-name gap below it moved into "The web app", because it is now one gap
and not two.*

The browser reads every term the server reads, with one exception: the sync
payload carries no creation date, so `created:` fails there with a sentence.
The phone refuses the same term for the same reason. Closing it is one field on
the sync payload and one capability flag in `filter.js`.

---

## Nothing tests the Room migration

*Added 2026-09-02 with the household on the phone.*

Database version 2 adds `sectionId` and `assigneeId` to `tasks`, and the
`sections` and `accounts` tables. `TehaDatabase.MIGRATION_1_2` writes that by
hand, and Room compares the result with what it generates for the entities
every time it opens the file. A statement that disagrees is an exception at the
first open after the upgrade, for every person who already has the app.

The right test is `MigrationTestHelper`, which needs `exportSchema = true`, the
schema JSON of version 1 and version 2 in the repository, and a run on an
emulator. Version 1 was built with `exportSchema = false`, so its JSON does not
exist and cannot be written now without building the old code.

What guards it today: the types are the three Room writes for `String`,
`String?` and `Long`, the column order follows the data class, and the emulator
corpus job opens a database built from the entities. What is not guarded is the
upgrade of a file that a person already has.

## The phone refuses `comment:`

*Added 2026-09-02 with the comment table. The section term closed on the same
day: Room holds `sections`, `accounts`, `sectionId` and `assigneeId` since the
phone joined the household, so `/Winter`, `no section`, `assigned` and
`assigned to: me` all answer there now.*

Room holds no comment table, so `filter.RoomSchema` leaves the name empty and
`comment: words` fails on the phone with a sentence. The failure is deliberate.
The term read the description while a comment lived there, and that answer is
close and wrong now that a comment is a row: see D-020.

Closing it needs a `comment` entity in Room, the comment rows in the sync
mapping, and a place in the detail sheet to read and write one.

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

## The desktop shell (M5)

**`Cargo.lock` is in the repository since 2026-09-02.** A binary crate wants
one, or two builds a month apart resolve different patch versions. Every
version in it matches the exact pin in `Cargo.toml`.

**The shell was never compiled.** Every version, plugin name, permission string
and configuration key was read from the Tauri v2 documentation and from
crates.io on 2026-08-27, and every Rust file was written to be `rustfmt` clean.
No Rust ran. The first `make desktop-check` is the first compile, and CI on
macOS runs the same check. Expect a first run to report small things: a
formatting difference that `cargo fmt --all` repairs, or a method that moved
between Tauri 2.11 releases.

The parts that did run: the JavaScript parses under Node, the contract test with
the web app passes, the JSON files parse, and `tools/make-icons.py` wrote the
icons in this tree.

**The panel shows no parse hint.** The web app tells you what it understood
before you press `Enter`. The panel cannot, because the parser lives in the
page and the panel is a local window. Two ways out: a hook in the web app that
the shell can call for a parse, or a `wasm` build of `quickadd`. Neither is
worth it before the shell has been used for a week.

**`panel_submit` reports that the page took the line, not that the task
exists.** The shell puts the line into the field of the web app and returns.
The page reports a real failure in its own console. A round trip needs the page
to answer the shell, and a page from a remote origin reaches no command on
purpose. See `desktop/README.md`.

**The quick add contract is a DOM contract.** The shell finds the field by the
id `qa` and waits for it to clear. `desktop/tools/contract.test.mjs` fails when
a rename in `internal/webui/assets/app.js` breaks it, so the build reports it
rather than the shortcut. The real fix is a small hook in the web app, for
example `window.teha.quickAdd(line)`, and then the shell calls that instead. It
needs a change in the AGPL tree, which this change did not touch.

**The shortcut in the menu bar label goes stale.** A save in the settings window
registers the new shortcut at once, and the label reads the new one at the next
start. Rebuilding the menu on a save is the fix.

**No log file.** Lines go to standard error, which a bundled application throws
away unless a person starts the binary from a terminal. `tauri-plugin-log`
writes to `~/Library/Logs`, and it is one more pinned dependency. Add it when a
real failure needs a diagnosis.

**The focus return needs a person on real hardware.** The panel gives the
keyboard back because the shell hides itself when the panel was its only window
on the screen, and macOS activates the application that was in front before.
That is how macOS behaves today. If a release changes it, the fallback is a
deactivate call through `objc2`.

**Windows and Linux are untried.** The shell compiles for them by design, and
`keyring` names the native store per platform. Nobody has built or run either.
The Windows desktop is a stated requirement in `docs/PLAN.md` section 1.

## Older, still open

- **The browser holds no query engine.** The rows moved out of `localStorage`
  into IndexedDB on 2026-09-02, which closed the storage shortcut: see D-022.
  What the browser still cannot do is answer a query with an index. Every
  filter is a walk over the rows in memory, which is fine for thousands of
  tasks and is the next thing to measure at a hundred thousand.
- **A comment is drawn from the local copy and never paged.** Every comment of
  every task the account can see arrives in the pull. A task with a hundred
  comments therefore costs a hundred rows on every client, and nothing pages
  them.
- **The restore drill runs on a laptop, not against the real replica.**
  `scripts/restore-drill.sh` rehearses the whole path against its own MinIO in
  Docker, and it passes. Nobody has restored from the bucket the deployment
  writes to, which is the run that also proves the credentials and the
  retention.
- **Nothing runs the drill on a schedule.** It is a script that a person has to
  remember. A backup rots quietly, so it belongs in a monthly job.
- **CI does not run the drill.** It needs a Docker daemon in the job and about
  two minutes. CI does run the screenshots, which need the same daemon, so this
  is a decision nobody has taken rather than a thing that cannot be done.
