# Project plan: a self-hosted task manager that beats Todoist for two people and one AI

*Status: draft v3, 2026-08-25. Name chosen: **teha**. The server, the sync engine, the web app and the MCP server now run: see [POC.md](POC.md). Every fact in the research notes carries a source and a read date. Evidence: [docs/research/](research/). Working name: undecided, **toimeta** leads (see §11). Sibling projects: lugu, kuula, vali.*

## 1. Vision

Todoist is the fastest way to get a thought into a list. It is also a closed service with a fragile API. Programmatic access times out, sync incidents lose edits, and the web app is slow. The open-source alternatives are either online-first web apps or Android clients bolted onto CalDAV. None has a quick add that feels like Todoist, and none has an MCP server that an agent can use for a whole session without a timeout.

**Goal: the task manager the author and a partner daily drive without a single workaround, and the one an AI agent can operate as fast as a human.** Self-hosted, small, local-first, open source. Later, a hosted option for other people.

Three users from day one:

1. **The author.** Captures from the macOS keyboard, the Android notification shade and Claude. Plans the day and the week. Keeps work tasks and life tasks in one place, with source links back into Obsidian.
2. **The partner.** Shared household projects, a shopping list that works in the store, trip plans. Must never see a spinner, an error code or a developer word. English only, decided 2026-08-25.
3. **The agent.** Any agent through MCP, not Claude alone. A locally hosted model counts, and so does another harness. Reads a filtered list in one call. Adds or updates twenty tasks in one call. Never times out.

Both people use Android phones and macOS laptops. The author also uses a Windows desktop, which the Tauri shell covers at no extra cost. Confirmed 2026-08-25.

Scope from day one: tasks, projects, sharing between two people, natural language capture, MCP. Not in scope until Phase 4: teams, billing, iOS native.

## 2. What we must beat

| Gripe | Evidence | Our answer |
|---|---|---|
| API and MCP time out | `td` and the official MCP fan out into many HTTP calls. Todoist allows 1 000 partial-sync and 100 full-sync requests per 15 minutes per user, 100 commands per request, and a 1 MiB body ([API v1 request limits](https://developer.todoist.com/api/v1/)). | One binary serves the app and MCP. Filters run in SQL. Batch mutations. Compact results. Our own limits are per token and generous. |
| Slow, mouse-first web app | HN threads 2020 to 2024. Godspeed and Linear set the bar: under 50 ms, every action on a key. | Local SQLite in every client. Optimistic UI. `Cmd+K` and single-key shortcuts from Phase 1. |
| Sync loses edits | September 2025 incident. Help center warns of data loss on a dismissed sync error. | Outbox on every client. Idempotent commands. Server never drops a command silently. Every change is in an activity log with undo. |
| No start date, no dependencies, no snooze | Top requests on r/todoist. Godspeed, OmniFocus, Obsidian Tasks have them. | Phase 3. The schema has the columns from Phase 1. |
| Calendar layout, durations and time blocking behind Pro | The free plan (now called Beginner) also caps filters at 3 and history at one week ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). Automatic reminders did move to the free plan in 2026. | Everything is included. No tiers, no caps. |
| Shopping in the store is clumsy | Apple Reminders and Bring! group by aisle and share live. | Shopping mode for a project: aisles, big targets, recent items, live sync. Phase 2. |
| Notes and tasks live in two apps | Trip plans blur between Todoist and Obsidian. | Deep links both ways in Phase 1. A vault bridge in Phase 3. |
| Closed data | No export of history, no self-hosting. | Full export at any time. Import from Todoist including completed history. |

Detail: [research/01-todoist-feature-map.md](research/01-todoist-feature-map.md), [research/02-beyond-todoist.md](research/02-beyond-todoist.md), [research/03-landscape.md](research/03-landscape.md).

## 3. Product principles

1. **Capture is sacred.** From any surface, the path from intent to saved task is one gesture and one text field. Parsing runs locally. Offline is not an error state.
2. **Every action is instant and reversible.** The UI never waits for the server. Every mutation has an undo. Deletion is soft for 30 days.
3. **One language for filters, everywhere.** The web, the apps and MCP share one query language. A saved filter is a saved query. A query in an MCP call is the same string.
4. **The agent is a first-class client, and it is not one vendor.** MCP tools are designed for a model: few, batchable, compact. What a human does in ten taps, the model does in one call. Assume a small local model, not only a frontier one, so the token budget is a hard limit rather than a target.
5. **No telemetry.** No analytics, no crash reports without opt-in. The only host the app talks to is your server.
6. **Polish before features.** A feature ships when the partner does not notice that it is new software.
7. **Small footprint.** One process, one SQLite file, under 50 MB of memory idle, one container image under 40 MB.
8. **An agent's own work belongs in the list.** A running agent session is work in progress, which is what a task is. teha shows that work. It never runs it. See §13.

## 4. Feature map by phase

Phase 1 is solo daily driving with import from Todoist. Phase 2 makes it a household tool. Phase 3 adds what Todoist lacks. Phase 4 opens it to others.

### Phase 1 — Capture and trust (solo daily drive)

**Server**

- Accounts with passkeys and a password fallback. Invite-only signup.
- Data model: projects (nested), sections, tasks (nested), labels, filters, comments (text only), reminders (table, scheduler and Web Push, see DECISIONS.md D-010 and D-011), completed history, activity log.
- Sync endpoint: `POST /v1/sync` with `since` version and a command batch. Version counter per account. Client UUIDs and temp ids. Last-write-wins per field. Fractional index for order.
- Filter language, evaluated in SQL. Todoist's grammar, so imported filters keep working: `#project`, `##project` (with sub-projects), `/section`, `%label` with `@label` as an accepted alias (Todoist retires `@` in filters through 2026), `p1`..`p4`, `today`, `tomorrow`, `overdue`, `no date`, `no time`, `date before:`, `date after:`, `deadline:`, `created:`, `recurring`, `subtask`, `assigned to:`, `search:`, `*` wildcards, `\` escape, `&`, `|`, `!`, parentheses, and `,` for several lists in one saved filter.
- Three documented gaps in Todoist filters that we close from day one: query comment text, show completed tasks, and show a parent task together with its sub-tasks in one result ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)).
- Recurrence stored as RFC 5545 RRULE plus a `from_completion` flag. Next date computed on the server and on the client with the same rules and the same test fixtures.
- Full-text search over titles, descriptions, comments.
- Export: one JSON file with everything. Import: Todoist API v1, including sub-projects, sections, labels, comments, filters and completed history.
- Live updates: one SSE stream per session that says "version changed", the client then pulls.
- Backup: Litestream replication of the SQLite file to S3-compatible storage. Restore is documented and tested.

