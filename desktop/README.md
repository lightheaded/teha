# The teha desktop shell

A small Tauri v2 shell around the web app of your own server. It gives the
laptop what a browser tab cannot:

- a **global shortcut** that opens a quick add panel over every application,
- a **menu bar icon** with quick add, the app and quit,
- the **`teha://` URL scheme**, for Apple Shortcuts, Raycast and Keyboard
  Maestro,
- one place for the **server address**, with the **device token in the
  keychain**.

It is not a second client. It holds no copy of the web app and no second quick
add parser. The window points at your server, and the page that arrives is the
page a browser gets. The panel hands its line to the quick add field of that
page, so one parser and one fixture corpus stay the contract for every client.

The build is **unsigned**. Signing and notarization is a Phase 4 decision,
because the certificate carries a legal name. See `docs/PLAN.md` section 4.

## What you need installed

| Tool | Why | How |
|---|---|---|
| Rust, stable | The shell is Rust | `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \| sh` |
| Xcode command line tools | The linker and WebKit | `xcode-select --install` |
| Tauri command line tool 2.11.4 | Builds the `.app` and the `.dmg` | `cargo install tauri-cli --version 2.11.4 --locked` |
| Python 3 | Only to redraw the icons | Already on macOS |
| Node | Only for the contract test | The repository already needs it for `make test` |

There is no npm and no bundler. The two pages of the shell are plain HTML, CSS
and JavaScript, and the web app comes from the server.

## Build

From the root of the repository:

```sh
make desktop-check   # cargo check. The fast answer to "does it still build?"
make desktop-dev     # a window on the screen, rebuilt on every change
make desktop         # the .app and the .dmg
```

Each target says what is missing before it starts. The bundle appears in
`desktop/src-tauri/target/release/bundle`.

Move `teha.app` into `/Applications` and open it once. macOS registers the
`teha://` scheme from the bundle, so the scheme works after that first open and
not before. `make desktop-dev` runs the shell without a bundle, so the scheme
does not work in that mode.

A `.dmg` that you copy to another Mac carries the quarantine flag. Because the
build is unsigned, the first open needs a right click and **Open**, or:

```sh
xattr -d com.apple.quarantine /Applications/teha.app
```

## The first run

The shell opens a settings window and asks for two things:

1. **The server address.** An origin, for example `https://teha.example` or
   `http://127.0.0.1:8637`. A path, a query and a fragment are dropped. Any
   scheme other than `http` or `https` is refused.
2. **The device token.** The same token the browser asks for. Read
   `docs/DEPLOY.md` for where it comes from.

The address and the shortcut go into a JSON file with mode 600, in the
application configuration directory of your account. **The token goes into the
keychain**, under the service `io.github.lightheaded.teha.desktop` with the
server address as the account. It is never in a file, never in a log line and
never in this repository.

The settings window shows the token once, when you type it. After that the
field stays empty and says that the keychain holds it. An empty field on a
later save keeps the token that is there, so you can correct the address alone.

## The quick add panel

| Key | What it does |
|---|---|
| `CmdOrCtrl+Shift+A` | Opens the panel, or closes it when it is open |
| `Enter` | Adds the task and closes the panel |
| `Escape` | Closes the panel and adds nothing |

The panel takes the same line as the web app and the command line client:
`milk tomorrow p1 #Home @store every week`. `docs/USAGE.md` section 3 holds the
whole syntax.

The panel is on top of every window, and it gives the keyboard back to the
application you came from when it closes. On macOS this works because the shell
is an accessory application while the panel is the only window on the screen:
it has no Dock icon and no place in the application switcher. The window that
hosts the web app takes the Dock icon back while it is open.

The panel also closes when it loses the keyboard, so a click somewhere else
does not leave it on the screen.

**To change the shortcut**, open **Settings** in the menu bar menu. A shortcut
is an accelerator, for example `CmdOrCtrl+Shift+A`, `Alt+Space` or
`Ctrl+Shift+Space`. A save takes effect at once. A string that the system
refuses falls back to `CmdOrCtrl+Shift+A`, and the shell writes one line about
it. The label in the menu bar menu reads the new shortcut at the next start.

## The `teha://` URL scheme

```sh
open "teha://add?text=Book%20the%20ferry%20tomorrow%20at%209:30%20p1%20%23Trip"
```

The URL adds one task with no window on the screen. Percent encoding and `+`
for a space both work, and a project name keeps its Estonian letters.

The URL is an input from outside the application, so the shell trusts nothing
in it:

- Only the `add` action does anything. Every other action writes one line and
  changes nothing. `teha://run?task=...` from `docs/PLAN.md` section 13 is not
  decided, so this build refuses it.
