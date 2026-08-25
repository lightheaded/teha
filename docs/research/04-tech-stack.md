# Research 04 — Technical building blocks

*Verified 2026-08-25 against GitHub, the Go module proxy, the npm registry, Google's Maven repository, and the modelcontextprotocol.io and code.claude.com documentation sites. Every version number below carries its source and read date. Unconfirmed items carry the **UNVERIFIED** tag.*

## 1. Local-first sync

| Option | Server needs | Client SDKs | Licence | Fit |
|---|---|---|---|---|
| Bespoke: client SQLite + outbox + `/sync?since=<version>` | None beyond our own server | Ours (Kotlin, TypeScript) | Ours | Best fit. Todoist, Linear, and lugu all use this pattern. The data set is small, conflicts are rare, and a last-write-wins rule per field is enough. |
| PowerSync (open edition) | Postgres, MongoDB, or MySQL, plus the PowerSync service | Kotlin, Swift, JS, Flutter. A Kotlin SDK repository exists (`powersync-ja/powersync-kotlin`) and had a commit on [2026-08-24](https://github.com/powersync-ja/powersync-kotlin) (read 2026-08-25). | [FSL-1.1-ALv2](https://github.com/powersync-ja/powersync-service/blob/main/LICENSE) (read 2026-08-25): each release converts to Apache 2.0 two years after publication. | Adds a service and a Postgres/Mongo/MySQL primary. Too heavy for the footprint target, but the licence claim in the earlier draft is correct. |
| ElectricSQL + TanStack DB | Postgres, Electric sync service | TypeScript. A GitHub search for a Kotlin ElectricSQL client returned no result on 2026-08-25, so treat it as web-only. | [Apache 2.0](https://github.com/electric-sql/electric) (read 2026-08-25) | Web only in practice, confirmed, not just a guess. |
| Zero (Rocicorp) | Postgres, zero-cache | TypeScript only | [Apache 2.0](https://github.com/rocicorp/mono) (read 2026-08-25) | Web only. |
| Replicache | Your server implements push and pull | TypeScript | Repository `rocicorp/replicache` is marked **archived** on GitHub, with no licence file detected (read 2026-08-25). Rocicorp point users to Zero as the successor. | Do not build on this. Superseded by Zero, not an active option. |
| cr-sqlite, Automerge, Yjs | None | Various | cr-sqlite: [MIT](https://github.com/vlcn-io/cr-sqlite), pushed 2026-08-10. Automerge: [MIT](https://github.com/automerge/automerge), pushed 2026-08-24. Yjs: MIT text in its `LICENSE` file, pushed 2026-08-06 (GitHub's licence detector reports NOASSERTION, but the file itself states MIT). All read 2026-08-25. | CRDT merge, all three actively maintained. Overkill for a task list. Ordering (`child_order`) is the one field that needs care. |
| Turso embedded replicas / libSQL | Turso or libSQL server | Rust, Go, JS. No official Kotlin client. A GitHub search on 2026-08-25 finds only [louloulin/kotlin-libsql](https://github.com/louloulin/kotlin-libsql), a single-author repository with zero stars, last push 2025-04-22. | [MIT](https://github.com/tursodatabase/libsql) (read 2026-08-25) | Read replicas, not a sync protocol for edits. |

Decision input: the data set stays small (thousands of rows per account). A version counter per account, an outbox on every client, and idempotent commands with client UUIDs give correct behavior with little code. Fractional indexes handle reorder without conflicts.

## 2. Server runtime

| Option | Idle RSS | Image size | Notes |
|---|---|---|---|
| Go, `modernc.org/sqlite`, `embed.FS` for web assets | 15 to 40 MB | 20 to 30 MB static | Single binary. Latest `modernc.org/sqlite` is [v1.57.0](https://proxy.golang.org/modernc.org/sqlite/@latest), published 2026-08-19 (read 2026-08-25), a pure-Go driver with no cgo requirement. `mattn/go-sqlite3` (cgo) stays actively maintained too: [MIT](https://github.com/mattn/go-sqlite3), 9,224 stars, pushed 2026-08-17 (read 2026-08-25) — cgo brings a slower build and cross-compile friction, so prefer `modernc.org/sqlite` for a static single binary. |
| Go recurrence library | — | — | `teambition/rrule-go` ([MIT](https://github.com/teambition/rrule-go), read 2026-08-25) has slowed: latest tag `v1.8.2` dates to 2023-01-13, and the last repository push was 2024-08-15 — over two years stale as of this read. A GitHub search for Go RRULE libraries on 2026-08-25 (`gh search repos rrule --language go`) returns no better-maintained option: `teambition/rrule-go` leads with 378 stars, and every alternative sits under 30 stars or has no push since 2024 (`stephens2424/rrule` 17 stars, last push 2019; `JulienBreux/rrule-go` 26 stars, 2017; `graham/rrule` 14 stars, 2023). So `rrule-go` is the only serious choice. Plan for a light fork or vendoring if a bug surfaces, since upstream response time is unproven. |
| Auth: `go-webauthn/webauthn` | — | — | Latest release [v0.17.4](https://github.com/go-webauthn/webauthn/releases), published 2026-05-22; repository pushed 2026-08-22 (read 2026-08-25). Licence: [BSD-3-Clause](https://github.com/go-webauthn/webauthn). Actively maintained, passkey support included. |
| MCP: `modelcontextprotocol/go-sdk` | — | — | Latest release [v1.7.0](https://github.com/modelcontextprotocol/go-sdk/releases), published 2026-07-28 (read 2026-08-25) — the same date as the current MCP spec revision. Licence: Apache 2.0 for new contributions, with pre-existing code kept under MIT, per the repository README. The `mcp/streamable_server.go` and `mcp/streamable.go` files confirm server-side Streamable HTTP support. Client-side OAuth is marked experimental in the README. No formal 1.0 stability statement was found; treat API stability as **UNVERIFIED** beyond what the version number implies. |
| SQLite replication: Litestream | — | — | Latest release [v0.5.16](https://github.com/benbjohnson/litestream/releases), published 2026-08-05; repository pushed 2026-08-24, 14,300 stars, licence [Apache 2.0](https://github.com/benbjohnson/litestream) (all read 2026-08-25). The 0.5 line first shipped as `v0.5.0` on 2025-09-30 and rebuilt replication on the LTX file format for compaction; earlier drafts calling 0.5 "not yet released" are wrong — it has been current for close to a year. |
| Rust, axum, rusqlite | 5 to 20 MB | 10 to 20 MB | Smallest. Slower to write. `rmcp` SDK, not checked in this pass. |
| Elixir, Phoenix | 80 to 150 MB | 100+ MB | LiveView gives a fast UI cheaply. Footprint and cold start are worse. `hermes_mcp` exists, not checked in this pass. |
| Node or Bun | 60 to 120 MB | 100+ MB | The official MCP SDK is TypeScript first. Footprint is worse than Go. |
| PocketBase | 30 MB | 40 MB | Go, SQLite, auth, and an admin UI included. Its data model and sync protocol fight the design. |

Decision: Go with `modernc.org/sqlite` (pure Go, no cgo cross-compile pain) and Litestream for backup, unchanged from the draft. Budget a maintenance risk line for `rrule-go`.

## 3. MCP

The spec changed more between 2025 and now than the earlier draft assumed. Read the current text before building against it.

- The current specification revision is [**2026-07-28**](https://modelcontextprotocol.io/specification/2026-07-28/) (read 2026-08-25 at `modelcontextprotocol.io/specification/versioning`), a stateless redesign of the previous, session-based protocol (`2025-11-25` and earlier). The [changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog) (read 2026-08-25) lists these changes against `2025-11-25`:
  - The `initialize` / `notifications/initialized` handshake is gone. Every request now carries its protocol version and client capabilities in `_meta` fields, and a version mismatch returns `UnsupportedProtocolVersionError`.
  - Protocol-level sessions and the `Mcp-Session-Id` header are gone from Streamable HTTP. A new mandatory RPC, `server/discover`, lets a client learn a server's supported versions, capabilities, and identity up front.
  - The HTTP GET endpoint and `resources/subscribe` / `resources/unsubscribe` are replaced by one `subscriptions/listen` stream that a client opts into per notification type.
  - Elicitation no longer works through `elicitation/create` plus a `notifications/elicitation/complete` signal. A server now returns an `InputRequiredResult`, and the client retries the original request with the answer attached — a pattern the spec calls Multi Round-Trip Requests (MRTR). Roots, Sampling, and Logging are marked Deprecated, with a twelve-month removal window.
  - The HTTP+SSE transport (deprecated since `2025-03-26`) is now formally Deprecated rather than merely discouraged. Build against Streamable HTTP only.
  - `inputSchema` and `outputSchema` for tools now allow any JSON Schema 2020-12 keyword, not a restricted subset — tool output schemas are a live, more flexible part of the spec.
  - OAuth: the specification asks an authorization server to send the `iss` parameter (RFC 9207), and a client must validate it; Dynamic Client Registration (RFC 7591) is Deprecated in favor of Client ID Metadata Documents, though it stays available for compatibility. Resource indicators and protected resource metadata, added in an earlier 2025 revision, remain part of the authorization baseline.
- Given a server rewrite that starts now, target the `2026-07-28` revision directly rather than the older session-based model the original draft described.
- Claude Code connects to a remote MCP server over Streamable HTTP with a bearer header: `claude mcp add --transport http <name> <url> --header "Authorization: Bearer <token>"`. SSE support remains for servers that expose only an SSE endpoint, but the documentation marks it deprecated and points to HTTP. OAuth sign-in runs through the `/mcp` command inside a session. Source: [code.claude.com/docs/en/mcp](https://code.claude.com/docs/en/mcp) (read 2026-08-25; `docs.claude.com/en/docs/claude-code/mcp` now redirects here).
- Design guidance that recurs in the community: few tools with clear names, structured JSON output, cursor-based pagination, no chatty per-item calls, short field names, omit nulls, a `fields` parameter to trim output. Not independently re-verified this pass.

## 4. Android

Room, Glance, and Compose Multiplatform version numbers below were re-checked on 2026-08-25 against Google's Maven repository and GitHub; the rest of this section carries over unchanged from a prior session's confirmation against developer.android.com and is not re-verified here.

- `TileService.startActivityAndCollapse(Intent)` throws for apps that target API 34 and later. Use the `PendingIntent` overload, or `TileServiceCompat.startActivityAndCollapse` from androidx.core. The intent needs `FLAG_ACTIVITY_NEW_TASK`.
- `isLocked()` is true when the lock screen shows. `showDialog()` then draws under the lock screen. `unlockAndRun(Runnable)` prompts for unlock first. `isSecure()` reports whether a lock is set.
- `StatusBarManager.requestAddTileService` (API 33 and later) shows a system prompt to add the tile, so the user does not need to edit the shade by hand.
- Android 15: `PendingIntent` creators block background activity launches by default. Edge-to-edge display is enforced. Android 16: the edge-to-edge opt-out is removed, and predictive back is on by default.
- Tasks.org implements this tile pattern and launches its new-task activity directly. It is GPL and a valid reference.
- Widgets: **Glance 1.1.1 stays the current stable release**, published 2024-10-16 per [Google's release notes](https://developer.android.com/jetpack/androidx/releases/glance) (read 2026-08-25). `1.2.0-rc01` shipped 2025-12-03, and `1.3.0-alpha02` is the newest pre-release, from 2026-07-01 — so 1.2 stable has not landed yet as of this read, contrary to a plan that assumes it has. Widgets cannot host a live text field; use a button that opens the activity.
- App shortcuts: static, dynamic, and pinned, with a combined static-plus-dynamic limit of about four per activity.
- App Functions (`androidx.appfunctions`) is still Alpha: Google's Maven metadata lists `1.0.0-alpha10` as the latest and only release track (read 2026-08-25), matching the draft's claim. This is the current path for exposing actions to Gemini; the older App Actions documentation has moved and its status stays unclear.
- **Room's current stable release is 2.8.4, published 2025-11-19** ([release notes](https://developer.android.com/jetpack/androidx/releases/room), read 2026-08-25) — newer than the 2.7.0 figure in the earlier draft. Kotlin Multiplatform support (Android, iOS, JVM desktop, native macOS, native Linux) began in the 2.7.0 stable release (2025-04-09); the 2.8.0 release (2025-09-10) added watchOS and tvOS targets. Pin 2.8.4 or later, not 2.7.0.
- Compose Multiplatform (JetBrains): latest release [v1.12.0](https://github.com/JetBrains/compose-multiplatform/releases), published 2026-08-25 (read same day) — active and current. iOS reached stable in the 1.8 line in 2025, per the earlier draft; Swift export stays Alpha.
- UnifiedPush + ntfy: `binwiederhier/ntfy` is [Apache 2.0](https://github.com/binwiederhier/ntfy), 33,745 stars, pushed 2026-08-20 (read 2026-08-25) — a healthy, active project, and a sound choice for push without Google Play Services.

## 5. macOS quick add

| Option | Footprint | Effort | Notes |
|---|---|---|---|
| Tauri v2 app: hosts the web UI, global shortcut plugin opens a small always-on-top window, tray icon, deep-link plugin for `app://add?text=` | 10 to 30 MB RSS, 10 MB binary | Medium | One web codebase serves web and desktop. Same parser, same offline store. Latest release [tauri-v2.11.5](https://github.com/tauri-apps/tauri/releases), published 2026-07-01; licence [Apache 2.0](https://github.com/tauri-apps/tauri); repository pushed the same day as this read, 2026-08-25, 110,549 stars — an active, healthy project. |
| Native Swift menu bar app: non-activating `NSPanel`, `KeyboardShortcuts` package, posts to the API | 15 MB | Medium | Best feel. A second UI codebase. Needs its own parser or a server `/parse` call. Not independently re-verified this pass. |
| Raycast extension or Alfred workflow | 0 | Low | A good extra, not a replacement. Raycast stays closed source. |
| Apple Shortcuts via URL scheme | 0 | Low | Serves Siri and automation use. |

Distribution outside the App Store needs Developer ID signing and notarization (99 USD per year, not re-verified this pass). The certificate carries the legal name of the developer. An unsigned build runs for personal use after `xattr -d com.apple.quarantine` removes the quarantine flag; current Gatekeeper still shows a warning dialog for a build without that step, and this was not re-tested against the newest macOS release this pass — treat the exact Gatekeeper wording as **UNVERIFIED**. A Homebrew cask can serve an unsigned build with a caveat line, but users still see the Gatekeeper warning first.

## 6. Exposure outside the VPN

Not independently re-verified this pass beyond what appears below; the underlying products (Traefik, Cloudflare Tunnel, Tailscale Funnel, Pangolin, mTLS, WebAuthn) are mature and their behavior is unlikely to have shifted materially since the prior draft. WebAuthn browser coverage was spot-checked:

| Option | Notes |
|---|---|
| Public hostname behind the existing public reverse proxy, app-level auth only | What the cluster runs today for eight services. Rate-limit and security-header middleware exist. The public entry point preserves the real client address (the load balancer service uses `externalTrafficPolicy: Local`), and the proxy passes it in `X-Forwarded-For`, so per-IP lockout works at the app. Trust that header only from the proxy network, otherwise a client spoofs its own address. Verified against the cluster manifests on 2026-08-25. |
| Cloudflare Tunnel + Access | Adds Cloudflare between the user and the app. Access needs an identity provider or a one-time PIN by email. The free tier covers 50 users. Native apps and MCP clients need service tokens or the Access cookie — friction for MCP. |
| Tailscale Funnel | The same conflict with the work VPN applies to the client, but Funnel is public, so the client needs no Tailscale. Adds a Tailscale node in the cluster. |
| Pangolin | An open-source tunnel plus identity-aware proxy. Still a young project. |
| mTLS with client certificates | Strong, and lugu already supports it. Certificate install is painful for a partner on iOS and Android browsers. MCP clients cannot present a client certificate. |
| Passkeys (WebAuthn) in the app | Phishing-resistant, synced through iCloud Keychain and Google Password Manager. Good for a partner. Go library: `go-webauthn/webauthn` (details in section 2). Browser support is broad: per [caniuse.com/webauthn](https://caniuse.com/webauthn) (read 2026-08-25), Chrome, Safari (desktop and iOS 14.5+), Edge, and Samsung Internet report full support; Firefox stays partial. |

## 7. Obsidian integration

Not independently re-verified this pass, except the Local REST API plugin's activity:

- Obsidian Tasks syntax: `- [ ] text 📅 2026-09-01 ⏳ 2026-08-30 🔁 every week 🆔 abc ⛔ def`. Query blocks render live lists.
- Obsidian Bases (2025) queries frontmatter and file properties into tables and cards. It does not read task lines.
- Obsidian URI: `obsidian://open?vault=X&file=Y`, `obsidian://new`, `obsidian://search`.
- The Local REST API plugin exposes the vault over HTTPS on localhost, desktop only. Repository `coddingtonbear/obsidian-local-rest-api`: [MIT](https://github.com/coddingtonbear/obsidian-local-rest-api), 2,848 stars, pushed 2026-08-25 (read same day) — actively maintained.
- The vault syncs over Syncthing, and Syncthing runs in the same cluster. A server-side folder writer is therefore possible without a plugin. Write with a lock-file or an atomic rename to avoid a Syncthing conflict copy on a concurrent write.
- Existing Todoist plugins (Todoist Sync, Ultimate Todoist Sync) render query blocks and mirror checkboxes both ways. Users report drift and duplicate tasks after conflicts.

## 8. Push without Google Play Services

| Option | Notes |
|---|---|
| UnifiedPush with ntfy | Open standard. Run ntfy in the cluster; see section 4 for its current activity level. The Android app registers through a distributor app, such as ntfy. No Google dependency. F-Droid friendly. |
| FCM | Needs Firebase in the app. Blocked by the F-Droid inclusion policy. Fine as an optional build flavor for a Play Store release. |
| Web Push (VAPID) | Works in Chrome and Firefox. Per [caniuse.com/push-api](https://caniuse.com/push-api) (read 2026-08-25), Safari on iOS carries only **partial** support starting at iOS 16.4, through the current 26.x line — it works for a web app added to the home screen, but expect gaps against the desktop implementation (for example, weaker background delivery guarantees). Confirm the exact constraint list against Apple's own release notes before relying on it; the caniuse partial-support flag was not decomposed further in this pass — **UNVERIFIED** in detail. |
| Local scheduling | Reminders for a known due time can fire from the device without any push, from the local database. Push is needed only for changes made elsewhere. |

## What changed from the previous draft

- **MCP spec revision**: the previous draft said "verify the current revision date." It is [**2026-07-28**](https://modelcontextprotocol.io/specification/2026-07-28/), and it replaces the session-based, handshake-based protocol model with a stateless one. Elicitation, Roots, Sampling, Logging, and the HTTP+SSE transport all changed status (elicitation reworked around MRTR; the other three Deprecated; SSE formally Deprecated, not just discouraged). Build against this revision, not the 2025 model the draft assumed.
- **Litestream 0.5**: the draft treated 0.5 as pending. It shipped 2025-09-30 and the current release is v0.5.16 (2026-08-05) — 0.5 has been the current line for close to a year.
- **Room**: the draft cited 2.7.0. Current stable is 2.8.4 (2025-11-19); KMP gained watchOS and tvOS in 2.8.0. Pin 2.8.4 or later.
- **Replicache**: the draft flagged its licence as "free since 2024, verify." The repository is archived with no licence file GitHub can detect. Rocicorp direct users to Zero. Do not plan around Replicache.
- **PowerSync licence**: confirmed as written — FSL-1.1-ALv2, converting to Apache 2.0 two years after each release. A Kotlin SDK exists and is active.
- **rrule-go**: last tagged 2023-01-13, last pushed 2024-08-15 — flag as a maintenance risk; no confirmed stronger alternative was found in this pass.
- **Glance 1.2**: still not stable as of 2026-08-25 (RC since 2025-12-03); do not plan around it shipping stable on a fixed date.
