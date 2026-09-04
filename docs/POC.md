# Proof of concept: what it proves, and what it does not

*2026-08-25. Built against [PLAN.md](PLAN.md) milestones M1 to M3, plus the
Todoist importer, a command line client and the deployment files.*

Tests: 216 Go cases and 201 web cases pass. `go vet` and `gofmt` are clean, and
`go test -race ./...` is clean as well.

The plan makes four risky promises. This build tests each one with running code,
not with a design note.

## 1. One binary, one file, small footprint

The binary holds the API, the sync engine, the filter compiler, the recurrence
engine, the web app and the MCP server. The web app is embedded, so a deployment
is one file plus one SQLite database.

| Measure | Result |
|---|---|
| Binary, macOS arm64, unstripped | 18 MB |
| Dependencies | 6 direct: SQLite driver, MCP SDK, RRULE, UUID, WebAuthn, standard library |
| Database with 10 011 tasks | 3.6 MB, plus a write-ahead log |

The SQLite driver is pure Go, so `CGO_ENABLED=0` produces a static binary and
cross-compiling needs no toolchain.

## 2. Sync that never loses an edit

The client holds the state and an outbox. A command carries a client uuid, so a
retry is safe. The server applies a batch in one transaction, bumps one version
counter per change, and returns every row above the version the client knows.

Tested:

- A replayed command does not apply twice, and the version does not move.
- Two clients that write 20 edits each in parallel land 41 applied commands, and
  the last write wins per field.
- One bad command in a batch fails alone. The rest still apply.
- With the server stopped, the browser accepts a new task, shows it at once, and
  queues the command. The queue drains without help when the server returns.

- The property test the plan asks for now runs: three simulated clients walk 300
  random steps over five seeds, each one pulls at random moments, and a quarter
  of the batches are sent twice. Every client, and a fresh client that pulls from
  zero, ends at the same state as the server.

*Updated 2026-08-27. §6.1 makes seven promises about sync, and there is now one
property for each of them. A seeded generator builds the command stream and
gives every command the rows it depends on, so the same set of commands runs in
two different legal orders.*

| Property | What it proves |
|---|---|
| Commands commute | Two orders of one conflict-free stream reach the same state, over every table and every field. A third pass changes nothing |
| Idempotence across a restart | The store closes, opens and refuses the whole stream again. `applied_command` is a table, not a cache |
| Monotone version | The counter never goes backwards, never repeats, and moves if and only if a command was accepted |
| Last write wins per field | Every pair of 13 task fields, in both orders. Both edits survive, and a field nobody named does not move |
| Temporary ids | One batch builds a project, a sub-project, a label, three levels of sub-task and a task that names its project by name, and every reference resolves |
| Every device catches up | Three devices, one of them offline for a long stretch, all reach the server state, and so does a device that starts from nothing |
| No lost edit, on the server | The version of a row equals the highest change log entry that names it, every accepted command holds a distinct version, no change log entry names a row that does not exist, and one search row exists per task |

The clock is injected, so nothing reads the wall clock. Five seeds are fixed and
one comes from the clock, so CI repeats a laptop failure and a long run still
finds what a fixed corpus cannot. Every failure prints its seed, and
`TEHA_SEED=<seed> go test` repeats one case. Go has no shrinker, so a failure
prints the first line of the state that differs instead of two states of
hundreds of rows. `-short` cuts the seeds and the stream, so the whole suite
runs in about 3 seconds, and a full run of the properties at `-count=20` takes
38 seconds.

The fractional index that §6.1 promises did not exist: every write path put the
literal `m` into `order_key`. It is now the `order` package with its own
property test, and D-013 records the choice. No client calls it yet: the web
app renumbers a short band instead, per D-024, so [BACKLOG.md](BACKLOG.md)
carries the rest.

## 3. A filter language that means one thing everywhere

One grammar compiles to a SQL `WHERE` clause on the server, and the same grammar
runs over the local rows in the browser. The phone compiles it as well, through
the gomobile binding, with the names of its own Room database. A saved view, a
hand-typed query and an MCP call use the same string.

```
today                       overdue | today & #Home
#Trip & !%errand            p1 & no date
search: ferry               before: friday & deadline
```

Twenty-one grammar cases and five rejection cases are under test. The phone
dialect adds 49 cases, each one run against a real SQLite database that carries
the Room column names. Todoist moved filters from `@label` to `%label`, so both
work here and an imported filter keeps working.

