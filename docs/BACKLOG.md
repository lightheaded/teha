# Backlog

Everything knowingly left unfinished, with the reason. A line leaves this file
when the work is done or when a decision says it will never happen.

`docs/PLAN.md` holds the plan and the milestones. This file holds the debts
inside what already ships.

---

## The desktop shell (M5)

**No `Cargo.lock` in the repository.** The machine that wrote the shell has no
Rust toolchain, so no lock file exists yet. A binary crate wants one in the
repository, or two builds a month apart resolve different patch versions. The
first build on a machine with Rust writes it. Commit it then.

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
keyboard back because the shell is an accessory application while the panel is
the only window on the screen. That is how macOS behaves today. If a release
changes it, the fallback is a deactivate call through `objc2`.

**`docs/PLAN.md` section 9 still says the clients are GPL-3.0-or-later.**
`LICENSING.md` and `DECISIONS.md` D-001 say Apache-2.0, and the client trees
carry `SPDX-License-Identifier: Apache-2.0`. The SPDX line in a file is the
authority, so nothing is wrong in the code. The plan line is stale and needs
one edit by the author, who owns that decision.

**Windows and Linux are untried.** The shell compiles for them by design, and
`keyring` names the native store per platform. Nobody has built or run either.
The Windows desktop is a stated requirement in `docs/PLAN.md` section 1.
