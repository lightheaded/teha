# Licensing

teha uses two licences on purpose. The table says which applies where.

| Tree | Licence | SPDX |
|---|---|---|
| `internal/api`, `internal/store`, `internal/mcpsrv`, `internal/webui`, `internal/todoist`, `cmd/teha` | GNU Affero General Public License v3.0 or later | `AGPL-3.0-or-later` |
| `id`, `filter`, `recur`, `quickadd`, `mobile`, `parser-fixtures`, `android` | Apache License 2.0 | `Apache-2.0` |

Full texts: [LICENSE](LICENSE) and [LICENSE.Apache-2.0](LICENSE.Apache-2.0).
Every source file carries an `SPDX-License-Identifier` line. That line is the
authority for that file.

## Why the server is AGPL

The server holds the value that is worth protecting: the sync engine, the
storage layer, the API and the web app. Section 13 of the AGPL closes the
network loophole. Anyone who runs a modified server and offers it to users over
a network must publish the changes to those users.

The duty applies to the author as well. A paid hosted service built on this code
must offer its source to its own users.

## Why the shared layer is Apache-2.0

Five packages define the shared contract between every client:

- `id` — the short sortable identifier scheme
- `filter` — the filter grammar and its compilers
- `recur` — recurrence rules
- `quickadd` — the quick add parser, with `parser-fixtures/` as its corpus
- `mobile` — the gomobile binding that exposes the four above to Android and iOS

The Android app in `android/` carries the same licence, for the same reason. It
is a client, it links the packages above, and it has to reach an app store one
day. A native iOS client will join it there.

A native client links these packages. Apple adds redistribution limits to the
App Store that the GPL family forbids. That conflict removed VLC from the store
in 2011. A permissive licence on the shared layer removes the conflict
permanently. No exception clause and no contributor licence agreement are
needed for it.

Apache-2.0 rather than MIT, because Apache-2.0 grants patent rights explicitly.

The direction matters and only works one way. Apache-2.0 code compiles into the
AGPL server. AGPL code must never move into these four packages. A change that
copies server code into the shared layer breaks the licence of every client that
links it.

These packages sit at the top level, not under `internal/`, because Go blocks
outside modules from importing an `internal/` path. A permissive licence on an
unimportable package helps nobody.

## What this costs

The parser, the filter compiler, the identifier scheme and the recurrence engine
become free for anyone, including a closed competitor. The server, the sync
engine and the web app stay protected. A closed client still needs a server.

## Contributions

The project takes no contributions yet. The choice between a Developer
Certificate of Origin and a contributor licence agreement stays open until the
first outside patch arrives. The difference matters:

- A DCO records that the contributor has the right to submit the code. It grants
  no right to relicense it.
- A CLA grants that right, and it costs contributor goodwill.

While the author holds every copyright, relicensing costs nothing. After the
first outside patch to an AGPL tree, a closed commercial fork of the server
needs permission from that contributor. Patches to the Apache-2.0 tree carry no
such constraint, which is why the iOS path stays open without a CLA.