Since 2026-08-31 the claim in the heading is a test rather than a hope.
`parser-fixtures/filter.json` holds one account of thirteen tasks, six
projects, two sections, two people and eighty-one queries with one answer
each. The Go test writes those answers by running the compiled SQL against real
SQLite, and the web test demands the same answers from the browser evaluator.
A term that means two things in two clients fails the build. Three terms fail
on purpose, each with a sentence and never with the wrong rows: a section term
on the phone, `created:` on the phone and in the browser, and an assignee term
wherever the store keeps no assignee.

Speed, on 10 011 tasks, for a page of 200 rows:

| Query | Time |
|---|---|
| `today` | 1.7 ms |
| `overdue` | 1.6 ms |
| `no date` | 3.2 ms |
| `%store` | 2.9 ms |
| `search: ferry` | 5.4 ms |

## 4. An MCP server an agent can drive all day

The transport is Streamable HTTP from specification revision 2026-07-28, in
stateless mode: no session header, no `initialize` handshake, and `server/discover`
answers with the supported versions and the server identity.

Ten tools, every write batched:

`list_tasks` `add_tasks` `update_tasks` `complete_tasks` `list_projects`
`add_project` `comments` `add_comment` `search` `plan_day`

| Call | Cost |
|---|---|
| `plan_day`, a full daily plan | 459 bytes, about 114 tokens |
| `list_tasks`, default page of 50 | about 1 200 tokens |
| `list_tasks`, 200 rows, the worst case | about 4 800 tokens, 96 bytes per row |
| `add_tasks` with three tasks | one call, one transaction |

A bad filter returns a tool error that repeats the grammar, so the model repairs
itself instead of failing the session.

Row ids are 17 characters, not a 36-character UUID: 8 characters of millisecond
time, 3 of a counter inside that millisecond, 4 random. That choice alone saves
about 1 000 tokens on a 200-row page, and the counter is what keeps an import of
thousands of rows unique.

## 5. An exit from Todoist that loses nothing

`teha import --token <token>` reads the account with one full sync, paces itself
inside the documented limits (1 000 partial-sync and 100 full-sync requests per
15 minutes), and writes in batches of 100 commands. Every row keeps its Todoist
id in `source_ref`, so a link in a note still resolves.

The import was rehearsed against a fake Todoist endpoint with a realistic
Estonian account: 4 projects (one archived), 10 tasks, a sub-task, two repeating
tasks, a completed task, a timed task with a zone, labels and comments.

| Check | Result |
|---|---|
| Rows written | 2 projects, 2 labels, 10 tasks, in 15 commands |
| Priority mapping | Todoist API 4 maps to our 1, and back |
| Recurrence | `every month` and `every! 3 days` became RRULE, and `every! ` set the from-completion flag |
| A rule that does not convert | The task still arrives, and the original words go into the description |
| A section, and a comment | Folded into the description, and counted in the summary |
| A second run | 0 new rows, 0 commands, the version does not move |
| The archive | Todoist does not send it in a full sync, and the summary says so |

33 recurrence forms and 13 clean failures are under test, together with the
backoff on 429 and 5xx, and the redaction of the token in every error.

*Updated 2026-08-27. A second fixture tree, `internal/todoist/testdata/zeroloss/`,
holds only the hard cases and nothing else. Every row in it is invented: no real
id, no real name, no personal content and no token. A guard test reads the two
files and refuses an address, a user id or a token, so the tree cannot rot into
a recording of a real account.*

The zero loss test walks the payload instead of a list of expected numbers, so a
row added to the fixture is a row the test demands.

| Hard case | What the test demands |
|---|---|
| A project two levels deep | Both parent links survive |
| Two sections | The name is the first line of the description, and the summary counts it |
| Three labels on one task | All three arrive, in one command |
| A sub-task three levels deep | Every level points at the right parent |
| A completed task | It closes and keeps its completion time |
| An `every!` rule | The rule converts and the from-completion flag is set |
| A task that is complete and repeating | It keeps both, because a completion would otherwise move it to its next date |
| A deadline with a duration | Both carry, with the clock time |
| Two comments on one task | Both fold into the description, oldest first, and a deleted one does not |
| Two saved filters | Both names and both queries reach the summary, and a deleted one does not |
| An empty title | The row still arrives, as "(no title)", and keeps its description |
| An emoji and a right to left script | The title survives byte for byte |
| A due date with a time zone | The date, the clock time and the zone name all carry |
| A deleted, missing or archived project | The task lands in the inbox and is never dropped |
| A sub-task whose parent never arrived | It becomes a top level task |
| Two pages | The cursor loop reads both, in two requests, with one full sync |