**Web app (also the desktop UI)**

- Views: Inbox, Today (overdue on top), Upcoming, project, label, filter, search, completed.
- Quick add with natural language for date, time, recurrence, priority, project, section, label. Autocomplete for `#`, `@`, `/`. Highlight of the parsed date, tap to undo the parse. Multi-line paste creates several tasks.
- Task detail: title, description (Markdown), sub-tasks, labels, priority, due, recurrence, comments.
- Drag to reorder and indent. Multi-select with bulk actions. Undo toast.
- Keyboard: `q` quick add, `Cmd+K` command menu with every command, `j`/`k` move, `e` edit, `1`..`4` priority, `t` today, `w` next week, `x` select, `Enter` complete. Keyboard reference on `?`.
- Offline: the client holds the account in SQLite (wa-sqlite in OPFS) or IndexedDB, renders from it, syncs later. Installable PWA. Works on the partner's phone in any browser while native apps mature.
- Light and dark themes. Accent colors per project.

**Android app**

- Offline-first: Room is the source of truth, outbox, WorkManager sync, same module shape as lugu.
- Quick settings tile: opens a translucent quick add activity with the keyboard already open. On a locked device, `unlockAndRun` first. `requestAddTileService` prompt on first run.
- Share target for text and links. App shortcut "Add task". Floating add button.
- Swipe right to complete, swipe left to schedule. Long press for multi-select and drag.
- Today, Upcoming, projects, filters, search, task detail with the same fields as the web.
- Local reminders for due times, fired from the local database, no push needed.
- Obtainium channel: one tagged GitHub release per CI build, stable signing key, Shizuku for silent installs.

**macOS**

- Tauri v2 desktop app that hosts the web UI. Global shortcut opens a small quick add window on top of everything. Tray icon. A URL scheme named after the app (`<name>://add?text=...`) for Shortcuts, Raycast and Keyboard Maestro.
- Unsigned build for personal use in Phase 1. Signing and notarization is a Phase 4 decision because the certificate carries a legal name.

**MCP server (in the same binary, `/mcp`)**

- Transport: Streamable HTTP, specification revision 2026-07-28, stateless. Auth: bearer token per device in Phase 1, OAuth 2.1 in Phase 3.
- Tools, designed for a model: `list_tasks(filter, fields, limit, cursor)`, `get_task(id)`, `add_tasks([...])`, `update_tasks([...])`, `complete_tasks([ids])`, `move_tasks`, `list_projects`, `list_labels`, `search(text)`, `today_summary()`. Every mutation tool takes a batch. Every list tool takes a `fields` list and returns compact JSON with short keys and no nulls.
- Resources: `<name>://project/{id}` as Markdown for a quick read.
- Budget: a typical `list_tasks` result under 2 000 tokens, a tool round trip under 200 ms on the LAN.

**Import and exit**

- Import from Todoist runs once from the web UI with a Todoist token. Maps recurrence strings through the parser. Keeps Todoist ids in a `source_ref` column so links in notes still resolve through a redirect.
- The importer paces itself to the documented limits: one full sync, then partial syncs, well under 1 000 requests per 15 minutes, and it resumes after an interruption instead of starting again.
- Export is a button. Also `GET /v1/export`.

### Phase 2 — Two people

