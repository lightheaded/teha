# Clients

This page covers the command line client, the macOS hotkey, the macOS desktop
app and the MCP server.
The web app needs no setup: open the server address in a browser.

Every example uses the default address `http://127.0.0.1:8637`.

## Install

The client lives in the same binary as the server.

```sh
go build -o teha ./cmd/teha
sudo install -m 755 teha /usr/local/bin/teha
```

The first argument selects the client. `teha add`, `teha ls`, `teha done`,
`teha today` and `teha projects` talk to a server over HTTP. Every other
argument starts the server itself.

```sh
teha add --help    # the client help. "teha --help" lists the server flags.
```

## Point the client at a server

| Setting | Where | Default |
|---|---|---|
| Server address | `--server <url>`, or `TEHA_SERVER` | `http://127.0.0.1:8637` |
| Token | `TEHA_TOKEN`, or the token file | none |
| Rows in a list | `--limit <n>` | 50 |

An option works in any position, so the text comes first:

```sh
teha add "Buy milk tomorrow" --server http://127.0.0.1:8699
```

## The token file

The client reads the token from `TEHA_TOKEN` first. When that variable is
empty, it reads `$XDG_CONFIG_HOME/teha/token`, or `~/.config/teha/token` when
`XDG_CONFIG_HOME` is not set. A file keeps the token out of your shell history
and out of the process list.

```sh
mkdir -p ~/.config/teha
umask 077 && printf '%s' "$TEHA_TOKEN" > ~/.config/teha/token
chmod 600 ~/.config/teha/token
```

The client refuses a token file that a group or another user can read, and it
tells you the command that fixes the mode. The token never reaches the screen,
a log line or an error message.

## Commands

### `teha add "<one line>"`

The line parses on the client, so the result is on the screen before the server
answers. One line makes one task.

```sh
$ teha add "Book the ferry tomorrow at 9:30 p1 #Trip @call"
added: Book the ferry — due tomorrow 09:30, p1, #Trip, @call
```

The line takes:

| Part | Examples |
|---|---|
| Date | `today`, `tomorrow`, `tom`, `friday`, `next tuesday`, `next week`, `in 3 days`, `in 2 weeks`, `24.12`, `03.02.2027`, `5 sep` |
| Time | `at 9`, `at 7pm`, `9:30`, `18:00` |
| Priority | `p1` to `p4`, or `!!1` to `!!4` |
| Project | `#Trip` |
| Label | `@call`, and more than one per line |
| Repeat | `every day`, `every week`, `every month`, `every weekday`, `every tuesday`, `every 3 days` |

The corpus at `parser-fixtures/quickadd.json` is the contract for every parser.
The web app and this client answer every case in it the same way.

A project name matches an existing project exactly, or by a prefix when only
one project starts with it: `#Trip` finds `Trip to Setomaa`. A name that
matches nothing sends the task to the inbox, and the answer says so. A typo
therefore never makes a junk project. A name that matches two projects stops
with the candidates, and writes nothing.

### `teha today`

The tasks that are due today or earlier. This is the same as `teha ls today`.

```sh
$ teha today
t_M0X40RK3002RHMA  !  Thu 20 Aug  Fix the sink  #Home
t_M0X40RHT0020QDF     today       Buy oat milk  #Shopping
```

The columns are the id, the priority marker, the due date, the title, the
project and the labels. The marker is `!!!` for p1, `!!` for p2 and `!` for p3.
A `~` after a date means the task repeats. A terminal gets color; a pipe or a
file gets plain text.

### `teha ls "<filter>"`

The filter language of the web app and the API. See `filter/filter.go`
for the whole grammar.

```sh
teha ls "overdue | today"
teha ls "#Trip* & p1"
teha ls "@call & no date"
teha ls "search: ferry"
teha ls "recurring" --limit 100
```

**A project name behaves differently here than in quick add.** Learn this once,
because the two rules look the same and are not:

| Where | `#Trip` means |
|---|---|
| Quick add | the one project whose name starts with Trip. An ambiguous prefix is an error, and an unknown name sends the task to the inbox. |
| A filter | a project named exactly Trip. Nothing else matches. |

In a filter, add a `*` for a prefix: `#Trip*` finds `Trip to Setomaa`. The
reason for the difference is that quick add must choose ONE project to write
to, so it refuses to guess. A filter only reads, so a prefix that matches two
projects returns the tasks of both, which is a useful answer rather than an
error.

An empty filter means every OPEN task. A query that names a state keeps that
state: `done`, `completed`, `wont do`, `skipped` and `open` all work, and none
of them is narrowed to open tasks afterward.