The pacing is under test as well: one full sync against a documented limit of
100 per 15 minutes, two requests against a limit of 1 000, at most 100 commands
per batch, and every batch body far under 1 MiB. The default pace of the client
stays at one request per second.

The resume is under test at every point. The command stream is cut after each of
its 26 commands, and the run that resumes has to reach the state of a clean run
every time. That is what found bug 9.

## 6. Capture from the macOS keyboard, today

The same binary is a client. It covered capture before the Tauri shell
existed, and it still covers a machine that has no shell installed. The
desktop app in `desktop/` is the shorter road now: one keystroke, a panel, and
`teha://add?text=` for a launcher.

```
$ teha add "Book the ferry next tuesday at 9:30 p1 #Trip @call"
added: Book the ferry — due Tue 1 Sep 09:30, p1, #Trip to Setomaa, @call

$ teha add "Order gravel #Garden"
added: Order gravel
no project matches #Garden, so the task is in the inbox

$ teha done the
"the" matches 8 open tasks. Give the id or more of the title:
...
```

`#Trip` resolves to `Trip to Setomaa` by a unique prefix. An unknown name goes
to the inbox and says so, because a typo must never make a junk project. An
ambiguous name changes nothing. `contrib/macos/` holds a dialog script, a Raycast
command and the Apple Shortcuts recipe for a global hotkey.

The Go parser and the web parser both run the corpus in
`parser-fixtures/quickadd.json`, so the two clients cannot drift.

## 7. A deployment that fits in one file

| Artifact | State |
|---|---|
| Image | 26.3 MB, distroless, non-root, read-only root filesystem |
| Health | `/v1/health`, probed by a small static client inside the image |
| Compose | The server plus a Litestream 0.5.16 sidecar to S3-compatible storage |
| Cluster | A kustomize example with one replica, a volume and both probes. No hostname anywhere |
| CI | GitHub Actions: build, vet, gofmt, Go tests, the parser tests, then the image |

## 8. A login the browser can do with a fingerprint

*Added 2026-08-27.*

WebAuthn with `github.com/go-webauthn/webauthn` v0.18.0, as an addition to the
device token and not a replacement. The token is one string that a phone, a
shell and an agent send in one header, and WebAuthn cannot serve any of the
three.

| Rule | What the server does |
|---|---|
| Enrolment | Behind the device token only. The token is the invite, so signup stays invite-only. A passkey session cannot enrol a second passkey. |
| User verification | Required, on the registration and on every assertion. A stolen unlocked phone must not be an account. |
| Discoverable credential | The login asks for no user name, and the server sends no credential list to a caller that has not signed in. |
| Origin and relying party | From `TEHA_RP_ID` and `TEHA_ORIGIN`, else from the request host. Both are pinned when a ceremony begins and read back at the finish step. |
| Signature counter | A counter that does not increase is a 401, not a warning. |
| Session | `teha_session`, separate from `teha_token`. Secure, HTTP-only, SameSite=Lax, fourteen days, with a logout route. |
| Lockout | Per client address and per account. The wait doubles above the allowance, up to fifteen minutes. |

Twenty-six Go cases cover it, and ten of them are refusals: a wrong origin, a
host rewritten between the two steps, a replayed counter, a lower counter, an
unknown credential id, a signature from another key, a wrong challenge, an
unknown user handle, an assertion with no user verification, and a registration
without the token. A software authenticator in the test signs real bytes, so
every check runs against a real ceremony rather than a mock.

## 9. A reminder that arrives once, or not at all

*Added 2026-08-27, milestone M6 in part.*

`reminder` and `push_subscription` join the schema. A reminder is account data:
it carries a version, it travels in the change log and every client sees it. A
subscription is not, and it stays out of both, because an endpoint plus two keys
is per-device plumbing that can push to that device.

The scheduler wakes every 30 seconds, claims the due rows in one transaction
and sends one Web Push message per device. Two rules make it safe, and both are
under test:

| Rule | How | Test |
|---|---|---|
| A reminder fires at most once | The claim marks `sent_at` in the same transaction that reads the row, and commits before any push leaves | Claim, close the database, open the file again, claim again. Nothing comes back. The same through the whole sender against an `httptest` push service: one request |
| A missed reminder fires late only inside a window | One hour for a point reminder, four hours for a digest | Five kinds and delays, each one fires or drops, and the second pass always finds nothing |

Failure handling matches what the push services answer: a 404 or a 410 deletes
the subscription, a 429 parks it until `Retry-After` with the deadline on disk,
and a service that accepts a connection and then hangs costs one deadline and
never the scheduler.

The race detector earned its place here. `webpush-go` v1.4.0 appends to the
message slice a caller hands it, so two devices in one pass wrote into one
array. Each send now copies. Nothing about that is visible without `-race`.

The full detail is in [DECISIONS.md](DECISIONS.md) D-010 and D-011.

## 10. Two people in one file

One SQLite file holds the household. A project belongs to one account and is
shared with none or more others, and every other row hangs off a project, so
visibility is one question asked in one place.

| The promise | How it is kept | The test |
|---|---|---|
| An invitation is not the device token | The owner writes a code with a name on it. The server keeps its hash, shows the code once, and it works for one person and seven days | A used code, a made-up code and an expired code are each refused with the same sentence |
| Two accounts cannot see each other | Every read names the account, and every write passes a gate that asks which projects the command touches | Each person adds a task; neither pull carries the other's. Seven ways to write into another account's list are each refused |
| A shared list reaches both | One membership row, and the same visibility rule | The partner sees the milk, ticks it off, and the owner sees the tick |
| A list stays its owner's | Rename, delete and share are the owner's alone | A member's rename is refused |
| Losing a share is not silent | The server records the version of every membership change and answers `reset` to a pull from before it | The pull after an unshare asks the client to start again, and the row is gone |
| A nudge is personal | A reminder and a push subscription carry an account | A reminder on a shared task reaches its owner's devices and nobody else's |
| An agent sees one account | The MCP server builds one tool set per account, from the token that drove the call | Two tokens, two answers, and no token is a 401 |

Each account has its own inbox, because an inbox is the most private list there
is. A capture with no project lands in the inbox of the person who typed it,
and `#inbox` means that one. The full reasoning is in
[DECISIONS.md](DECISIONS.md) D-016 and D-017.

## 11. A backup that somebody has restored

`scripts/restore-drill.sh` starts MinIO, a server and Litestream in Docker,
writes through the API, destroys the database file, restores it from the
replica and compares. It takes about a minute and it removes everything it
made.

The first run failed, and that is the point of the section. Litestream had
replicated the database as it stood when it attached and nothing after it, so
the restore brought back the seed and lost every write. The cause is neither
Litestream nor the drill: the server holds one long-lived connection, so SQLite
leaves a write in the write-ahead log, and it moves the log into the file only
when the log passes about four megabytes. For two people that is days. The
backup looked healthy the whole time.

The server now checkpoints every ten seconds and once more on a clean stop, so
a restore can lose at most one interval. The drill passes: the account version
and every row came back, and the restored file took a new write. It fails
loudly without the checkpoint, which is what makes it worth keeping. See
[DECISIONS.md](DECISIONS.md) D-019.

The drill also found that `docs/DEPLOY.md` named a `litestream snapshots`
command that the 0.5 line does not have. The guide now says `ltx`.

## 12. A conversation on a task, and a notification when it matters

A comment is a row in `comment`: the task, the author, the body, and the three
stamps with the version counter. It travels in the pull with every other row.

| The promise | How it is kept | The test |
|---|---|---|
| Everybody who sees the task sees the talk | A comment hangs off a task, which hangs off a project, so one visibility rule answers | A comment on an unshared list is unreadable to the other account, and readable one line later when the list is shared |
| A line belongs to whoever said it | `account_id` is the author, and the gate refuses an edit or a delete by anybody else | Both refusals arrive as one sentence, and the author's own edit lands |
| A query reaches comment text | `comment:` reads the table, in the Go compiler and in the browser evaluator | Five cases in `parser-fixtures/filter.json`, answered by real SQLite and demanded of the browser |
| A store with no comments says so | `filter.Schema` leaves the table name empty, and the term fails | A schema stripped of the table refuses `comment:` with a sentence |
| An import keeps a conversation | A Todoist comment becomes a row and keeps its posting time | The zero-loss fixture demands both comments of one task, in order, and no deleted one |
| Being given a task is worth hearing | `ApplyWithEvents` reports the fact, `internal/push` writes the words | An assignment and a comment reach the other person's devices, and a repeat or a self-assignment sends nothing |
| Nobody hears about a task they cannot see | The gate refuses an assignment to a person who cannot see the list | Three shapes of that command are refused, and the same command works once the list is shared |