- Household space: projects shared with one other account, assignee per task, "assigned to me" filter, comments with attachments (images, files, up to a size cap), reactions.
- Push notifications: UnifiedPush through ntfy for Android, Web Push for browsers and iOS PWA. Reminder at time, before due, daily digest, comment and assignment notifications.
  - **Web Push runs since 2026-08-27**, with the three reminder kinds: at the due time, before the due time and a daily digest. `internal/push` holds the scheduler and the sender. Comment and assignment notifications wait for the second account. UnifiedPush waits until one transport proves too little.
- Shopping mode per project: items grouped by category (learned from history, editable), big check targets, recently bought suggestions, quantity in the title (`2x milk`), live sync while two people are in the store, checked items collapse and clear on request.
  - **Loose categories, not a store map.** Aisle order differs per shop, so a real map is a modelling job with a maintenance burden per shop and little payoff. Learn a category order from what the household actually buys, and let a person drag it. Revisit only if the loose grouping proves useless in a real shop.
  - **It must work in split screen.** The shop's own scanner app is open beside it, so the layout has to hold at roughly half the screen width, with touch targets that survive one-handed use in a cold aisle. Test at that width, not only at full width.
  - **Live sync is a requirement here, not a nicety.** Two people shop in parallel and each must see the other tick an item within a second, or they buy it twice. This is what sets the latency target in §7.
- Board layout (sections as columns) and calendar layout (month and week, drag to reschedule).
- Deadline field with `{date}` syntax. Duration with `for 30min`.
- Activity log view per project and per task, with restore.
- Widgets on Android (Glance): Today list with a filter, add button.
- Location reminders on Android (geofence).
- Templates: save a project as a template, create a project from a template (packing list, trip prep).
- Project notes: a Markdown page per project (the trip page), tasks can be embedded in it.

### Phase 3 — Beyond Todoist

- Start date (`⏳` in Obsidian terms) separate from due. Deferred tasks are grey in Today until the start date.
- Snooze that keeps the recurrence, and a "won't do" completion state.
- Dependencies: `after: <task>`, a "not blocked" filter term.
- Someday and Anytime buckets. Weekly review mode with a per-project review interval. **The review is agent-enabled**, decided 2026-08-25: the agent proposes what to defer, drop, split or reschedule, and the person accepts or rejects each suggestion. A review that a person has to run alone is a review that stops after three weeks.
- Command palette macros: a saved sequence of commands with variables (`{clipboard}`, `{today}`), bound to a key or a URL.
- Filter language extensions: `sort:`, `group:`, `limit:`, `start:`, `deadline:`, `blocked`, `assigned to:`, `created:`.
- Calendar feed (iCal) per project and per filter. CalDAV read-only view if cheap.
- Per-project email address for email-to-task.
- Obsidian bridge: the server writes a read-only Markdown mirror of chosen projects into a folder inside the vault, in Obsidian Tasks syntax with `📅`, `⏳`, `🔁`, `🆔`, `⛔`. The bridge watches the folder and completes a task when its checkbox is ticked. Trip note in Obsidian shows the trip project live and quick add from the note lands in the project.
- OAuth 2.1 for MCP so claude.ai and Claude Desktop connectors work without a pasted token. Client ID Metadata Documents, with Dynamic Client Registration only as a fallback for an authorization server that lacks them.
- Webhooks on task and project events.
- Gentle statistics: completed per day and per project, no points, no streaks, off by default.
- Time blocking: drag a task with a duration onto the calendar layout. Optional read of external calendars over iCal URLs for context.

### Phase 4 — Open and hosted

- F-Droid and IzzyOnDroid listing with reproducible builds. Google Play optional.
- Signed and notarized macOS build, Homebrew cask. Decide how much identity to reveal.
- iOS app: KMP core, SwiftUI shell. Only if the PWA is not enough for the partner.
- Wear OS quick add and Today. Android App Functions for Gemini.
- Multi-tenant hosting: one SQLite file per account, a control plane for signup and billing, a status page, a docs site.
- Habit and routine tracking, if wanted by then.
- Google Calendar two-way sync, Slack and Gmail capture, Zapier and IFTTT.
- Karma-like goals as an opt-in module.

## 5. Tech stack

