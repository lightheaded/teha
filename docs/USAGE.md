# Use teha

This page tells you how to run teha and how to use it every day. It covers the
server, the web app, the quick add syntax, the filter language, the command
line client, the MCP server and the Todoist importer.

Every command, every quick add line and every filter query on this page was run
against this code. The examples use the seed data from `teha -seed`, and the
fixed date Tuesday 2026-08-25.

Contents:

1. [Start the server](#1-start-the-server)
2. [The web app](#2-the-web-app)
3. [Quick add](#3-quick-add)
4. [Filters](#4-filters)
5. [The command line client](#5-the-command-line-client)
6. [Claude and MCP](#6-claude-and-mcp)
7. [Import from Todoist](#7-import-from-todoist)
8. [What does not work yet](#8-what-does-not-work-yet)

---

## 1. Start the server

### Build the binary

```sh
cd /path/to/teha
go build -o teha ./cmd/teha
```

The build needs no cgo. One binary holds the server, the web app, the MCP
server, the command line client and the Todoist importer.

To make the binary available to a hotkey, install it:

```sh
sudo install -m 755 teha /usr/local/bin/teha
```

### The first run

Write the example data one time. The command exits after the write.

```sh
./teha -db teha.db -seed
```

The output is:

```
  14 commands, 0 failed, version 18
seeded
```

Now start the server without a token:

```sh
./teha -db teha.db -dev
```

Open <http://127.0.0.1:8637>.

### Flags

| Flag | Environment | Default | Function |
|---|---|---|---|
| `-addr` | `TEHA_ADDR` | `127.0.0.1:8637` | The listen address. |
| `-db` | `TEHA_DB` | `teha.db` | The path to the SQLite file. |
| `-token` | `TEHA_TOKEN` | empty | The device token. |
| `-dev` | — | off | No token, debug logs. |
| `-seed` | — | off | Write example data and exit. |
| `-version` | — | — | Print `teha 0.1.0 (proof of concept)` and exit. |

The flag wins over the environment variable.

The word `serve` in front of the flags is optional. `teha serve -db teha.db` and
`teha -db teha.db` do the same thing. The first argument selects a client only
when it is `add`, `ls`, `done`, `today`, `projects` or `import`.

### Where the database goes

- `-db` names the file. The default is `teha.db` in the working directory.
- The server creates the parent directory of that path with mode 0750.
- SQLite runs in WAL mode. Two more files appear beside the database:
  `teha.db-wal` and `teha.db-shm`. Copy all three, or stop the server first.
- The container image sets `TEHA_DB=/data/teha.db`. Read
  [DEPLOY.md](DEPLOY.md) for the container and the backup.

### The device token

One token guards `/v1/*`, `/mcp`, `/login` and the web app. `/v1/health` needs
no token.

If `-token` and `TEHA_TOKEN` are both empty, and `-dev` is off, the server makes
a new random token at each start. It prints the token to stderr:

```
  No TEHA_TOKEN was set, so this run uses a new one:

    9f3c...  (64 hexadecimal characters)

  Export it to keep the same token across restarts.
```

A new token at every restart logs out every client. To keep one token, make it
one time and export it:

```sh
export TEHA_TOKEN=$(openssl rand -hex 32)
./teha -db teha.db -addr 127.0.0.1:8637
```

| Client | How it sends the token |
|---|---|
| The web app | One POST to `/login`. The server keeps the token in the `teha_token` cookie for one year. |
| The command line client | The header `Authorization: Bearer <token>`, from `TEHA_TOKEN` or the token file. |
| An MCP client | The header `Authorization: Bearer <token>`. |
| Any HTTP client | The header, or the cookie. |

`-dev` sets the token to empty and allows every request. Use `-dev` on a
private machine only.

Write the token to a file, so that it stays out of the shell history:

```sh
mkdir -p ~/.config/teha
umask 077 && printf '%s' "$TEHA_TOKEN" > ~/.config/teha/token
chmod 600 ~/.config/teha/token
```

The command line client refuses a token file that a group or another user can
read. It names the file and the `chmod` command. The token value never reaches
the screen, a log line or an error message.

### The HTTP routes

| Route | Method | Function |
|---|---|---|
| `/v1/sync` | POST | `{since, commands[]}` in, changed rows out. At most 200 commands per request, at most 4 MB. |
| `/v1/tasks` | GET | `?filter=`, `?limit=`, `?offset=`. At most 500 rows. |
| `/v1/projects` | GET | Every project. |
| `/v1/labels` | GET | Every label. |
| `/v1/export` | GET | The whole account as one JSON file. |
| `/v1/events` | GET | Server-sent events. One `version` event per write, and a ping every 25 seconds. |
| `/v1/health` | GET | `{"ok":true,"version":18}`. No token. |
| `/mcp` | POST | The MCP endpoint. |
| `/login` | GET, POST | The token form. |
| `/` | GET | The web app. |

---

## 2. The web app

Open the server address, for example <http://127.0.0.1:8637>.

### Log in

With a token set, `/` sends the browser to `/login`. Type the token, then press
**Sign in**. The server writes the `teha_token` cookie. The cookie is HTTP-only
and same-site. It carries the Secure flag over TLS only. It lasts one year.

A wrong token returns to the form with the message `That token did not match.`

### The screen

| Part | What it does |
|---|---|
| Sidebar | Six built-in views, then one entry per project. Each entry shows a count. The sidebar appears at a window width of 800 px or more. |
| Header | The view name, the number of tasks, and the status. |
| Status | `v18` after a good sync. `3 to send` while the outbox holds commands. `offline · 3 queued` after a failed sync. |
| Quick add box | One line makes one task. Press Enter. |
| Hint line | What the parser found, as you type. |
| Task list | Groups in this order: Overdue, Today, Tomorrow, then one group per date, then No date. |
| The circle | Click it to complete the task. |
| The rest of the row | Click it to open the detail sheet. |
| Overdue section head | A **Reschedule** button. It moves every overdue task in the view. |
| Selection bar | Appears at the bottom when tasks are picked. It says how many, and holds Schedule, Priority, Move, Complete and Delete. |
| Toast | An **Undo** button for six seconds after a completion, a deletion, a reschedule or a bulk action. |
| The round + button | On a narrow screen, it moves the cursor into the quick add box. |

The built-in views are filter queries:

| View | Query |
|---|---|
| Today | `today` |
| Overdue | `overdue` |
| Next 7 days | `week` |
| Inbox | `#inbox` |
| No date | `no date` |
| Priority 1 | `p1` |

A project entry in the sidebar runs `#<project name>`.

The list order is the due date, then the priority, then the title. A task with
no date goes last.

### Move every overdue task in one gesture

The morning after a busy week a dozen tasks all say yesterday. To move them all:

1. Open a view that shows overdue tasks, for example Today.
2. Touch **Reschedule** on the Overdue section head.
3. Pick a day.

The choices are Today, Tomorrow, This weekend, Next week, No date, and a date
field. Each choice shows the day it means, so nothing is guessed. `Shift+T` does
the same as picking Today.

The button acts on the view on screen and never on a task the view hides. So
**Reschedule** inside a project view moves that project's overdue tasks alone.

A task keeps its time of day. A repeating task keeps its rule and only its next
date moves. **No date** takes the time away with the day, because a task with a
time and no day is one that no view can print.

The toast holds an **Undo** button for six seconds. Undo puts every task back on
the day it had, and the time with it.

### Act on many tasks at once

To pick a set of tasks:

1. Hold the platform modifier, Command on a Mac or Control elsewhere, and click
   a task. The row is then picked.
2. Hold Shift and click another task. Every row between the two is picked.

The keyboard does the same. `s` picks the task under the cursor. `Shift+A` picks
every task in the view. Escape drops the whole set.

A bar appears at the bottom. It says how many tasks are picked, and it holds
five actions:

| Action | What it does |
|---|---|
| Schedule | The same day menu as the overdue button, and a date field. |
| Priority | p1 to p4. |
| Move | One project per entry. The inbox reads as Inbox. |
| Complete | Closes every picked task. A repeating task moves to its next date. |
| Delete | Removes every picked task. |

While a set is picked, every action key acts on the set instead of on the task
under the cursor. So `1` sets p1 on the whole set, and `t` moves the whole set
to today.

Each action drops the selection and offers one **Undo** for six seconds. Undo
puts every task back, one task at a time.

The set never includes a task the view hides. A mark survives a change of view,
and an action on a row the user cannot see is a change nobody can check.

### The detail sheet

The sheet holds every field of one task: title, notes, due date, time, start
date, deadline, priority, project, labels and the repeat rule. The labels field
takes a comma separated list, for example `store, call`. The repeat field takes
a raw RRULE string, for example `FREQ=WEEKLY;BYDAY=MO`.

To add a sub-task, type a title in the last field and press Enter.

**Delete** removes the task. **Escape** closes the sheet. Each field saves when
it loses focus. The sheet never waits for the server.

### Keys

| Key | Action |
|---|---|
| `q` or `a` | Move the cursor into the quick add box. |
| `j`, `k`, arrow down, arrow up | Move down and up the list. |
| `x` or Enter | Complete the selected task. |
| `1` to `4` | Set the priority. |
| `t` | Due today. |
| `m` | Due tomorrow. |
| `w` | Due in seven days. |
| `Shift+T` | Move every overdue task in the view to today. |
| `s` | Pick the selected task for a bulk action. |
| `Shift+A` | Pick every task in the view. |
| Command or Control, and click | Pick one task. |
| Shift and click | Pick a run of tasks. |
| `e` or `o` | Open the detail sheet. |
| Backspace, Delete or `#` | Delete the selected task. |
| `u` | Undo the last completion or deletion. |
| `r` | Sync now. |
| `g` | Go to Today. |
| `?` | Show the key list. |
| Escape | Drop the selection, close the sheet, or leave the quick add box. |

### Offline

The browser holds the account in `localStorage`. Every edit lands in the local
state and in an outbox. The screen updates first. The outbox drains to
`POST /v1/sync` when the network allows it. A retry runs every two seconds
while the outbox holds commands. A command carries a uuid, so a retry is safe.

The service worker caches `/`, `/app.js`, `/parse.js`,
`/manifest.webmanifest` and `/icon.svg`. It never caches `/v1/` or `/mcp`.

| With the server down | State |
|---|---|
| Add a task, with the full quick add syntax | Works |
| Complete, delete, undo | Works |
| Priority keys, the `t`, `m` and `w` keys | Works |
| Reschedule the whole overdue section, and undo it | Works |
| Every bulk action on a picked set, and undo it | Works |
| Every field in the detail sheet | Works |
| The six built-in views, and a project view | Works |
| The first load in a new browser | Needs the server one time |
| The login | Needs the server |
| An unusual filter term | See the next table |

### The browser filter is a subset

The browser evaluates the filter over the local rows. It knows fewer terms than
the server. A term that it does not know becomes a title search, so a view
quietly shows the wrong rows.

| The browser knows | The browser does not know |
|---|---|
| `today`, `tod`, `tomorrow`, `overdue`, `od`, `week`, `next 7 days`, `no date`, `recurring`, `subtask`, `done`, `completed`, `no priority`, `p1` to `p4`, `#Project`, `#inbox`, `%label`, `@label`, `search: text`, a bare word, and the operators `&`, `|`, `,`, `!` | `##Project`, `#Project*`, `%label*`, `no label`, `before:`, `after:`, `date:`, `deadline`, `deadline:`, `created:`, `yesterday`, `no time`, `has time`, `top level`, `no parent`, `started`, `deferred`, `wont do`, parentheses |

One more difference: in the browser `#Trip` finds `Trip to Setomaa` by prefix.
On the server `#Trip` needs the exact project name. Read
[section 4](#4-filters) for the rule.

---

## 3. Quick add

One line makes one task. The parser runs on the client, so the task appears
before the network call. The web app, the command line client and the Android
binding share one corpus, `parser-fixtures/quickadd.json`.

Rules:

- The parser reads the tokens in this order: repeat rule, priority, project,
  labels, time, date.
- A token leaves the title only when it parses.
- A space must come in front of `#`, `@`, `p1` and `!!1`. The start of the line
  counts as a space.
- One line takes up to ten labels, and one project.
- A repeat rule with a weekday also sets the first due date.

### Dates

Today is Tuesday 2026-08-25 in every example.

| Write | Example line | Due date |
|---|---|---|
| `today`, `tod`, `tonight` | `Buy milk today` | 2026-08-25 |
| `tomorrow`, `tom`, `tmr` | `Buy milk tomorrow` | 2026-08-26 |
| `<weekday>` | `Gym friday` | 2026-08-28 |
| `<weekday>` for today | `Gym tuesday` | 2026-08-25 |
| `next <weekday>` | `Gym next tuesday` | 2026-09-01 |
| `next week` | `Dentist next week` | 2026-09-01 |
| `in N days` | `Pay rent in 3 days` | 2026-08-28 |
| `in N weeks` | `Book flights in 2 weeks` | 2026-09-08 |
| `D.M` | `Christmas dinner 24.12` | 2026-12-24 |
| `D.M.YYYY` | `Renew passport 03.02.2027` | 2027-02-03 |
| `D <month>` | `Party 5 sep` | 2026-09-05 |

A weekday name means the next such day, and today counts. `next` always adds a
full week. A day and a month with no year mean the next time that date comes.

### Times

| Write | Example line | Time |
|---|---|---|
| `at H` | `Standup today at 9` | 09:00 |
| `at H am`, `at H pm` | `Call mum at 7pm` | 19:00 |
| `H:MM` | `Standup at 9:30` | 09:30 |
| `HH:MM` | `Standup at 18:00` | 18:00 |

The word `at` is optional in front of `H:MM`.

### Priority, project and labels

| Write | Example line | Result |
|---|---|---|
| `p1` to `p4` | `Fix the sink p1` | priority 1 |
| `!!1` to `!!4` | `Fix the sink !!2` | priority 2 |
| `#Name` | `Oat milk #Shopping` | project Shopping |
| `@name` | `Oat milk #Shopping @store` | label store |
| Several labels | `Review the plan @work @deep` | labels work and deep |

Priority 1 is the highest. Priority 4 means no priority.

A label name with no label row creates the label. A quick add therefore never
fails on a new label.

### Repeat

| Write | Example line | RRULE |
|---|---|---|
| `every day` | `Water the plants every day` | `FREQ=DAILY` |
| `every week` | `Water the plants every week` | `FREQ=WEEKLY` |
| `every month` | `Pay rent every month` | `FREQ=MONTHLY` |
| `every year` | `Pay tax every year` | `FREQ=YEARLY` |
| `every weekday` | `Standup every weekday` | `FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR` |
| `every <weekday>` | `Take out the bins every tuesday` | `FREQ=WEEKLY;BYDAY=TU`, due 2026-08-25 |
| `every N days` | `Water the tomatoes every 3 days` | `FREQ=DAILY;INTERVAL=3` |
| `every N weeks` | `Water plants every 2 weeks` | `FREQ=WEEKLY;INTERVAL=2` |
| `every N months` | `Review every 3 months` | `FREQ=MONTHLY;INTERVAL=3` |

The quick add parser knows these nine forms only. For any other rule, open the
detail sheet and type the RRULE, or send it through MCP.

A repeating task moves to its next date on completion. It does not close. A
task that is months overdue moves to its next real slot, not to the slot after
the old one.

### One full line

```
Call the plumber tomorrow p1 #Home @call
```

Title `Call the plumber`, due 2026-08-26, priority 1, project Home, label call.

### When the project name does not match

The rule is the same everywhere: a typo must never make a new project.

| Case | The web app | `teha add` | MCP `add_tasks` |
|---|---|---|---|
| The name matches one project, exactly or by prefix | That project | That project | That project |
| No project matches | The inbox. The hint says `(no such project, goes to the inbox)` before you press Enter. | The inbox. The second output line says `no project matches #Garden, so the task is in the inbox`. Exit code 0. | That one task fails with `no project named "Garden"`. Every other task in the same call still applies. |
| Two or more projects start with the name | The inbox. The hint says `(matches 2 projects, type more)`. | Nothing is written. The client prints `the name #Ho matches Home, Homelab. Write the full name` and exits with 1. | That one task fails with `"Ho" matches Home, Homelab: use the full name`. |

`#Trip` finds `Trip to Setomaa`, because one project starts with `Trip`. The
match ignores upper and lower case.

### A known limit

The parser takes a priority token from inside a sentence:

| Line | Title | Priority |
|---|---|---|
| `Read about p2 engines` | `Read about engines` | 2 |

The corpus records this case. Put such a word in the description instead.

---

## 4. Filters

One filter language runs in the app, in `/v1/tasks`, in `teha ls` and in the
MCP tools. The server compiles a query to a SQL `WHERE` clause.

### The grammar

A query is a set of terms joined by operators. The lexer splits the text on
`&`, `|`, `,`, `!`, `(` and `)` only. Everything between two operators is one
term, so a term can hold a space, for example `no date`.

| Operator | Meaning | Example |
|---|---|---|
| `&` | and | `today & p1` |
| `\|` | or | `overdue \| today` |
| `,` | or, the same as `\|` | `today, tomorrow` |
| `!` | not | `!recurring` |
| `( )` | group | `(today \| overdue) & #Home` |

`&` binds tighter than `|`. Use parentheses when the order matters.

### Date terms

| Term | Meaning |
|---|---|
| `today`, `tod` | Due today or earlier. This includes overdue tasks. |
| `date: today` | Due exactly today. |
| `tomorrow`, `tom` | Due tomorrow. |
| `yesterday` | Due yesterday. |
| `overdue`, `od`, `over due` | Due before today. |
| `week`, `next 7 days`, `7 days` | Due in the next seven days, and overdue. |
| `no date`, `no due date`, `nodate` | No due date. |
| `no time` / `has time` | No time of day / a time of day. |
| `date: <date>`, `due: <date>` | Due on that date. |
| `before: <date>`, `date before:`, `due before:` | Due before that date. |
| `after: <date>`, `date after:`, `due after:` | Due after that date. |
| `deadline` / `no deadline` | A deadline is set / not set. |
| `deadline: <date>` | The deadline is that date. |
| `deadline before: <date>` | The deadline is before that date. |
| `created: <date>` | Created on that date. |
| `created before: <date>`, `created after: <date>` | Created before or after that date. |

A `<date>` takes these forms: `today`, `tomorrow`, `yesterday`, `3 days`,
`+5 days`, `-3 days`, `2026-12-24`, `24.12.2026`, `Jan 2 2026`, `2 Jan 2026`,
or a weekday name. A weekday name means its next occurrence, never today.

### State terms

| Term | Meaning |
|---|---|
| `p1` to `p4` | The priority. |
| `no priority` | Priority 4. |
| `recurring` | The task has a repeat rule. |
| `subtask` | The task has a parent. |
| `no parent`, `top level` | The task has no parent. |
| `done`, `completed` | Completed tasks. |
| `wont do` | Tasks closed as will not do. |
| `started` | No start date, or a start date today or earlier. |
| `not started`, `deferred` | A start date in the future. |

### Place terms

| Term | Meaning |
|---|---|
| `#Name` | The project with that exact name. |
| `#Name*` | Every project whose name starts with `Name`. |
| `#inbox` | The inbox. |
| `##Name` | That project and every sub-project under it. |
| `%label`, `@label` | Tasks with that label. |
| `%lab*`, `@lab*` | Tasks with a label that starts with `lab`. |
| `no label`, `no labels` | Tasks with no label. |
| `search: text` | The text in the title or the description. |
| a bare word | The same title and description search. |

Both `%label` and `@label` work. Todoist moved filters from `@label` to
`%label`, and retires `@` through 2026. teha accepts both marks, so an imported
filter still works, and an old habit still works.

### Worked queries

Every query below ran against the seed data.

| Query | What it gives you |
|---|---|
| `today` | The daily list: due today, plus everything overdue. |
| `date: today` | Only the tasks due today, with no overdue work. |
| `overdue` | Only the late work. |
| `week` | The next seven days, plus the overdue work. |
| `no date` | The undated pile, ready for a weekly review. |
| `p1` | Every top priority task, at any date. |
| `no priority` | Every task that never got a priority. |
| `recurring` | Every repeating chore. |
| `#Home` | The project named exactly `Home`. |
| `#Trip*` | Every project whose name starts with `Trip`. |
| `##Home` | `Home` and its sub-projects together. |
| `#inbox` | The capture pile that needs a project. |
| `%store` | The shopping label, wherever the task lives. |
| `@store` | The same result. The two marks are equal. |
| `no label` | Tasks that no label reaches. |
| `search: milk` | `milk` in a title or a description. |
| `milk` | The same search, written short. |
| `before: friday` | Due before the next Friday. |
| `after: today` | Everything in the future, with today left out. |
| `overdue \| today` | The same rows as `today`, written the long way. |
| `#Trip to Setomaa & p1` | The trip work that is urgent. |
| `(today \| overdue) & !%errand` | Today, with the errands taken out. |
| `today, tomorrow` | Two days in one list. A comma means or. |
| `no date & #Shopping` | The shopping list. |
| `done` | Completed tasks. |
| `deferred` | Tasks with a start date in the future. |

### Six traps

1. `#Trip` finds nothing when the project is `Trip to Setomaa`. A filter needs
   the exact project name. Write `#Trip*`, or the full name. The quick add box
   is different: there a prefix is enough.
2. `today` includes the overdue tasks. For today alone, write `date: today`.
3. A filter shows open tasks only, unless the query names a state itself.
   `done`, `completed`, `wont do`, `skipped` and `open` are the terms that
   count. A text search such as `search: done` does NOT turn the rule off,
   because the test reads what the parser saw and not the query text.
4. An empty filter means every open task. `teha ls` with no argument shows the
   same rows as `teha ls "open"`.
5. A word that the grammar does not know becomes a title search. `overdu`
   returns nothing, and no error.

A bad query gives a clear error:

```
$ teha ls "before: never"
teha: the server answered 400 Bad Request: cannot read the date "never"

$ teha ls "today & ("
teha: the server answered 400 Bad Request: expected a term, found ""
```

---

## 5. The command line client

The client lives in the same binary. It keeps no local copy. Each command is
one or two HTTP calls, so a capture from a hotkey costs one round trip.

The first argument selects the client: `add`, `ls`, `done`, `today` or
`projects`. `import` runs the importer. Every other first argument starts the
server.

### Options

| Setting | Where | Default |
|---|---|---|
| Server address | `--server <url>`, or `TEHA_SERVER` | `http://127.0.0.1:8637` |
| Token | `TEHA_TOKEN`, or `~/.config/teha/token` | none |
| Rows in a list | `--limit <n>` | 50 |
| Help | `-h`, `--help` | — |

An option works in any position, so the text comes first:

```sh
teha add "Buy milk tomorrow" --server http://127.0.0.1:8699
```

Every command exits with 0 after a success, and with 1 after a failure.

### `teha add "<one line>"`

```
$ teha add "Book the ferry next tuesday at 9:30 p1 #Trip @call"
added: Book the ferry — due Tue 1 Sep 09:30, p1, #Trip to Setomaa, @call
```

An unknown project name still captures the task:

```
$ teha add "Order gravel #Garden"
added: Order gravel
no project matches #Garden, so the task is in the inbox
```

An unclear project name writes nothing:

```
$ teha add "Test ambiguity #Ho"
teha: the name #Ho matches Home, Homelab. Write the full name
$ echo $?
1
```

### `teha today`

The tasks due today or earlier. This is the same as `teha ls today`.

```
$ teha today
t_h1  !!   Sun 23 Aug ~  Change the water filter  #Home
t_h3  !!!  today         Call the plumber         #Home  @call
t_i1  !!   today         Read the plan and pick the first milestone
t_h2       today ~       Take out the bins  #Home
```

The columns are the id, the priority mark, the due date, the title, the project
and the labels. The marks are `!!!` for p1, `!!` for p2 and `!` for p3. A `~`
after the date means that the task repeats. An overdue date and a p1 mark are
red in a terminal. A pipe or a file gets plain text.

### `teha ls "<filter>"`

```
$ teha ls "overdue | today"
t_h1  !!   Sun 23 Aug ~  Change the water filter  #Home
t_h3  !!!  today         Call the plumber         #Home  @call
t_i1  !!   today         Read the plan and pick the first milestone
```

More examples that work:

```sh
teha ls "#Trip to Setomaa & p1"
teha ls "@call & no date"
teha ls "search: ferry"
teha ls "recurring" --limit 100
teha ls "(today | overdue) & !%errand"
```

An empty result prints one line:

```
$ teha ls "#Trip & p1"
no tasks match
```

### `teha done <id or title fragment>`

```
$ teha done ferry
done: Book the ferry
```

A repeating task moves to its next date. It does not close:

```
$ teha done bins
done: Take out the bins — it repeats, so the next date is set
```

A fragment that matches more than one open task changes nothing:

```
$ teha done the
"the" matches 7 open tasks. Give the id or more of the title:
t_s1  Book the guest house
t_s2  Print the route
t_h1  Change the water filter
t_h2  Take out the bins
t_h3  Call the plumber
t_i1  Read the plan and pick the first milestone
t_i2  Try the MCP server from a Claude session
$ echo $?
1
```

An exact id always wins over a title fragment.

### `teha projects`

```
$ teha projects
PROJECT          OPEN
Inbox            2
Home             3
Shopping         3
Trip to Setomaa  3
```

The inbox comes first. The other projects follow in alphabetical order.

### A hotkey on macOS

`contrib/macos/` holds three ways to reach `teha add` from any application:

- `teha-quickadd.sh` shows a dialog with `osascript`.
- `add-task.sh` is a Raycast script command.
- `SHORTCUTS.md` explains the Apple Shortcuts recipe for a global hotkey.

A shell script from Shortcuts starts with a short `PATH`. Give the binary an
absolute path, for example `/usr/local/bin/teha`.

**The desktop app is the shorter road.** `desktop/` holds a Tauri shell around
the same web app, with a quick add panel of its own:

| Where | What it does |
|---|---|
| `CmdOrCtrl+Shift+A` | Opens a quick add panel over every application |
| `Enter` in the panel | Adds the task and closes the panel |
| `Escape` in the panel | Closes the panel and adds nothing |
| The menu bar icon | Quick add, open the app, settings, quit |

The panel takes the same line as `teha add`, and it gives the keyboard back to
the application you came from. The shortcut is in **Settings** in the menu bar
menu. The device token lives in the keychain, and the server address in a file
with mode 600. See [DECISIONS.md](DECISIONS.md) D-009.

The app also answers a URL, which is what a launcher wants:

```sh
open "teha://add?text=Book%20the%20ferry%20tomorrow%20at%209:30%20p1%20%23Trip"
```

One URL adds one task, with no window on the screen. Percent encoding and `+`
for a space both work. In Apple Shortcuts use an **Open URLs** action instead
of **Run Shell Script**, so no `PATH` is involved at all.

The URL is an input from outside the app. Only the `add` action does anything,
the text becomes one line of at most 500 characters, and the line reaches a text
field and never a shell. An unknown action writes one line and changes nothing.

Build it with `make desktop`. [../desktop/README.md](../desktop/README.md) lists
what you need installed. The build is unsigned in Phase 1.

### Messages and their causes

| Message | Cause |
|---|---|
| `cannot reach the server at ...` | The server is down, or the address is wrong. |
| `the server refused the token` | The token does not match. Set `TEHA_TOKEN`, or write the token file. |
| `the token file ... has mode 0644` | Run `chmod 600` on the file. |
| `the name #Trip matches ...` | Two projects start with that name. Write the full name. |
| `no open task matches ...` | The task is complete already, or the fragment is wrong. |
| `the line has no title left after the date and the tags` | Every word in the line parsed as a token. |

---

## 6. Claude and MCP

**The MCP endpoint is off by default.** Start the server with `-mcp`, or set
`TEHA_MCP=1`. Without it `/mcp` answers 404.

```sh
teha serve -db ~/teha.db -mcp
```

The reason is blast radius. A task list is a map of a person's life and work. An
agent endpoint drives that map rather than only reading it, so one leaked token
must not hand an attacker the controls as well. An operator turns it on once,
deliberately.

The server serves MCP at `/mcp` over streamable HTTP, specification revision
2026-07-28, in stateless mode. There is no session header and no `initialize`
handshake. A `tools/call` POST gets an answer at once.

### Register the server in Claude Code

```sh
claude mcp add --transport http teha http://127.0.0.1:8637/mcp \
  --header "Authorization: Bearer $TEHA_TOKEN"
```

A repository checks in a `.mcp.json` instead, so that every session gets the
same server. Keep the real token out of the file:

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

With `-dev`, the server needs no header at all.

### The eight tools

| Tool | Arguments | Answer |
|---|---|---|
| `list_tasks` | `filter`, `fields` (only `description`), `limit` (default 50, maximum 200), `cursor` | `{"t":[...],"n":3}`, plus `"next"` when a page follows |
| `add_tasks` | `tasks[]`: `title`, `project`, `due`, `time`, `priority`, `labels`, `repeat`, `description`, `start_date`, `deadline`, `parent_id` | `{"ok":2,"ids":[...],"errors":[...],"v":25}` |
| `update_tasks` | `tasks[]`: `id`, plus any field above, plus `clear[]` | The same write shape |
| `complete_tasks` | `ids[]`, `wont_do` | The same write shape |
| `list_projects` | none | `{"projects":[{"id":"p_home","name":"Home","open":3}]}` |
| `add_project` | `name`, `color` | The same write shape |
| `search` | `text`, `limit` (default 50) | `{"t":[...],"n":1}` |
| `plan_day` | none | Overdue, due today, and the undated pile by project |

`clear[]` accepts six field names: `due_date`, `due_time`, `rrule`,
`start_date`, `deadline` and `parent_id`. Any other name fails.

`repeat` takes an RRULE string, for example `FREQ=WEEKLY;BYDAY=MO`. The server
validates the rule before it writes.

A write tool applies the whole batch in one transaction. One bad command fails
alone, and the answer names it in `errors`. The rest still apply.

A bad filter returns a tool error that repeats the whole grammar, so the model
repairs the query without a failed session.

### The short keys in an answer

The answer drops every empty field, to save tokens.

| Key | Field |
|---|---|
| `id` | The task id |
| `ti` | The title |
| `due` | The due date, `YYYY-MM-DD` |
| `tm` | The time, `HH:MM` |
| `p` | The priority. Priority 4 is dropped. |
| `pr` | The project name. The inbox is dropped. |
| `lb` | The labels |
| `rec` | The RRULE |
| `sub` | True when the task has a parent |
| `d` | The description, on request only |
| `st` | The state, when it is not open |

### A real `plan_day` answer

```json
{"today":"2026-08-25",
 "overdue":[{"id":"t_h1","ti":"Change the water filter","due":"2026-08-23","p":2,"pr":"Home","rec":"FREQ=MONTHLY"}],
 "due":[{"id":"t_h3","ti":"Call the plumber","due":"2026-08-25","p":1,"pr":"Home","lb":["call"]},
        {"id":"t_i1","ti":"Read the plan and pick the first milestone","due":"2026-08-25","p":2}],
 "undated":{"Inbox":["Order gravel"],"Shopping":["Oat milk","Rye bread"],"Trip to Setomaa":["Print the route"]},
 "n":{"due":2,"overdue":1,"undated":5}}
```

The whole daily plan costs one call and about 460 bytes.

### Three things to ask Claude

1. "Plan my day in teha." Claude calls `plan_day` one time, and reads the whole
   picture from one answer.
2. "Move every overdue task in Home to tomorrow." Claude calls `list_tasks`
   with `overdue & #Home`, then `update_tasks` one time with every id.
3. "Add the shopping list for the trip: bread, coffee, salt, matches, gas."
   Claude calls `add_tasks` one time with five titles and the project
   `Shopping`.

Give Claude the exact project name when you know it. A prefix works, but two
projects with the same first letters make that one task fail.

---

## 7. Import from Todoist

### The command

```sh
export TODOIST_TOKEN=<your Todoist API token>
./teha import --dry-run --db teha.db     # read, print the summary, write nothing
./teha import --db teha.db               # the real import
```

| Flag | Environment | Default | Function |
|---|---|---|---|
| `--token` | `TODOIST_TOKEN` | none | The Todoist API token. It is necessary. |
| `--db` | `TEHA_DB` | `teha.db` | The SQLite file to write. |
| `--dry-run` | — | off | Read Todoist, print the summary, write nothing. |
| `--timeout` | — | 15m | The limit for the whole read. |

Stop the server before the import, or restart it after. The importer writes
straight to the file. `/v1/events` sends no notice, so an open browser keeps
the old rows until you reload it.

The importer reads the account with one full sync. It stays inside the
documented Todoist limits, and it writes in batches of 100 commands.

### What the importer maps

| Todoist | teha |
|---|---|
| A live project | A project. Parents come before children. |
| The Todoist inbox | The fixed inbox |
| A label | A label |
| A task | A task, with `source_ref` set to `todoist:<id>` |
| The priority, API 4 down to 1 | 1 to 4. The two scales run in opposite directions. |
| The due date, time and zone | `due_date`, `due_time`, `due_tz` |
| A deadline | `deadline` |
| A duration | `duration_min` |
| A repeat string | An RRULE. `every!` sets the from-completion flag. |
| A sub-task | `parent_id` |
| A completed task | The task, then a completion command |
| A section name | The first line of the description |
| A task comment | The end of the description |
| The child order | `order_key` |

The importer converts 33 recurrence forms, among them `every other week`,
`every mon, fri`, `every 2nd tuesday`, `every last day of the month` and
`every 15th`.

### What the importer cannot map

| Case | What happens |
|---|---|
| An archived project | Skipped and counted in the summary |
| The completed archive | A full sync does not send it. The summary reports the count that Todoist gives. |
| A project comment | Skipped and counted |
| A repeat rule that does not convert, such as `every 3 hours` or `every 4th thursday of november` | The task still arrives. The original words go into the description. |
| A time inside a repeat rule, such as `every day at 10:00` | The rule becomes `FREQ=DAILY`. The time survives in the due time. |
| Filters, reminders, attachments, assignees and karma | Not read at all |
| A row with no parent row in the payload | It moves to the top level, or to the inbox. The summary counts it. |

### The import is safe to run twice

Every command carries a uuid built from the Todoist id. A project and a label
match by name. A task matches by `source_ref`. A second run therefore writes
nothing, and the database version does not move.

The summary tells you exactly that:

```
Projects: 0 new, 2 already in the database.
Labels: 0 new, 2 already in the database.
Tasks: 0 new, 10 already in the database.
Commands: 0, failed: 0.
```

Run `--dry-run` first, read the summary, then run the real import.

---

## 8. What does not work yet

This build is a proof of concept. The list below comes from
[POC.md](POC.md).

**No widget, and no signature on the macOS app.** The desktop shell ships: a
global quick add panel, a menu bar icon and the `teha://` scheme, around the
same web app. The build is unsigned, so a copy from another machine needs a
right click and **Open** on the first run. The panel shows no parse hint before
you press `Enter`, because the parser is in the page.

**The phone app has gaps of its own.** The Android app ships through Obtainium,
and [android/README.md](../android/README.md) holds the install steps and the
list of open defects. The phone app shows Today and All open, captures with the Quick
Settings tile, edits every field of a task, and acts on a picked set of tasks.
It has no project view, no filter field and no notifications yet.

**One person only.** You cannot share a project. There is no second account,
no assignee, no push notification, no comment and no attachment.

**One token, no passkeys.** One device token guards everything. The browser
keeps it in a cookie. A leaked token needs a new `TEHA_TOKEN` and a restart,
and then every client logs in again.

**No sections, no board, no calendar.** The importer folds a Todoist section
name into the description, because there is no section table.

**The browser stores its state in `localStorage`,** not in OPFS with SQLite.
That is enough for thousands of tasks. It needs a replacement before it holds a
decade of history.

**The backup restore is untested.** Litestream replicates the cluster database
to object storage and reports a matching transaction id. Nobody has rehearsed a
restore from those files yet.

**Start date, deadline and duration have columns but little UI.** The detail
sheet edits the start date and the deadline. The filter language reads them.
Nothing else uses them.

**The browser filter is a subset of the server filter.** Read
[section 2](#the-browser-filter-is-a-subset).

Next, in order: give the phone the project and filter views it still lacks,
rehearse a restore from the Litestream replica, then add passkeys and a second
account.

The real Todoist import already ran, on 2026-08-25: 17 projects, 250 tasks, 63
labels, 0 failed commands, one HTTP request, 34 milliseconds. It also found
seven repeat rules that did not convert, which is why the converter now
normalises the abbreviated `ev` keyword.