The store reports a fact and never a wording. That is what lets a second
transport, such as UnifiedPush, send the same events with no copy of the store.
See [DECISIONS.md](DECISIONS.md) D-020.

## 13. A list that works in a shop

Shopping mode is a layout of a project view, and it needed no table. An aisle
is a section of the project. The suggestions are the completed tasks of it. The
aisle of a new item is copied from the newest item of the same name, so `milk`
lands in Dairy from the second time on.

| The promise from §4 of the plan | What it is |
|---|---|
| Items grouped by category, learned from history, editable | Sections as aisles, one lookup by name for the guess, and the **Section** field to move one |
| Big check targets | A 30-pixel circle in a 50-pixel row, tested at 320 pixels wide |
| Recently bought suggestions | The completed items of the list, newest first, one per name, that are not on the list now |
| A quantity in the title | `2x milk` draws the count as a chip and keeps the title as typed |
| Live sync while two people are in the store | The event stream wakes a pull, and the pull redraws. Nothing was added for this |
| Checked items collapse and clear on request | The basket holds this trip, twelve hours, and a button empties it |
| It must work in split screen | `scripts/screenshots.mjs` fails the build if the layout scrolls sideways or a target shrinks at 320 pixels |

The one thing the plan asks for and this does not do is a drag from one aisle
to another. See [DECISIONS.md](DECISIONS.md) D-021.

## 14. A local copy that can hold a decade

The web app kept its whole state in one `localStorage` string: five megabytes
of quota, rewritten on every keystroke. That was the last known shortcut in the
web app.

It now keeps one IndexedDB object store per table, keyed by the row id, plus one
small record for the sync watermark, the outbox and how the browser is
arranged. A write marks one row and a timer writes the batch, so a change costs
one row and not the whole account.

| The promise | How it is kept | The test |
|---|---|---|
| One tick of the app is one write | Every local edit passes one function, which reads the table from the command type | Three rows and the device record land in one transaction |
| An unsent command survives everything | The outbox is written with the same call, and it is never dropped | The move off `localStorage` keeps it, and so does a refused write |
| A device that used the old string keeps its account | The string moves once, at the next start, and the old key is removed | Two copies on one device is what the move exists to prevent, and the test demands the removal |
| A browser with no IndexedDB still works | The fallback keeps the old single string behind the same interface | Both backends round-trip a row and a delete |
| The real binding works | Node has no IndexedDB, so a real browser proves it | The screenshot job reads the object stores and fails if the rows or the watermark are missing |

This is a change of plan, which named wa-sqlite in OPFS. The browser has no use
for a query engine: the renderer walks the rows and the filter evaluator does
too. See [DECISIONS.md](DECISIONS.md) D-022.

## 15. A record of who did what

Nothing recorded who changed a task, and §6.6 asked for the same table as the
audit trail of a login. One `activity` table now holds both.

Every command writes one line, in one place: right after the command succeeds
and its savepoint is released, so a refused command leaves nothing. Every login,
failed login, logout, passkey, invitation, join and share writes one as well.

| The promise | How it is kept | The test |
|---|---|---|
| A line is as private as the row it describes | It carries the project, and an account reads a line of a project it can see | A second person reads the shared list and not the one beside it |
| A personal line is nobody else's business | A login and a reminder carry no project, so only their own account reads them | The owner of the household cannot read the other person's login |
| A refused command leaves no line | The line is written after the savepoint is released | A `task_update` against a missing task leaves the log empty |
| The log can name what went away | The title of the row is copied into the line | A deleted task is still named in the log, and one button brings it back |
| The reader is not shown a command type | The store writes the fact and the client writes the sentence | Every command type and every security action has words, and a raw type in the view fails the screenshot job |
| A big log does not become a big client | It is outside sync, and read one page at a time | The route answers a page and a cursor, and says whether more exist |

A deleted comment is logged and its words are not: a log nobody can delete is
the wrong place for a line somebody deleted on purpose. See
[DECISIONS.md](DECISIONS.md) D-023.

## What this build does not have