| Layer | Choice | Why |
|---|---|---|
| Server | Go, `modernc.org/sqlite` v1.57.0, `embed.FS` for the web build, MCP Go SDK v1.7.0, `rrule-go` v1.8.2, `go-webauthn` v0.17.4 | Single static binary, 15 to 40 MB idle, one 25 MB image. Go is already in use in a sibling project. The MCP SDK is official. Versions read 2026-08-25. |
| Database | SQLite in WAL mode, one file per account in Phase 4, Litestream v0.5.16 to S3-compatible storage | Small, fast, backed up with one process. Multi-tenant later without Postgres. The 0.5 line shipped 2025-09-30 and rebuilt replication on the LTX format. |
| Web | Svelte 5.56, SvelteKit 2.70, Vite, TypeScript, wa-sqlite in OPFS with an IndexedDB fallback, virtual lists | Small bundle, fast, compiles away. |
| Android | Kotlin, Jetpack Compose, Room 2.8.4, WorkManager, Ktor, Glance 1.1.1 | Same shape as lugu. Room is KMP-stable since 2.7.0, so an iOS core later is cheap. Glance 1.2 is still a release candidate, so plan on 1.1.1. |
| macOS | Tauri v2.11.5 around the web build, global shortcut, tray, deep link plugins | One UI codebase. 10 to 30 MB idle. A native Swift `NSPanel` quick add stays the fallback if the Tauri window feels wrong. |
| Parser | Two implementations, TypeScript and Kotlin, one shared fixture corpus (`parser-fixtures/*.json`) | Offline parse on every client. The corpus keeps the two equal. The server accepts structured fields and never parses free text. |
| Recurrence | RRULE on the wire and in the database, natural language only in clients | One engine per language, exhaustive fixtures. |
| Push | ntfy (UnifiedPush) in the cluster, Web Push with VAPID | No Google dependency. F-Droid friendly. |
| Auth | Passkeys plus password fallback, per-device tokens, OAuth 2.1 for MCP in Phase 3 | Phishing-resistant and partner-friendly. Tokens are listed and revocable. |
| CI | GitHub Actions: Go tests, web tests, Android build with Roborazzi screenshots, one tagged release per build | Same as lugu. |

Alternatives considered and rejected: Elixir and Phoenix (footprint and cold start), PowerSync and ElectricSQL (extra services and Postgres, and no Kotlin client for ElectricSQL), Replicache (the repository is archived, Zero replaces it), Zero (TypeScript only), CalDAV as the model (no sections, no comments, no filter language, and an IETF draft exists because VTODO scheduling is too weak), Flutter (one more runtime, weaker tile and widget story). Detail: [research/04-tech-stack.md](research/04-tech-stack.md).

## 6. Architecture

### 6.1 Sync

The local database is the source of truth for the UI on every client. The server is the sync target and the source of truth for conflict resolution.

- Every client keeps `version` (the last seen server version) and an **outbox** of commands. A command is `{uuid, type, args, client_ts}`. Ids for new rows are client-generated UUIDv7.
- `POST /v1/sync {since, commands[]}` applies the commands in order inside one transaction, then returns every row changed after `since` and the new version. A command with a known `uuid` is a no-op, so retries are safe.
- Conflicts: last write wins per field, by server receipt order. Ordering uses fractional indexes, so two clients that reorder concurrently both keep a valid order.
- The server keeps a change log `(version, table, id, op)` per account, so a pull is one indexed range read. A client that is too far behind gets a full snapshot.
- Live: `GET /v1/events` (SSE) sends `{version}` on every change. Clients pull on that event and on foreground.
- Tests: a property test runs random command interleavings across three simulated clients and checks that every client converges to the server state.
- Fan-out limit: one sync call per foreground event, not one call per row. A public entry point counts requests per client address, so a client that fans out many parallel requests trips the flood ceiling.

### 6.2 Data model (core tables)

`account`, `space` (household), `member`, `project`, `section`, `task`, `label`, `task_label`, `comment`, `attachment`, `reminder`, `filter`, `activity`, `device_token`, `change_log`.

`task` columns worth fixing now: `id`, `project_id`, `section_id`, `parent_id`, `order_key`, `title`, `description`, `priority`, `due_date`, `due_time`, `due_tz`, `rrule`, `rrule_from_completion`, `start_date`, `deadline`, `duration_min`, `assignee_id`, `completed_at`, `state` (open, done, wont_do), `deleted_at`, `source_ref`, `created_at`, `updated_at`.

Start date, deadline, duration and `wont_do` exist in the schema from Phase 1 even if the UI arrives later, so no migration blocks a Phase 3 feature.

### 6.3 Filter language

One grammar, one parser in Go (server, MCP) and one in TypeScript and Kotlin (clients, for offline views), one fixture corpus. The Go parser compiles a query to a SQL `WHERE` clause with parameters. Client parsers compile to a predicate over local rows. The grammar is Todoist's, plus the Phase 3 extensions.

### 6.4 Quick add parser

Input: one line. Output: `{title, due, rrule, priority, project, section, labels, assignee, deadline, duration, reminders}` plus the spans in the source text so the UI can highlight and un-parse tokens. Rules: tokens are removed from the title only when they parse, a date phrase inside quotes stays literal, and `#`, `@`, `/` need a match in the account to be consumed. English only. A project or a label keeps its Estonian letters, because a name is data rather than interface text.

### 6.5 MCP

Build against specification revision **2026-07-28**, not the 2025 session model. That revision makes MCP stateless, and it removes several parts the earlier draft of this plan assumed ([changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog), read 2026-08-25).

What the revision means for this server:

