# Proof of concept: what it proves, and what it does not

*2026-08-25. Built against [PLAN.md](PLAN.md) milestones M1 to M3, plus the
Todoist importer, a command line client and the deployment files.*

Tests: 73 Go cases and 29 parser cases pass. `go vet` and `gofmt` are clean.

The plan makes four risky promises. This build tests each one with running code,
not with a design note.

## 1. One binary, one file, small footprint

The binary holds the API, the sync engine, the filter compiler, the recurrence
engine, the web app and the MCP server. The web app is embedded, so a deployment
is one file plus one SQLite database.

| Measure | Result |
|---|---|
| Binary, macOS arm64, unstripped | 18 MB |
| Dependencies | 5 direct: SQLite driver, MCP SDK, RRULE, UUID, standard library |
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

## 3. A filter language that means one thing everywhere

One grammar compiles to a SQL `WHERE` clause on the server, and the same grammar
runs over the local rows in the browser. A saved view, a hand-typed query and an
MCP call use the same string.

```
today                       overdue | today & #Home
#Trip & !%errand            p1 & no date
search: ferry               before: friday & deadline
```

Twenty-one grammar cases and five rejection cases are under test. Todoist moved
filters from `@label` to `%label`, so both work here and an imported filter
keeps working.

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

Eight tools, every write batched:

`list_tasks` `add_tasks` `update_tasks` `complete_tasks` `list_projects`
`add_project` `search` `plan_day`

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

## 6. Capture from the macOS keyboard, today

The same binary is a client. That covers capture before the Tauri app exists.

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

## What this build does not have

- No Android app, no macOS app, no widget, no quick settings tile.
- No sharing, no second account, no push, no comments, no attachments.
- No passkeys. One device token guards everything, and the browser keeps it in
  a cookie.
- No sections, no board layout, no calendar layout.
- Litestream is wired in the compose file, but no restore has been rehearsed
  against real object storage.
- The web app stores its state in `localStorage`, not in OPFS with SQLite. That
  is fine for thousands of tasks and needs replacing before it holds a decade of
  history.

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
   so.

## Next, in order

1. Run the real import, then daily drive the result for a week (M1 exit test).
2. Deploy behind the public entry point, with Litestream running and a restore
   rehearsed once.
3. The Android app with the quick settings tile (M4). This is the largest
   remaining risk, because it is a third parser and a second local store. The
   partner uses Android, so this is also what unlocks the household milestone.
4. Passkeys, then invite the second account.