*Updated 2026-09-03, with the activity log and the phone reaching what the
browser reaches.*

- No widget, and no signature on the macOS app. The desktop shell hosts the web
  app, a global shortcut opens a quick add panel over every application, a menu
  bar icon carries the menu, and `teha://add?text=` adds a task from Shortcuts,
  Raycast or Keyboard Maestro. The build is unsigned, per the Phase 4 decision
  about identity, and the panel shows no parse hint before `Enter`, because the
  parser is in the page.
- The Android app reaches what the browser reaches, with two exceptions. It has
  a quick settings tile, a share target, the six built-in views, one view per
  project, a field that takes any query the grammar knows, comments, shopping
  mode, and a background sync every fifteen minutes. It joins a household,
  assigns a task, files one under a heading and acts on `reset`. **What it does
  not have:** a notification that arrives at once, because it has no push
  transport and a nudge is found by comparing a pull with what it held; and a
  way to write an invitation or share a list, because both are the owner's jobs
  and both are in the browser.
- No attachment. A comment is a row with an author, and only the author changes
  it. A file has nowhere to go: no part of this build stores one.
- Shopping mode runs in the browser and on the phone. An aisle is a section of
  the project and the suggestions come from what the list has held, so it
  needed no table on either.
- Push works in the browser and in the installed web app, and nowhere else. A
  reminder and a push subscription belong to one person, so two people who
  share a chore each keep their own nudge. A task given to somebody, and a
  comment on a task they can see, both notify them. Neither is retried: an
  event is sent and forgotten. The phone notifies without a push, by comparing
  a pull with what it held, so it hears within about a quarter of an hour and
  not within a second.
- No second factor beyond the passkey itself. The browser signs in with a
  passkey, with an invitation code, or with the device token, and each account
  has a token of its own.
- The web app draws a calendar of the current view, as a month or as a week,
  with drag to reschedule. It has no hour grid, so a task with a time sorts to
  the top of its day rather than sitting at its hour.
- The phone answers every term of the grammar except `created:`. It holds the
  comments, the sections and the people, so `comment:`, `/section`, `no
  section` and every `assigned` term compile there in the Room column names.
- The browser holds no creation date, so `created:` fails there in the same way
  and for the same reason. Every other term of the grammar answers the same in
  the browser, on the phone and on the server, and
  `parser-fixtures/filter.json` proves it.
- The web app holds no query engine. Its rows are in IndexedDB and every filter
  is a walk over them in memory, which is fine for thousands of tasks. The
  `localStorage` shortcut is gone.
- The full-text index holds no comment text, so `search:` and `comment:` mean
  two different things.

## Bugs found and fixed while building

1. **A pool deadlock.** The store holds one connection, and a query that read
   labels while a result set was still open blocked for ever. Both read paths
   now close the result set before the next query.
2. **Recurrence rounded the wrong way.** A weekly chore completed on its own due
   day moved to the same day. The next date is now the first occurrence strictly
   after both the due date and the day of completion.
3. **A stale script after an upgrade.** An embedded file has a zero modification
   time, so the browser kept an old `app.js`. Assets now carry a content hash as
   an ETag.
4. **A token budget that grew with the id.** UUID row ids cost about 2 000 tokens
   per 200-row page. Short sortable ids replaced them.
5. **The first short id collided.** Five random characters inside one
   millisecond repeated after four thousand draws, which an import reaches in a
   second. The id now carries a counter inside the millisecond, and 200 000
   draws produce no repeat.
6. **Two clients disagreed about an unknown project.** The command line client
   created it, and the web app used the inbox. Both now use the inbox and say
   so. `TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere` locks all
   three write paths: the importer and the command line client fall back to the
   inbox, the store refuses to guess so an agent gets an error it can repair,
   and none of the three invents a project.
7. **A command that failed halfway kept what it had written.** Every command of
   a batch ran in one transaction with no savepoint. A `task_update` against a
   task that does not exist first created the labels it named, then hit the
   foreign key and reported a failure, and the account was left holding a label
   that nobody asked for. The version had moved for a command that nobody
   accepted, so every client woke up for nothing. Each command now runs in its
   own savepoint.
8. **A deleted label never reached a client that had already synced.** A pull
   hides a deleted label from the label list of a task, so the list of a task
   changed while the task row did not. A client pulls with `version > since`, so
   it never saw the task again and kept showing a label that the account had
   deleted. The row was right on the server and wrong on every synced client. A
   label delete now moves the version of every task that carries it. The
   property test found it on seed 7.