- **Off by default.** The endpoint mounts only when the operator turns it on, with `-mcp` or `TEHA_MCP=1`. Decided 2026-08-25. A task list is a map of a person's life and work, and an always-on tool endpoint on a public hostname widens the blast radius of one leaked token from reading tasks to driving them. An operator who wants it says so once.
- Same binary, path `/mcp`, Streamable HTTP over POST. Do not implement HTTP+SSE. The specification deprecates it.
- Do not depend on one vendor's client. A locally hosted model must drive the same tools, so keep every answer inside the token budget in §7 and never rely on a behaviour that only one client has.
- No `initialize` handshake and no `Mcp-Session-Id`. Each request carries the protocol version and the client capabilities in `_meta`. A version mismatch returns `UnsupportedProtocolVersionError`.
- Implement `server/discover`. It is mandatory, and it advertises the supported versions, the capabilities and the server identity.
- Every result carries `resultType`. Ordinary results use `"complete"`.
- `tools/list` returns a deterministic order and the `ttlMs` and `cacheScope` fields, so a client caches the list and the model keeps its prompt cache.
- Cross-call state travels as a server-minted handle in a normal tool argument. The list cursor is exactly such a handle.
- Skip Roots, Sampling and Logging. The specification deprecates all three, with a twelve-month window.
- Do not depend on stream resume. The revision removed `Last-Event-ID` and message redelivery. A broken stream means the client repeats the request with a new id, so every tool must stay idempotent.
- Live change notice, if an agent ever needs it, uses `subscriptions/listen`. Phase 1 does not need it.

Tool design, unchanged by the revision:

- Tool schemas describe the filter language in one paragraph, so the model writes a filter instead of a client-side loop.
- Output is compact JSON: `{t:[{id,ti,due,p,pr,lb}], next}`. A `fields` argument widens it. Errors are structured with a hint.
- Rate limits are per token and generous, because the server belongs to the user. A long list pages at 100 rows.
- A `plan_day` tool returns overdue, today, and the undated pile grouped by project in one call, because that is what the daily ritual needs.

Auth: a per-device bearer token in Phase 1. Claude Code connects with `claude mcp add --transport http <name> <url> --header "Authorization: Bearer <token>"`. Phase 3 adds OAuth 2.1 with **Client ID Metadata Documents**, not Dynamic Client Registration, which the revision deprecates. The client must validate the `iss` parameter (RFC 9207).

### 6.6 Security on a public hostname

- The service sits behind the public Traefik entrypoint with the existing rate limit and security header middleware. The app must protect itself.
- Passkeys are the primary login. Password fallback with Argon2id, lockout with exponential backoff per account and per client IP, and a login notification.
- The public entry point preserves the client address, so the app reads it from the forwarded header. Trust that header only from the proxy network, otherwise a client spoofs its own address and escapes a ban.
- Native apps and MCP use per-device tokens: random 256-bit, hashed at rest, named, listed, revocable, optional expiry. The web session is a secure, same-site cookie.
- Signup is invite-only. Registration endpoints are off unless an invite token is present.
- CSRF on the cookie session, strict CSP, no third-party scripts, no external fonts in the app.
- Attachments are stored on disk under the account, served with a signed URL, scanned for size and type. No SVG uploads.
- Audit: every login, token creation and failed attempt goes into `activity` and is visible in the UI.
- Backup files are encrypted at rest (Litestream to MinIO with server-side encryption, or age-encrypted snapshots).
- Optional hardening later: forwardAuth in Traefik, Cloudflare Access, mTLS for the author's own devices (lugu already supports client certificates).

### 6.7 Deployment

- **Container**: distroless image, one binary, `/data` volume with the SQLite file and attachments. Environment for the base URL, the data directory, the S3 target for Litestream. `docker compose` example in the repository for self-hosters.
- **Push keys**: `TEHA_VAPID_PUBLIC_KEY` in the deployment, `TEHA_VAPID_PRIVATE_KEY` in the secret. `teha -vapid-keys` makes the pair. The private key never enters an image, a manifest or this repository. See [DEPLOY.md](DEPLOY.md) and [DEV-SECRETS.md](DEV-SECRETS.md).
- **Cluster**: kustomize manifests in the private infrastructure repository, one replica (SQLite), PVC, `IngressRoute` on both the private and the public entrypoint, SOPS secrets. Public manifests in this repository show a generic example without hostnames.
- **Android**: Obtainium against GitHub Releases. `ci.yml` publishes one release per build, tagged `v<versionName>`, `versionName` carries the run number, `versionCode` increases per build, the signing key never changes, the repository is public. Shizuku paired once for silent installs. F-Droid later with reproducible builds.
- **macOS**: `.dmg` and `.zip` in the same GitHub release. Unsigned in Phase 1.
- **Web**: served by the binary at `/`. PWA manifest and service worker.

## 7. UX bar