### `teha done <id or title fragment>`

```sh
$ teha done ferry
done: Book the ferry
```

A fragment that matches more than one open task changes nothing:

```sh
$ teha done call
"call" matches 2 open tasks. Give the id or more of the title:
t_M0X40RJF000NSVM  Call the dentist
t_M0X40RJR000W3WN  Call the bank
$ echo $?
1
```

A repeating task moves to its next date instead of closing, and the output says
so. Every command exits with 0 after a success and with 1 after a failure.

### `teha projects`

```sh
$ teha projects
PROJECT   OPEN
Inbox     3
Home      1
Shopping  1
Trip      1
```

## A hotkey on macOS

The fastest capture is a key press from any application. `contrib/macos` holds
three ways to get one:

- `teha-quickadd.sh` shows a dialog with `osascript` and sends the line to
  `teha add`. Bind it in any launcher that runs a shell script.
- `add-task.sh` is a Raycast script command. Copy it into a Raycast script
  directory, then run **Add task**.
- `SHORTCUTS.md` explains the Apple Shortcuts recipe, both with the binary and
  with a URL action for an iPhone.

The short version with Apple Shortcuts: **Ask for Input**, then **Run Shell
Script** with `/usr/local/bin/teha add "$1"` and input passed as arguments,
then **Add Keyboard Shortcut** in the shortcut details. A shell script from
Shortcuts starts with a short `PATH`, so give the binary an absolute path.

## The macOS desktop app

`desktop/` holds a Tauri v2 shell around the same web app. It adds four things
to a browser tab: a global quick add, a menu bar icon, the `teha://` URL scheme
and one place for the server address. It holds no second copy of the web app
and no second quick add parser.

Build it from the root of the repository. `desktop/README.md` lists what you
need installed, and `make desktop` says what is missing before it starts.

```sh
make desktop         # the .app and the .dmg, unsigned
```

Move `teha.app` into `/Applications` and open it once. The shell asks for the
server address and the device token, then opens the app.

- The address and the shortcut go into a JSON file with mode 600.
- **The token goes into the keychain**, never into a file. See
  [DECISIONS.md](DECISIONS.md) D-009.

| Where | What it does |
|---|---|
| `CmdOrCtrl+Shift+A` | Opens the quick add panel over every application |
| `Enter` in the panel | Adds the task and closes the panel |
| `Escape` in the panel | Closes the panel and adds nothing |
| The menu bar icon | Quick add, open the app, settings, quit |
| **Settings** | Changes the address, the token and the shortcut |

The panel takes the same line as `teha add`. It gives the keyboard back to the
application you came from when it closes.

### The `teha://` URL scheme

```sh
open "teha://add?text=Book%20the%20ferry%20tomorrow%20at%209:30%20p1%20%23Trip"
```

One task, with no window on the screen. Percent encoding and `+` for a space
both work. Bind it in Apple Shortcuts with an **Open URLs** action, in Raycast
or in Keyboard Maestro.

The URL comes from outside the app, so the shell trusts nothing in it. Only the
`add` action does anything, the text becomes one line of at most 500
characters, and the line reaches a text field and never a shell. An unknown
action writes one line and changes nothing.

The scheme needs the bundled app. `make desktop-dev` runs the shell without a
bundle, and macOS registers no scheme for it.

## The MCP server, in Claude Code

The server serves MCP at `/mcp` over streamable HTTP. Register it once:

```sh
claude mcp add --transport http teha http://127.0.0.1:8637/mcp \
  --header "Authorization: Bearer $TEHA_TOKEN"
```

A project checks in a `.mcp.json` instead, so everybody in the repository gets
the same server. Keep the real token out of the file:

```json
{
  "mcpServers": {
    "teha": {
      "type": "http",
      "url": "http://127.0.0.1:8637/mcp",
      "headers": {
        "Authorization": "Bearer REPLACE_WITH_YOUR_TOKEN"
      }
    }
  }
}
```

The tools are `list_tasks`, `add_tasks`, `update_tasks`, `complete_tasks`,
`list_projects`, `add_project`, `search` and `plan_day`. They take the same
filter language as `teha ls`.

## Troubleshooting

| Message | Cause |
|---|---|
| `cannot reach the server at ...` | The server is down, or the address is wrong. |
| `the server refused the token` | The token does not match. Set `TEHA_TOKEN`, or write the token file. |
| `the token file ... has mode 0644` | Run `chmod 600` on the file. |
| `the name #Trip matches ...` | Two projects start with that name. Write the full name. |
| `no open task matches ...` | The task is complete already, or the fragment is wrong. |