9. **An interrupted import lost the completed state of a task.** A completed
   task costs two commands, and a completed repeating task three. A resume finds
   the task by its `source_ref` and skips it, so the `task_complete` that
   followed the `task_add` was never rebuilt. A run that died between the two
   left the task open for ever and a completed repeating task lost its rule as
   well. The resume now re-issues only what the store still needs, with the same
   command uuids, so a plain second run still writes nothing. The test cuts the
   command stream at all 26 points and demands the state of a clean run at every
   one. It failed at cut 17.
10. **A saved filter vanished without a word.** The sync model had no `filters`
    field, so the importer never read one and never named one. Our schema still
    has no filter table, so it writes none, but it now prints every name and
    every query the way it already does for a project comment. A gap a person
    can see is not a loss.

11. **The backup replicated a stale file.** Litestream copies the database
    file, and one long-lived connection leaves every write in the write-ahead
    log for days. A restore brought back the seed and nothing else, while the
    replica reported a healthy transaction id. The server now checkpoints on a
    timer. Found by the first run of `scripts/restore-drill.sh`.
12. **A reschedule left the reminder behind.** Only the detail sheet moved a
    reminder when the due date moved. The `t`, `m` and `w` keys, the bulk
    reschedule, the overdue sweep and the new calendar drag all wrote a new due
    date and left the reminder pointing at the old moment, so it fired at a
    time that meant nothing or had already passed. Every path that writes a due
    date now goes through one function, and that function re-arms.

13. **The MCP search reached across the household.** Every other read was
    scoped to the account that asked, and `search` was not: it read the
    full-text index directly and then fetched each task by id. An agent of one
    person could therefore find the titles of the other person's tasks. The
    scope is now inside the query rather than a filter over its result, because
    a filter over the result still reports how many rows it threw away. Found
    by reading every store call in the diff, not by a test, and it has one now.

14. **Taking an assignee away was refused.** The browser cleared the field with
    `clear: ["assignee_id"]`, and the list of fields a command may clear did
    not hold that name, so the server answered "field cannot be cleared" and
    the row kept the person on it. The phone sends an empty string for the same
    intent, which would have written a second way of saying nobody into the
    column. The list now holds the name, an empty value writes NULL, and one
    test drives both clients' shapes. Found by comparing the phone's write
    path with the server's, while bringing the phone into the household.

15. **A task could be given to somebody who cannot see the list.** The gate
    asked which projects a command touches and whether the writer may touch
    them. It never asked whether the person named as the assignee may see them.
    So the owner of a private list could put the other account's id on a task
    in it, and that person then received a notification carrying the title of a
    task they could not open. The gate now refuses it with one sentence, and
    the same command works as soon as the list is shared. Found by reading the
    new notification path against the visibility rules, not by a test, and it
    has one now.

## Next, in order

*Updated 2026-09-03. The earlier list is done except for the two items that
need a person: the activity log is built, and the phone now reaches what the
browser reaches.*

1. **Use it.** Every milestone from M2 to M6 says "built" and none of them says
   "proven". Four exit tests need a person and not code: one week of the web
   app, one week of the macOS shell, the author uninstalling Todoist from the
   phone, and one month of the partner's shopping. Nothing on this list matters
   more than starting them.
2. **Attachments.** The one Phase 2 item that no part of this build can do,
   because nothing here stores a file. It needs blob storage, a size cap, an
   upload and a download path, per-household scoping, a CSP rule for an image,
   a backup answer for the files, and offline behaviour in three clients. It is
   the first thing that is not a row.
3. **The hour grid on the week calendar.** The last layout the plan names that
   this build draws differently: a week is seven day columns, so a task with a
   time sorts to the top of its day. Time blocking in Phase 3 needs the grid.
4. **Order keys on the phone.** The browser can order a day by hand since
   2026-09-04: a drag or `Shift+J` moves a task inside its band of one day at
   one priority, and D-024 records the shape. The phone draws that order and
   cannot set one, and neither can the MCP tools. The `order` package still has
   no caller, because a renumber of a short run needs no key between two
   neighbours.
5. **The drill against the real bucket, on a schedule.** The restore is
   rehearsed on a laptop and it passes. Nobody has restored from the bucket the
   deployment writes to, which is the run that also proves the credentials, and
   nothing runs it monthly.