- The text becomes **one line**: the first line only, with every control
  character removed, trimmed, and cut at 500 characters.
- The line reaches a **text field**, never a shell. Nothing in this shell
  builds a command line.
- The line crosses into the page as a JSON string literal, with the two Unicode
  line separators escaped as well.
- No log line holds the URL or the text. A task title is a person's own words.

In **Apple Shortcuts**: an **Ask for Input** action, then **Open URLs** with
`teha://add?text=` and the input appended, then **Add Keyboard Shortcut** in
the shortcut details. `contrib/macos/SHORTCUTS.md` holds the recipe that uses
the binary instead, which still works.

In **Raycast** and in **Keyboard Maestro**: an action that opens a URL.

## How the shell reaches the web app

Two scripts, both from Rust into the page, and nothing in the other direction.

1. **Sign in.** The window loads the server. The server sends the login page
   when there is no cookie. The script posts the token from the keychain to
   `/login`, the way the login form does, then loads the app. The token stays
   inside a function, so no script of the page can read it.
2. **Add a task.** The script puts the line into the field with the id `qa`,
   raises an `input` event and an `Enter` key event, and waits for the field to
   clear. That is the whole contract with the web app: **the quick add field
   has the id `qa`, and it clears itself after it adds a task.** A change to
   that field in `internal/webui/assets` breaks quick add here, so a test holds
   the contract and needs no Rust to run:

   ```sh
   node --test desktop/tools/contract.test.mjs
   ```

   `make desktop-check` runs it first, and so does CI. It reads `app.js` as
   well, so a rename of the field fails the build rather than the shortcut.

The page from the server is a remote origin. **No capability names it**, so it
reaches no command of this shell at all. `capabilities/local.json` names the
two local pages only.

## The layout

```
desktop/
  src/                     the two pages the shell serves itself
    quickadd.html/.js      the panel: one field, Enter and Escape
    settings.html/.js      the address, the token and the shortcut
    shell.css              the colours of the web app
    index.html             a note. Nobody opens it
  src-tauri/
    Cargo.toml             every version pinned exactly
    tauri.conf.json        the bundle, the CSP and the teha:// scheme
    capabilities/          which page can call which command
    icons/                 drawn by tools/make-icons.py
    src/
      main.rs              the plugins, the setup and the URL action
      settings.rs          the file, the keychain and the input rules
      scheme.rs            the teha:// parser, with its tests
      web.rs               the window that hosts the web app, and the bridge
      panel.rs             the quick add panel
      setup.rs             the settings window
      shortcut.rs          the global shortcut
      tray.rs              the menu bar icon
      policy.rs            the macOS Dock behaviour
      commands.rs          the four commands the local pages call
      js/                  the two scripts that go into the page
  tools/
    make-icons.py          draws the icons from the web app shape
    contract.test.mjs      holds the DOM contract with the web app
```

## Versions

Every version in `Cargo.toml` is exact.

| Crate | Version |
|---|---|
| `tauri` | 2.11.5 |
| `tauri-build` | 2.6.3 |
| `tauri-plugin-deep-link` | 2.4.9 |
| `tauri-plugin-global-shortcut` | 2.3.2 |
| `tauri-plugin-single-instance` | 2.4.3 |
| `keyring` | 3.6.3 |
| `serde` | 1.0.229 |
| `serde_json` | 1.0.151 |
| `url` | 2.5.8 |
| `tauri-cli` (a tool, not a dependency) | 2.11.4 |

Tauri v2 keeps the global shortcut, the tray icon, the deep link and the single
instance behaviour on separate release trains, so the plugin versions do not
follow the version of `tauri` itself.

## The icons

`tools/make-icons.py` draws the bundle icons and the menu bar mask from the
same shape as `internal/webui/assets/icon.svg`. It uses the standard library
only, and it writes the same bytes on every run.

```sh
python3 desktop/tools/make-icons.py
```

The menu bar image is a mask: macOS reads the alpha channel and paints the
colour, so one file follows a light and a dark menu bar.

## What this shell does not have

- **No signature.** See the note at the top.
- **No parse hint in the panel.** The web app shows what it understood before
  you press `Enter`. The panel does not, because the parser is in the page. The
  panel reports a failure instead.
- **No log file.** Lines go to standard error. To read them, start the binary
  from a terminal: `/Applications/teha.app/Contents/MacOS/teha`.
- **No offline settings check.** The shell cannot tell a wrong address from a
  server that is down. Both look like a page that does not load.
- **No `Cargo.lock` yet.** The first build on a machine with Rust writes it,
  and it belongs in the repository after that. See `docs/BACKLOG.md`.

`docs/BACKLOG.md` holds the rest, with the reason for each one.
