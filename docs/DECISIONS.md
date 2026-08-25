# Decisions

Each entry states what was decided, why, what it costs, and what reverses it.
The date is the date of the decision.

---

## D-001 — Two licences, split by tree

**Date:** 2026-08-25 · **Status:** done

The server is AGPL-3.0-or-later. Four shared packages (`id`, `filter`, `recur`,
`quickadd`) and `parser-fixtures/` are Apache-2.0.

**Why.** Apple adds redistribution limits to the App Store that the GPL family
forbids. A native iOS client that links GPL or AGPL code cannot ship there. A
permissive shared layer removes the conflict permanently, without an exception
clause and without a contributor licence agreement.

The alternative was one AGPL licence for everything, plus a GPL section 7
additional permission for the App Store. That works only while one person holds
every copyright. The first outside patch to the client tree ends it, and
reopening the path then needs written permission from that contributor.

**Cost.** The parser, the filter compiler, the identifier scheme and the
recurrence engine become free for anyone, including a closed competitor. About
2 000 lines. The sync engine, the server and the web app stay protected, and a
closed client still needs a server.

**Consequence.** The four packages moved out of `internal/`, because Go blocks
outside modules from importing an `internal/` path. Server code must never move
into them. See [LICENSING.md](../LICENSING.md).

**Reverses if:** the author decides that App Store distribution will never
happen, and no other client ever needs the shared layer.

---

## D-002 — Bind the Go parser for Android, do not rewrite it in Kotlin

**Date:** 2026-08-25 · **Status:** decided, acted on at M4

The Android app calls `quickadd`, `filter`, `recur` and `id` through
`gomobile bind`, rather than a second implementation in Kotlin.

**Why.** `gomobile bind` emits an `.aar` for Android and an `.xcframework` for
iOS from the same source. One binding decision therefore settles two platforms.
A Kotlin rewrite means a third parser now and a fourth in Swift later. Every
copy drifts, and every copy needs the corpus run against it.

The choice belongs to M4 and not to the iOS milestone. Once Kotlin holds a
second implementation, the iOS cost is a full third implementation. Decide it
early, because it gets more expensive, not less.

**Cost.** About 10 MB added to each platform binary. `gomobile` is lightly
maintained, so a broken release blocks a client build. `parser-fixtures/`
remains the contract, so a fallback rewrite stays possible.

**Reverses if:** `gomobile` stops working on a current Android or iOS toolchain.
The fallback is a Kotlin and a Swift implementation, both verified against
`parser-fixtures/quickadd.json`.

---

## D-003 — Push is Web Push with VAPID, not Firebase only

**Date:** 2026-08-25 · **Status:** decided, open until push exists

The server speaks the Web Push protocol with VAPID keys. Firebase Cloud
Messaging is not the only transport.

**Why.** Web Push reaches an installed iOS web app, Chrome on Android and every
desktop browser with one server-side implementation. A Firebase-only design
serves the Android app and nothing else, and it grows a second transport later.

Web Push on iOS needs the app on the home screen. A tab in Safari receives no
push.

**Cost.** VAPID key management, and a subscription record per device instead of
a single vendor token.

**Reverses if:** the Android app needs a delivery guarantee that only Firebase
provides. Then Firebase becomes a second transport beside Web Push, not a
replacement.

---

## D-004 — The iOS answer is the installed web app

**Date:** 2026-08-25 · **Status:** decided

iOS gets the progressive web app on the home screen. No native Swift client is
planned.

**Why.** The web app already exists, already works offline and already syncs.
A native client costs 99 USD each year, a Swift rewrite of the local store, and
a fourth client to keep in step. The features that only a native client reaches
are a Control Center control, home screen widgets, App Intents, Siri and the
share sheet. None of them blocks daily use.

D-001 keeps the native path open at zero cost. Walk it only when a missing
integration actually hurts.

**Cost.** No share sheet, no Shortcuts, no widget, no Control Center tile. iOS
can evict web app storage under pressure, and a delete of the home screen icon
clears everything.

**Consequence — a rule the sync design must hold.** The outbox is the only
client state that the server cannot rebuild. Every other row is a cache of the
command log. Therefore the client must drain the outbox eagerly and must keep it
small. An eviction then costs a re-sync and never an edit.

**Reverses if:** a Control Center control or a share sheet becomes the reason
capture fails. The licence layout in D-001 makes that a build decision, not a
legal one.

---

## D-005 — One repository, one release train per artifact

**Date:** 2026-08-25 · **Status:** decided

The server, the shared packages, the web app and the Android app live in one
repository. Releases separate by tag prefix, not by repository:

| Artifact | Versioned by | Published to |
|---|---|---|
| the server image | the commit hash | `ghcr.io/lightheaded/teha:<commit>` |
| the Android APK | `v<base>.<CI run number>` | a GitHub Release, for Obtainium |

**A GitHub Release in this repository is always an Android build, and never a
server build.** The server is versioned by its image tag in the registry, which
is the artifact store it already needs. That rule exists for one reason:
Obtainium follows the Latest release and expects an APK asset there. A server
release would take the Latest pointer and hand every phone a release with no
APK in it.