- Quick add from tile tap to keyboard open: under 300 ms. From `Enter` to the task in the list: under 50 ms, before any network.
- Cold start of the Android app to a rendered Today list: under 500 ms from Room.
- Web app first render on a mid-range phone: under 1 s. Bundle under 250 KB compressed.
- Every list scrolls at 60 fps with 5 000 tasks.
- No modal confirmation for a reversible action. Undo instead.
- Every screen works with one hand on a phone. Primary actions at the bottom.
- Empty states say what to do next in one sentence.
- Screenshot tests guard every screen in both themes, and the README shows generated images that CI keeps current. See [DECISIONS.md](DECISIONS.md) D-006.
- **A change one person makes reaches the other person's screen in under one second, on the same network.** Two people shop in parallel, and a slower list means a duplicate in the basket. This is the number that makes shopping mode work, so it is a target and not a hope. Measure it on the phone, in a shop, over mobile data as well.

## 8. Milestones

| Milestone | Content | Exit test | State |
|---|---|---|---|
| M0 Bootstrap | Name, repository, licence, CI, plan | First signed commit, green CI | Name chosen (teha). CI written. Licence and the first commit are open. |
| M1 Core server | Schema, sync, filters, recurrence, export, import | Todoist account imports with zero loss. Property test converges. | Schema, sync, filters, recurrence and export run and carry tests. Import and the property test are open. |
| M2 Web | Views, quick add, keyboard, offline, PWA | Author uses the web app for one week without Todoist | Views, quick add, the task detail, the keyboard, offline and the service worker run. The week has not started. |
| M3 MCP | Tools, token auth, Claude Code config | An agent plans the day in three calls, never times out | Eight tools, stateless transport, token auth. `plan_day` answers in one call and 114 tokens. |
| M4 Android | Offline core, tile, share, gestures, Obtainium | Author uninstalls Todoist from the phone | Not started. |
| M5 macOS | Tauri app, global shortcut, URL scheme | Author removes the Todoist hotkey | A command line client covers capture first. |
| M6 Household | Sharing, comments, push, shopping mode | Partner uses it for groceries for one month without asking for Todoist. Two phones tick items in the same shop and neither buys a duplicate | Push runs: reminders, a daily digest and Web Push to the browser and the installed app. Sharing, comments and shopping mode are open, and they need the second account first. The partner uses Android, so the app matters more than the PWA. |
| M7 Beyond | Start dates, snooze, dependencies, review, macros, Obsidian bridge | A trip is planned in the vault and shopped from the app | Schema carries start date, deadline and `wont_do` already. |
| M8 Open | F-Droid, docs, hosted pilot | A stranger self-hosts from the README in under 15 minutes | Dockerfile, compose, kustomize and a deployment guide exist. |

## 8a. Decisions taken while building

Decisions with a lasting consequence live in [DECISIONS.md](DECISIONS.md):
the licence split (D-001), the Android parser binding (D-002), Web Push
(D-003), the iOS answer (D-004), the once-only reminder (D-010) and the
missed-reminder window (D-011).

Three choices in this plan changed when the code met reality. Each one is small, and each one has a reason.

| Plan said | Build does | Why |
|---|---|---|
| Client ids are UUIDv7 | 13-character sortable ids: 8 characters of millisecond time in base32, then 5 random | A UUID costs 36 characters in every MCP row. On a 200-row page that is about 1 000 tokens of pure identifier. The short id keeps the sort order and stays collision-safe for a household. |
| The web client holds SQLite in OPFS | The web client holds its state in `localStorage` | The proof of concept needed the sync loop, not the storage engine. This must change before the account holds years of history, and it is the one known shortcut in the web app. |
| MCP uses sessions and dynamic client registration | Stateless Streamable HTTP, revision 2026-07-28 | The revision removed sessions. The Go SDK needs `Stateless: true` explicitly, or it refuses a modern client. |

## 9. Engineering practices

- Every user promise is a test: parser fixtures, recurrence fixtures, filter fixtures, sync convergence, screenshot tests.
- Conventional commits `type(scope): what and why`. All commits signed.
- No hostnames, credentials, tokens or captures with real ids in the repository. Dev settings in a gitignored `local.properties` or `.env`.
- Every knowingly unfinished thing goes into `docs/BACKLOG.md` with the reason.
- Licence: GPL-3.0-or-later for the clients, AGPL-3.0-or-later for the server. The two licences combine, and a client that talks over HTTP only stays a separate program, so section 13 of the AGPL reaches the server alone. A self-hoster who modifies the server must offer its source to the people who use it.
- Contribution policy: a Developer Certificate of Origin certifies origin only. It does **not** keep the right to sell a differently licensed hosted build later. Only a Contributor Licence Agreement does that, and only the sole copyright holder can dual-license without one. Decide this before the first external pull request. Detail and sources: [research/05-name-and-licence.md](research/05-name-and-licence.md).
- Neither licence requires publication of the release signing key. Reproducible builds are an F-Droid practice, not a licence duty.
- Privacy: no analytics ever. Crash reporting only opt-in, off by default, self-hosted or EU-hosted.

## 10. Risks

