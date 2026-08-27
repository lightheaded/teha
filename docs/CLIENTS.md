# Clients

This page covers the command line client, the macOS hotkey and the MCP server.
The web app needs no setup: open the server address in a browser.

Every client on this page uses the device token. Passkeys are for the browser
only: WebAuthn needs a browser and a person's gesture, so a shell and an agent
cannot use one. The token is therefore not going away. Read
[USAGE.md](USAGE.md#passkeys) for the browser side.

||||||| 01c9321
Notifications are a browser feature, so the web app carries them. Open settings
with the gear in the header, or with the comma key. The command line client and
the MCP server set no reminder yet, and the MCP tool list below says so.

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
`list_projects`, `add_project`, `search` and `plan_day`. `list_projects` also
reports the sections of each project, and `/Name` filters by one. They take the same
filter language as `teha ls`.

No tool sets a reminder. An agent can give a task a due date and a due time,
and a person then arms the notification in the web app. The commands exist on
the sync endpoint (`reminder_add`, `reminder_update`, `reminder_delete`), so a
tool is a small addition when something needs one.

## Troubleshooting

| Message | Cause |
|---|---|
| `cannot reach the server at ...` | The server is down, or the address is wrong. |
| `the server refused the token` | The token does not match. Set `TEHA_TOKEN`, or write the token file. |
| `the token file ... has mode 0644` | Run `chmod 600` on the file. |
| `the name #Trip matches ...` | Two projects start with that name. Write the full name. |
| `no open task matches ...` | The task is complete already, or the fragment is wrong. |