**Why one repository.** D-002 binds the Go parser into the Android app with
`gomobile bind`. Inside one module that binding reads the working tree. Across
two repositories it reads a published Go module, so every parser change needs a
tag, a release and a dependency bump before the app can use it. That turns a
one-line parser fix into a four-step release dance.

Three more reasons follow the same line:

- `parser-fixtures/quickadd.json` is the contract between the clients. One
  repository runs the Go parser, the web parser and the Android parser against
  it in one CI job. Two repositories cannot fail one build when they drift.
- The licence split (D-001) already spans the tree, and the Android app links
  the Apache-2.0 half. One `LICENSING.md` describes it once.
- A change that touches the sync protocol touches the server and every client.
  One commit shows the whole change, and one revert undoes it.

**Why the releases still separate.** Obtainium reads the tracked version out of
the git tag name, and its maintainer declined richer parsing, so a filter
expression is not a reliable escape hatch. The tag must therefore be exactly
`v` plus the version string. Keeping server builds out of GitHub Releases
entirely is what makes that possible, and it means a server change never shows
up as a phone update.

**Cost.** CI carries a Go job and an Android job, and the Android job is slow.
Path filters keep it from running on a server-only change. A reader who wants
only the Android app clones more than they need, which costs disk and nothing
else.

**Reverses if:** the Android app grows its own release cadence and its own
contributors, and the shared packages stabilise enough that a published Go
module version stops being a burden. Splitting later is cheap. `git filter-repo`
extracts the directory with its history, and the shared packages are already
importable, because D-001 moved them out of `internal/`.

---

## D-006 — README screenshots are generated and checked, never captured by hand

**Date:** 2026-08-25 · **Status:** done

`scripts/screenshots.sh` writes `docs/screenshots/*.png`. The README embeds
them. CI runs the same script with `--check` and fails when the committed images
no longer match the app.

**Why.** A hand-captured screenshot rots. Someone changes a layout, nobody
retakes the image, and the README quietly starts lying about the product. A
reader cannot tell a current image from a two-year-old one. The only fix that
holds is to make staleness fail the build.

**Why a container, and why three pins.** The browser runs inside
`mcr.microsoft.com/playwright`, pinned by digest tag, on `linux/amd64`. The
Playwright package version and the seed date are pinned beside it. Without that,
a font renders one pixel differently on a laptop and the check fails for a
reason nobody can act on. A screenshot check that cries wolf gets ignored within
a week, and then it is worse than nothing. lugu learned the same lesson from
Roborazzi: a baseline recorded on macOS never pixel-matches `ubuntu-latest`, and
it keeps a container recipe for exactly this reason.

**Three things are frozen**, because each one would otherwise change an image
that nobody edited:

- the seeded data, through the `-seed-date` flag added for this
- the browser clock, so that "today" agrees with the seeded data
- the locale and the time zone, so that a date reads the same everywhere

**Verified, not assumed.** Two consecutive runs produce byte-identical files.
Changing one accent colour makes four of the five images differ and the check
exit non-zero. Reverting it makes the check pass again.

**Cost.** About two minutes of CI on a change to the web app, and a developer
must run one command and commit five files after a visual change. The job is
path-filtered, so a server-only change does not pay it. A weekly scheduled run
catches drift from an upstream rebuild of the browser image, rather than letting
it surface inside somebody's unrelated pull request.

**Reverses if:** the images stop earning their place, or the app gains a
screen whose content cannot be made deterministic.

**Applies to lugu as well.** lugu already produces screenshots through Roborazzi
and shows none of them in its README. The same rule belongs there: publish the
canonical Roborazzi outputs and let the existing verify-by-default run keep them
honest.

---

## D-007 — The MCP endpoint is off unless the operator turns it on

**Date:** 2026-08-25 · **Status:** done

`/mcp` mounts only with `-mcp` or `TEHA_MCP=1`. Without it the path answers 404.

**Why.** A task list is a map of a person's life and work. An agent endpoint
drives that map rather than only reading it: it adds, edits and completes in
batches. The server also runs on a public hostname, and one device token guards
everything until passkeys land. Leaving the endpoint always on means one leaked
token gives an attacker the controls, not just the view.

Making it opt in costs an operator one flag, once. It buys a smaller blast
radius for every deployment that never wanted an agent.

**Consequence.** The cluster deployment sets `TEHA_MCP=1` explicitly, with a
comment saying why. That IS the opt in, and a reader of the manifest can see
that a person chose it.

**Also decided here:** do not depend on one vendor's client. A locally hosted
model must drive the same tools, so the token budget in PLAN.md section 7 is a
hard limit rather than a target, and no answer may rely on a behaviour that only
one client has.

**Reverses if:** authentication becomes per-client, so that an agent token can
be scoped and revoked separately from a person's. Then the endpoint can be on by
default, because a leaked agent token would no longer be a leaked account.