| Risk | Mitigation |
|---|---|
| Two parsers drift | One fixture corpus, CI runs both against it, a fixture is added with every bug. |
| SQLite write contention with two people and an agent | WAL mode, one writer goroutine, batches. Load test with 50 commands per second. |
| Tauri quick add feels wrong on macOS | Keep the web app as the UI and build a Swift `NSPanel` quick add if needed. Two weeks of budget. |
| ~~Partner's phone is an iPhone~~ | Closed 2026-08-25. Both people use Android. iOS is now the installed web app only, see [DECISIONS.md](DECISIONS.md) D-004. |
| The author's Windows desktop has no client | Tauri v2 builds for Windows from the same source as macOS, so the desktop shell covers it. Confirmed as a requirement 2026-08-25. |
| Agent tasks turn the app into a second harness | Keep the boundary in §13. teha stores and shows the work. It never runs it. |
| Public exposure attracts scanners | Invite-only, passkeys, lockout per account and per IP, a flood ceiling at the proxy, no version in headers. Add forwardAuth if the logs show pressure. |
| Google developer verification (30 September 2026 in four countries, global in 2027) | Limited distribution tier covers 20 devices for free. F-Droid and ADB installs stay exempt. |
| `rrule-go` is stale (last tag 2023-01-13, last push 2024-08-15) and no better Go option exists | Wrap it behind our own interface, keep an exhaustive fixture corpus, and fork it if a bug appears. |
| Shizuku has had no push since 2025-06-18 | Silent install is a convenience, not a requirement. Obtainium still installs with a normal prompt. |
| The MCP specification changes fast (stateless redesign in 2026-07-28, features deprecated on a twelve-month clock) | Keep the MCP layer thin over the same service code the web app uses. Pin the SDK. Re-read the changelog at the start of each MCP milestone. |
| Apple App Store terms conflict with the GPL (the VLC case) | Phase 4 only. Either add a licence exception as the sole copyright holder, or ship iOS as a PWA. |
| Apple Developer ID reveals identity | Unsigned builds for personal use. Decide at Phase 4. |
| Scope creep from the feature map | Phase 1 has an exit test: uninstall Todoist. Nothing from Phase 2 starts before that. |

## 11. Name

Sibling projects use short Estonian words: *kuula* (listen), *vali* (choose), *lugu* (story). Every row below was checked on 2026-08-25: DNS for `.app`, `.dev`, `.io` and `.ee`, the GitHub search API, npm, PyPI, crates.io, and a search of Google Play and the App Store. Full method, output and caveats: [research/05-name-and-licence.md](research/05-name-and-licence.md). Trademark status is open for every candidate, because both the USPTO and the EUIPO search pages need their interactive application.

| Name | Meaning | `.app` / `.dev` | `.io` / `.ee` | Software collision | Reading | Verdict |
|---|---|---|---|---|---|---|
| **toimeta** | do the chores, imperative | free / free | free / free | none. Zero GitHub repositories, all three package registries free, no app found | clean | **Recommended.** The only candidate with nothing against it. Imperative, like *kuula* and *vali*. |
| **loend** | a list | free / free | free / free | none. 17 unrelated GitHub hits, registries free, no app found | clean | Strong runner-up. A noun, so it fits *lugu* rather than the imperatives. |
| **teha** | to do | free / free | taken / taken | no software collision. 459 substring hits on GitHub, none of them a task app. A German heating-cost company uses the term | clean | Still viable. It costs `.io` and `.ee`, and a company in an unrelated field shares the word. |
| kirjas | written down, on the list | free / free | free / free | none. A consulting firm shares the name | clean | Good. Slightly abstract for an app. |
| toime | to cope, to manage | free / free | free / taken | close to `toimer`, a small CLI timer | clean | Good. The meaning is idiomatic. |
| tehtud | done | taken / free | free / taken | none | clean | A done list, not a to-do list. |
| meeles | in mind | free / free | free / taken | a small Android food app uses this exact name | reads like "meal-less" | Avoid. |
| plaan | plan | taken / free | taken / taken | two apps in adjacent categories (dating, route planning) | clean | Avoid. |
| korda | in order | taken / taken | taken / taken | Gradle plugin family, a well-known surname | weak negative slang entry | Taken. |
| siht | aim, target | free / free | taken / taken | PyPI taken | **reads as an English profanity** | Rejected. |
| tegu | deed | not resolved | taken / taken | an AT&T open-source project, a live home-services app | also a lizard | Taken. |
| teed | roads | taken / taken | taken / taken | `TEED`, an edge-detection project (267 stars), a live golf app | slang for angry or drunk | Taken. |
| valmis | ready | taken / free | not resolved | `valmishq/valmis`, a self-hosted open-source AI agent platform (108 stars) | clean | Taken, and in our own space. |
| kava | plan, schedule | taken / taken | taken / taken | Kava Labs blockchain (461 stars), npm and PyPI taken | a plant and a drink | Taken. |

The v1 draft recommended **teha**. The evidence weakened it but did not kill it: `.io` and `.ee` are gone, and a German company in an unrelated field uses the word. No task app and no code project holds the name.

**Decision, 2026-08-25: teha.** The shorter word wins. Package `io.github.lightheaded.teha`, module `github.com/lightheaded/teha`, URL scheme `teha://`, lowercase always, listed as "teha — tasks". `teha.app` and `teha.dev` are free and need registering. `.io` and `.ee` are gone, and a German company in an unrelated field shares the word, so the trademark search still needs a person.

## 12. Open items

Decisions for the author. Everything that research can settle is settled.

- [x] **Name**: teha, decided 2026-08-25. Still open: register `teha.app` and `teha.dev`, and run the trademark search by hand at [USPTO](https://tmsearch.uspto.gov/search/search-information) and [EUIPO](https://euipo.europa.eu/eSearch/).
- [x] **The partner uses Android.** So the native Android app carries the household milestone, and the PWA is the bridge until it ships. Web Push on iOS no longer blocks anything.
- [x] **Contribution policy**: deferred until the first outside contribution arrives. Until then the author holds every copyright, so a later dual licence stays possible.
- [x] **Licence files**: added 2026-08-25. AGPL-3.0-or-later for the server, Apache-2.0 for the four shared packages and the parser corpus. See [DECISIONS.md](DECISIONS.md) D-001 and [LICENSING.md](../LICENSING.md).
- [x] **iOS**: the installed web app is the answer, and D-001 keeps the native path open at no cost. See D-002, D-003 and D-004.
- [x] **First commit**: made 2026-08-25, signed, as the alias identity.
- [ ] **Todoist backup endpoint**: confirm before the importer runs against the real account.

Settled by research on 2026-08-25:

- Todoist request limits: 1 000 partial-sync and 100 full-sync requests per 15 minutes per user, 100 commands per request, 1 MiB body, 15 second timeout.
- Todoist filters now use `%label`, not `@label`. The free plan is called Beginner.
- The MCP specification revision to build against is 2026-07-28, and it is stateless.
- Versions to pin: `modernc.org/sqlite` v1.57.0, Litestream v0.5.16, Room 2.8.4, Glance 1.1.1, Tauri v2.11.5, MCP Go SDK v1.7.0, Svelte 5.56.
- Added 2026-08-27: `github.com/SherClockHolmes/webpush-go` v1.4.0, the Web Push sender. It is the only maintained Go implementation of RFC 8291 and RFC 8292. It pulls `golang-jwt/jwt/v5` and `golang.org/x/crypto` with it. Two facts to keep in mind at an upgrade: the module has no `/v2` path, and v1.4.0 appends to the message slice a caller hands it, so a concurrent sender must copy the payload per send.
- The public entry point preserves the client address, so per-IP lockout works.
- `rrule-go` is the only serious Go RRULE library, and it is stale. The build wraps it.
- No existing open-source project fits. Vikunja comes closest and has no tile, no strong parser and no official MCP server.

---

## 13. Agent work in the task list

Proposed 2026-08-25, from the author's own note. **Not decided.** Read the
boundary below before building anything here.

### The problem

Several agent sessions run at once, and there is no single place that says what
each one is doing. The author loses track. A task manager is the natural
register: every session is work in progress, which is what a task is.

The wish has two halves:

1. An agent writes its own tasks into teha, so that human work and agent work sit
   in one list. Claude does this, and so must another harness.
2. A person opens a task, or a group of tasks, and starts an agent on it.

### The boundary

**teha stores and shows the work. It never runs it.**

The second half is where a task manager turns into a harness, and the author
named that risk himself. Hold the line here for three reasons:

- The server is on a public hostname. A task list that spawns processes is a
  remote code execution feature with a task list attached.
- Running work needs isolation, permissions, logs, retries and an audit trail. A
  task manager has none of those, and growing them means building a worse CI
  system.
- The value the author asked for is *visibility*, and visibility needs no
  execution. Knowing which five sessions are open, and which one is blocked, is
  the whole win.

So a task carries a pointer to work, never the work itself:

| Field | Holds |
|---|---|
| `source_ref` | the harness session identifier, the same column the Todoist import uses |
| `agent` | which harness owns it, for example `claude-code` or `opencode` |
| `cwd` | the working directory or the repository the session belongs to |
| `state` | running, waiting for a person, blocked, done |

Launching stays outside. A small local helper reads the task and starts the
harness: a shell script, a Raycast command, or a `teha://run?task=<id>` URL that
only the desktop client answers. That helper runs on the laptop, where the code
and the credentials already are. The server never learns how to start anything.

### What this needs

- A reserved label for agent work, so an ordinary view is not flooded.
- An MCP tool that lets a session claim a task and report progress, cheap enough
  to call often.
- A view that groups by session and shows which one is waiting for a person.
- A rule for stale rows: a session that stops without a final update must not
  leave a task that says "running" for ever.

### Open questions

- Does an agent write into the same account, or into its own project?
- What stops a chatty agent from filling the list with noise?
- Where do Hermes tasks fit, given that Hermes already files its own issues?
- Is a task the right shape for a session at all, or is a session a different
  row type that borrows the same views?

Answer these before writing code. The first version is worth building only after
the boundary above holds in a written decision.
