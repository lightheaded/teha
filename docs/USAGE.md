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
| `-token` | `TEHA_TOKEN` | empty | The device token of the owner. Every invited person gets one of their own. |
| `-rp-id` | `TEHA_RP_ID` | empty | The WebAuthn relying-party id, a bare domain such as `teha.example`. Empty reads it from the request host. |
| `-origin` | `TEHA_ORIGIN` | empty | The origin the web app is served from, such as `https://teha.example`. Empty builds it from the request host. |
| `-trust-forwarded` | `TEHA_TRUST_FORWARDED` | off | Read the client address from `X-Forwarded-For`. Turn it on only behind a proxy that writes that header. |
| `-dev` | — | off | No token, debug logs. |
| `-seed` | — | off | Write example data and exit. |
| `-version` | — | — | Print `teha 0.1.0 (proof of concept)` and exit. |
| `-vapid-keys` | — | off | Make a Web Push keypair, print it and exit. |
| `-vapid-public` | `TEHA_VAPID_PUBLIC_KEY` | empty | The Web Push public key. |
| — | `TEHA_VAPID_PRIVATE_KEY` | empty | The Web Push private key. A secret, so it has no flag. |
| `-vapid-subject` | `TEHA_VAPID_SUBJECT` | a repository URL | A `mailto:` address or an `https:` URL for the push service. |
| `-push-interval` | — | `30s` | How often the reminder scheduler looks for due reminders. |
| `-checkpoint-interval` | — | `10s` | How often the write-ahead log is written into the database file. A backup replicates the file, so this bounds what a restore can lose. `0` turns it off, and [DEPLOY.md](DEPLOY.md) says not to. |

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
no token, and `/v1/join` takes an invitation code instead.

`TEHA_TOKEN` is the **owner's** token. Everybody the owner invites gets a token
of their own, which the join answer shows once, and a request is answered for
whichever account its token names.

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

### Passkeys

A passkey is a second way into the same account, for the browser. The device
token stays, because the Android app, the command line client and the MCP
endpoint all use it.

**Enrol one.** Sign in with the token, open **Settings** in the header (or press
`,`), type a name and press **Add**. The browser asks for a fingerprint, a face
or a PIN. The list then shows the passkey, its last use and a **Remove** button.

Enrolment asks for the device token and nothing else. A browser that signed in
with a passkey cannot add another one: it must paste the token once more. The
token is therefore the one invitation into the account, which is what keeps
signup invite-only.

**Sign in.** The sign-in page has a **Sign in with a passkey** button. The token
box stays below it. A good assertion sets the `teha_session` cookie: Secure,
HTTP-only, same-site Lax, and it lasts 14 days. **Sign out** in Settings clears
it, and so does `POST /v1/logout`.

**Name the deployment.** A passkey is bound to one relying-party id for life, so
set the id and the origin on a public hostname:

```sh
TEHA_RP_ID=teha.example TEHA_ORIGIN=https://teha.example ./teha -db teha.db
```

Both values default to the request host, so a run on `localhost` needs neither.
The scheme follows the host: https on a real name, http on a loopback name.
A browser only runs a passkey in a secure context, so those are the two cases.

**What the server refuses.** Every one of these answers 401 and writes nothing:

- an assertion from another origin, or from another relying-party id
- a signature counter that does not increase, which is what a replay looks like
- an unknown credential id, or a user handle no account carries
- an assertion that reports no user verification

Repeated failures lock the client address out, and the account has its own
budget as well. The wait doubles with each failure above the allowance, up to
15 minutes. `-trust-forwarded` makes the lockout read the real address behind a
proxy. Without it a client could write its own address and escape the ban.

A restart clears the sessions and the lockout counters, because both live in
memory. A passkey login is one tap, so a restart costs one tap.

### The HTTP routes

| Route | Method | Function |
|---|---|---|
| `/v1/sync` | POST | `{since, commands[]}` in, changed rows out, for the account the token or the session names. At most 200 commands per request, at most 4 MB. The rows are `projects`, `sections`, `labels`, `tasks`, `reminders` and `comments`. The answer also carries `me`, `inbox` and, after a share was taken back, `reset`. |
| `/v1/tasks` | GET | `?filter=`, `?limit=`, `?offset=`. At most 500 rows. |
| `/v1/projects` | GET | Every project. |
| `/v1/sections` | GET | Every section, in the order of its project. |
| `/v1/labels` | GET | Every label. |
| `/v1/export` | GET | The whole account as one JSON file. |
| `/v1/activity` | GET | One page of the activity log, newest first. `?project=`, `?task=`, `?limit=` (50 by default, 100 at most) and `?before=` for the next page. The answer is `{activity[], more}`, and `before` takes the smallest `seq` of the page just read. |
| `/v1/events` | GET | Server-sent events. One `version` event per write, and a ping every 25 seconds. |
| `/v1/push/key` | GET | `{"enabled":true,"key":"...","devices":1}`. The browser subscribes with the key. |
| `/v1/push/subscribe` | POST | The browser posts its own subscription, unchanged. |
| `/v1/push/unsubscribe` | POST | `{"endpoint":"..."}`. The row goes. |
| `/v1/push/test` | POST | Send one notification to every subscribed device. |
| `/v1/health` | GET | `{"ok":true,"version":18}`. No token. |
| `/v1/passkeys/register/begin` | POST | Start an enrolment. Needs the device token. |
| `/v1/passkeys/register/finish` | POST | Finish an enrolment. `?name=` names the passkey. Needs the device token. |
| `/v1/passkeys/login/begin` | POST | Start a login. No token. |
| `/v1/passkeys/login/finish` | POST | Finish a login and set the session cookie. No token. |
| `/v1/passkeys` | GET | Every passkey, without its public key. |
| `/v1/passkeys/{id}` | DELETE | Remove one passkey. |
| `/v1/logout` | POST | Clear the session cookie and the token cookie. |
| `/v1/household` | GET | Who is in the house, which lists are shared, and the id of your own inbox. |
| `/v1/invites` | POST | Write an invitation. The answer carries the code, once. The owner only. |
| `/v1/invites` | GET | Every invitation, with no code. The owner only. |
| `/v1/invites/revoke` | POST | `{"id":"..."}`. Removes an invitation nobody used. The owner only. |
| `/v1/join` | POST | `{"code":"...","name":"..."}`. Makes an account and answers with its device token. No token. |
| `/v1/share` | POST | `{"project_id":"...","account_id":"...","share":true}`. The owner of that list only. |
| `/mcp` | POST | The MCP endpoint. |
| `/login` | GET, POST | The passkey button and the token form. |
| `/` | GET | The web app. |

---

## 2. The web app

Open the server address, for example <http://127.0.0.1:8637>.

### Log in

With a token set, `/` sends the browser to `/login`. The page offers two ways in.

**A passkey.** Press **Sign in with a passkey**. The browser asks for a
fingerprint, a face or a PIN, and the server writes the `teha_session` cookie
for 14 days. The button appears only where the browser can make a passkey.

**The token.** Type the token, then press **Sign in with the token**. The server
writes the `teha_token` cookie. The cookie is HTTP-only, same-site and Secure on
any real hostname. It lasts one year.

A wrong token returns to the form with the message `That token did not match.`

Read [Passkeys](#passkeys) for the enrolment and the rules the server applies.

### The screen

| Part | What it does |
|---|---|
| Sidebar | Six built-in views, then one entry per project. Each entry shows a count. A shared list carries a dot and a `⋯` button. The sidebar appears at a window width of 800 px or more. |
| Header | The view name, the number of tasks, the layout buttons, the filter box and the status. |
| Status | `v18` after a good sync. `3 to send` while the outbox holds commands. `offline · 3 queued` after a failed sync. |
| Quick add box | One line makes one task. Press Enter. |
| Hint line | What the parser found, as you type. |
| Task list | Groups in this order: Overdue, Today, Tomorrow, then one group per date, then No date. |
| Board button | In a project view only. It swaps the list for a board of columns, and back. |
| Shop button | In a project view only. It swaps the list for shopping mode, and back. |
| Calendar button | In every view. It swaps the list for a month or a week, and back. |
| Filter box | Any query the grammar knows. `/` focuses it, Enter runs it. |
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

A project entry in the sidebar runs `#<project name>`. A seventh view,
**Assigned to me**, appears once a second person is in the household.

### The calendar layout

The **Calendar** button, and the `c` key, swap the list for a calendar of the
same view. **Month** and **Week** are the two shapes.

- Drag a task to another day to move it. One Undo puts it back.
- Below the grid is a strip of every task in the view that has no day. Drag one
  onto a day to schedule it, or drag a task onto the strip to take its date off.
- `[` and `]` step to the period before and after. **Today** comes back.
- A task due outside the window is counted in the header, never hidden without
  a word.

A reminder follows its task. Every path that writes a due date re-arms it, the
calendar drag included.

### Markdown in a note

The note field of a task reads as Markdown and edits as plain text. Click it to
edit, press Escape or `⌘`/`Ctrl`+`Enter` to leave it. It reads headings, a
fenced code block, a blockquote, a bullet list, a numbered list, a task list, a
rule, `**bold**`, `*italic*`, `` `code` ``, `~~strike~~`, `[text](url)` and a
bare URL. A task title reads the inline part of that list, so a link in a title
works and opens.

Two rules keep a note safe. The text is escaped before anything else, so no
markup in a note reaches the page. A link target must carry a scheme the app
trusts, so a `javascript:` target stays plain text. An image is drawn as a
link, because the content security policy allows no remote picture.

**Paste a link over selected text and it becomes a link.** Select the words,
paste the URL, and the field holds `[the words](the url)`. It works in the note
and in the title field of the detail sheet. A URL pasted over nothing, or over
a URL, is an ordinary paste.

### The household

The **Settings** panel lists everybody in the house and invites the next one.

1. Type a name and press **Invite**. The server answers with a code, once. Copy
   it and send it.
2. The other person opens the app, sees the sign-in page, types their name and
   the code, and presses **Join**. They get an account of their own, with their
   own inbox and their own device token. On the phone the same two fields are in
   **Settings**, under **The household**, and the app fills in its own token.
3. Share a list with them from the `⋯` button beside it in the sidebar. A tick
   means it is shared. Press it again to take it back.

What each person sees is what they own and what somebody shared with them. An
inbox is never shared. A reminder belongs to the person who set it, even on a
shared task.

In a shared list, the detail sheet grows a **Who** field. `assigned to: me`,
`assigned to: <name>`, `assigned` and `unassigned` are filter terms, and
**Assigned to me** is a view in the sidebar.

Only the owner of a list may rename it, delete it or share it. A member adds
tasks, edits them, ticks them off and assigns them.

An invitation is good for seven days and for one person. **Revoke** takes back
one that nobody used.

A task given to somebody, and a comment on a task they can see, reach them as a
notification. Nobody hears their own action.

### The board layout

A project view has two layouts. The **Board** button in the header, and the `b`
key, swap the list for one column per section. The first column holds the tasks
with no section, so a task is never hidden because nobody filed it. The choice
is saved, so a reload keeps the board.

A board arranges work by hand, so a column is in the manual order, not in date
order. That is the difference between the two layouts.

| To do this | Pointer | Keyboard |
|---|---|---|
| Move a task to another column | Drag the card into the column | `H` and `L` |
| Move a task inside a column | Drag it above or below a card | `J` and `K` |
| Move the cursor between columns | Click a card | `h` and `l`, or the arrow keys |
| Reorder the columns | Drag a column head | `<` and `>` |
| Add a section | Type in the last column and press Enter | `n`, then the name and Enter |
| Rename or delete a section | Click the column name | Tab to the column name, then Enter |

**A deleted section keeps its tasks.** They stay in the project and move to the
first column. **Undo** puts the heading back and files the tasks into it again.

A section belongs to one project. A task therefore carries its project and its
section in one command, so the pair can never disagree.

The list order is the due date, then the priority, then the title. A task with
no date goes last.

### Shopping mode

A project view has a third layout. The **Shop** button in the header, and the
`S` key, draw the list the way a shop needs it: big targets, one heading per
aisle, and the field to add an item at the top.

An aisle is a section of the project, so the headings are the same ones the
board draws as columns. Make them once, on the board or in the detail sheet.

| What it does | How |
|---|---|
| Add an item | Type in the top field and press Enter. The field keeps the caret, so three items are three lines. |
| Put an item in the basket | Tap the circle. |
| Take it back | Tap the circle again, or press `u`. |
| Empty the basket | **Clear** in the basket heading. |
| Add something you buy often | Tap it under **Bought before**. |
| A count | Write `2x milk`. The count draws as a chip. |
| Move an item to another aisle | Open it and use **Section**. |

**The aisle is learned.** A new item goes into the aisle of the newest item of
the same name, so `milk` lands in Dairy from the second time on. An item nobody
has bought before goes under **Anything else**. Moving one teaches the next.

**Bought before** lists what the list has held and does not hold now, newest
first, one row per name.

**The basket** holds what went in on this trip: the last twelve hours, and
whatever **Clear** has not removed. A tick is on the server at once, so the
other person in the shop sees it on their own screen.

The layout holds at half a phone width, because the shop's own app is often
open beside it. The screenshot job fails the build if it scrolls sideways or if
a check target shrinks below a thumb at 320 pixels.

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
date, deadline, priority, project, section, labels and the repeat rule. The
labels field takes a comma separated list, for example `store, call`. The repeat
field takes a raw RRULE string, for example `FREQ=WEEKLY;BYDAY=MO`.

**Section** appears when the project has one. It is the same heading the board
draws as a column, and it is the aisle of shopping mode, so a phone or a
one-handed hand can file a task without dragging it.

**Remind** arms one notification for the task: at the due time, or 10 minutes,
30 minutes, an hour or a day before it. The row needs a due date, and a task
with a date but no time counts as 09:00. A change of the due date or the due
time moves the reminder with it.

To add a sub-task, type a title in the field under **Sub-tasks** and press
Enter.

**Comments** is the talk about the task. Type a line and press Enter. A comment
reads as Markdown, it carries who said it and how long ago, and everybody who
can see the task can read it and add to it. Your own line has a small cross to
remove it, and a click on it opens it for editing. Nobody edits or removes
somebody else's line, here or on the server.

A comment reaches the other person as a notification, and so does a task you
give them. Nothing is sent for your own action.

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
| `b` | Swap the list and the board, in a project view. |
| `c` | Swap the list and the calendar, in any view. |
| `/` | Move the cursor into the filter box. |
| `[`, `]` | On the calendar: the period before and after. |
| `h`, `l`, arrow left, arrow right | On the board: move to the column on the left or the right. |
| `Shift+H`, `Shift+L` | On the board: carry this task one column left or right. |
| `Shift+J`, `Shift+K` | On the board: move this task down or up inside its column. |
| `<`, `>` | On the board: move this column left or right. |
| `n` | On the board: move the cursor into the Add a section field. |
| Command or Control, and click | Pick one task. |
| Shift and click | Pick a run of tasks. |
| `e` or `o` | Open the detail sheet. |
| Backspace, Delete or `#` | Delete the selected task. |
| `u` | Undo the last completion or deletion. |
| `r` | Sync now. |
| `g` | Go to Today. |
| `,` | Open settings and the notification controls. |
| `?` | Show the key list. |
| Escape | Drop the selection, close the sheet, or leave the quick add box. |

### Offline

The browser holds the account in IndexedDB: one object store per table, keyed
by the row id, plus one small record for the sync watermark, the outbox and how
this browser is arranged. A write marks one row and a timer writes the batch, so
nothing on the screen waits for the disk. A browser that refuses IndexedDB, which
is what a private window does, falls back to one `localStorage` string.

A browser that used an older build moves to the new database once, at the next
start. Nothing is lost, the outbox included, and the old key is removed.

A write that the browser refuses is kept and tried again with the next one, and
the status line then reads **not saved here**. The screen and the outbox are
still right, so nothing is lost while the tab is open.

Every edit lands in the local state and in an outbox. The screen updates first.
The outbox drains to `POST /v1/sync` when the network allows it. A retry runs
every two seconds while the outbox holds commands. A command carries a uuid, so
a retry is safe.

The service worker caches `/`, `/app.js`, `/parse.js`, `/passkey.js`, `/md.js`,
`/filter.js`, `/db.js`, `/manifest.webmanifest` and `/icon.svg`. It never caches
`/v1/` or `/mcp`.

| With the server down | State |
|---|---|
| Add a task, with the full quick add syntax | Works |
| Complete, delete, undo | Works |
| Priority keys, the `t`, `m` and `w` keys | Works |
| Reschedule the whole overdue section, and undo it | Works |
| Every bulk action on a picked set, and undo it | Works |
| Every field in the detail sheet | Works |
| The six built-in views, a project view, the board and shopping mode | Works |
| Reading and writing a comment | Works |
| The first load in a new browser | Needs the server one time |
| The login | Needs the server |
| An unusual filter term | See the next table |

### Notifications

The server sends a reminder to this browser with Web Push. The browser must
subscribe once, and the operator must set a VAPID keypair on the server. Read
[DEPLOY.md](DEPLOY.md) for the server side.

To turn notifications on:

1. Press the gear in the header, or press `,`.
2. Press **Turn on notifications**. The browser asks for permission at that
   moment.
3. Press **Send a test**. A notification on the screen is the proof.

The panel says the state of this device in one sentence. If the browser blocked
notifications, allow them in the browser site settings and open the panel
again. A blocked site cannot ask a second time on its own.

Each device subscribes on its own: every browser, and the installed web app on
every phone. **Turn off** removes this device and nothing else.

**Daily digest** arms one notification each morning with what is due that day.
Set a time and press **Turn on**. The digest is an account setting, so it
arrives on every subscribed device.

A click on a notification opens the task. If the app is already open, the
window comes to the front and the detail sheet opens.

Two rules are worth knowing:

- **A reminder arrives once, or not at all.** The server marks it sent before
  it sends, so a restart never sends it twice. A crash at the wrong moment
  loses the notification, and the task stays overdue in Today.
- **A reminder that came due while the server was down arrives late only inside
  a window**: one hour for a task reminder, four hours for a digest. After
  that the server drops it. A notification about 09:00 is noise at 16:00, and
  the overdue task says the same thing better.

[DECISIONS.md](DECISIONS.md) D-010 and D-011 hold the reasons.

### The browser reads the whole grammar

This used to be a list of what the browser could not do. It is not one any
more.

The browser evaluates the filter over its local rows in its own JavaScript, and
it reads every term the server reads, with one exception: the sync payload
carries no creation date, so `created:` fails there with a sentence. The phone
calls the shared Go compiler through the gomobile binding and reads every term
except `created:`, `/Section`, `no section` and the assignee terms, because the
Room database keeps no such columns. A term a client cannot answer says so. It
never quietly answers with the wrong rows.

`parser-fixtures/filter.json` is what holds them together: one small account,
eighty-one queries, and one answer each. The Go test writes the answers by
running the compiled SQL against real SQLite, and the browser test demands the
same ones. See [DECISIONS.md](DECISIONS.md) D-018.

One difference is worth knowing, because it changed. `#Trip` is now an **exact**
project name in the browser, as it always was on the server. Use `#Trip*` for
the prefix. Quick add still resolves `#Trip` to `Trip to Setomaa` by a unique
prefix, which is a different question asked at a different moment.

### The filter field

The header carries a filter box. Press `/` to focus it, type any query, and
press `Enter`. Escape puts the box back to the query of the current view.

A query the store cannot answer replaces the list with the sentence that says
why, so an unreadable filter is never an empty list with no reason.

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

### A reminder

| You type | What it sets |
|---|---|
| `remind me at 8` | A reminder at 08:00 on the due day. |
| `remind me at 7pm` | A reminder at 19:00. |
| `remind me at 9:15` | A reminder at 09:15. |
| `remind me 30 minutes before` | Thirty minutes before the due moment. |
| `remind me 2 hours before` | Two hours before it. |
| `remind me 1 day before` | A day before it. |

`reminder` works in place of `remind me`, and `early` and `ahead` work in place
of `before`.

A reminder needs a moment to hang from. A task with a day and no time counts as
09:00, which is the same rule the detail sheet follows. A line that names a
clock time and no day means the next time that hour comes: today while it is
ahead, and tomorrow once it has passed.

```
Call the bank tomorrow remind me at 8
Leave for the airport friday at 6:30 remind me 45 minutes before
```

The phone parses the words and cannot arm the reminder yet, because the Android
database holds no reminder row.

### What the parser leaves alone

Four kinds of span are protected, and no date, priority, project or label
pattern reads inside them:

| Span | Example |
|---|---|
| A quoted phrase | `Read "tomorrow never dies"` keeps the words and takes no date. |
| A URL | `Check https://example.org/v1.2/notes tomorrow` keeps the `1.2`. |
| A code span | ``Fix `every day` in the docs`` sets no recurrence. |
| A Markdown link | `Read [the plan for tomorrow](https://example.org/p) friday` is due on Friday and keeps its link. |

### A known limit

The parser takes a priority token from inside a sentence:

| Line | Title | Priority |
|---|---|---|
| `Read about p2 engines` | `Read about engines` | 2 |

The corpus records this case. Put such a word in the description instead.

---

## 4. Filters

One filter language runs in the app, on the phone, in `/v1/tasks`, in `teha ls`
and in the MCP tools. The server compiles a query to a SQL `WHERE` clause.

The phone runs the same compiler over its own database, and the browser reads
the same grammar over its local rows, so every term below means the same thing
in all three places. `parser-fixtures/filter.json` is the contract that proves
it: one account, eighty-one queries, one answer each, checked against real
SQLite and against the browser.

Four terms are exceptions, and each one says so rather than answer with the
wrong rows:

| Term | Where it fails | Why |
|---|---|---|
| `created:` | The phone and the browser | Neither keeps a creation date. |
| `/Section`, `no section` | The phone | Room holds no section table. |
| `assigned`, `assigned to:` | The phone | Room holds no assignee and no account table. |

See [the browser section](#the-browser-reads-the-whole-grammar).

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
| `/Name` | The section with that exact name, in any project. |
| `/Name*` | Every section whose name starts with `Name`. |
| `no section` | Tasks in no section. |
| `##Name` | That project and every sub-project under it. |
| `%label`, `@label` | Tasks with that label. |
| `%lab*`, `@lab*` | Tasks with a label that starts with `lab`. |
| `no label`, `no labels` | Tasks with no label. |
| `search: text` | The text in the title or the description. |
| `comment: text`, `note: text` | The text of a comment on the task. |
| a bare word | The same title and description search. |
| `assigned to: me` | Tasks that are yours to do. |
| `assigned to: <name>` | Tasks that are the named person's to do. |
| `assigned to: nobody` | Tasks in a shared list that nobody picked up. |
| `assigned`, `unassigned` | With and without a person on them. |

`search:` and `comment:` read two different things. The full-text index is
written when a task changes, so it holds the title and the description and no
comment text. A comment search reads the comment table.

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
with mode 600. See [DECISIONS.md](DECISIONS.md) D-015.

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

### The ten tools

| Tool | Arguments | Answer |
|---|---|---|
| `list_tasks` | `filter`, `fields` (only `description`), `limit` (default 50, maximum 200), `cursor` | `{"t":[...],"n":3}`, plus `"next"` when a page follows |
| `add_tasks` | `tasks[]`: `title`, `project`, `due`, `time`, `priority`, `labels`, `repeat`, `description`, `start_date`, `deadline`, `parent_id` | `{"ok":2,"ids":[...],"errors":[...],"v":25}` |
| `update_tasks` | `tasks[]`: `id`, plus any field above, plus `clear[]` | The same write shape |
| `complete_tasks` | `ids[]`, `wont_do` | The same write shape |
| `list_projects` | none | `{"projects":[{"id":"p_home","name":"Home","open":3,"sec":[{"id":"x_plan","n":"Plan"}]}]}` |
| `add_project` | `name`, `color` | The same write shape |
| `comments` | `task_id` | `{"task":"t1","c":[{"id":"cm1","wh":"Partner","at":"...","body":"..."}],"n":1}` |
| `add_comment` | `task_id`, `body` | The same write shape |
| `search` | `text`, `limit` (default 50) | `{"t":[...],"n":1}` |
| `plan_day` | none | Overdue, due today, and the undated pile by project |

`clear[]` accepts ten field names: `due_date`, `due_time`, `due_tz`, `rrule`,
`start_date`, `deadline`, `duration_min`, `parent_id`, `section_id` and
`assignee_id`. Any other name fails.

`search` reads the full-text index, which holds titles and descriptions. To
find a task by what somebody said about it, use the filter term `comment:` in
`list_tasks`.

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
| `sec` | The sections of a project, in `list_projects`. `n` is the name. |

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
| A section | A section row in the same project |
| A task in a section | `section_id` |
| A task comment | A comment row on the task, with the original posting time |
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
| A section in an archived project | Skipped and counted. Its tasks reach the inbox with no section. |
| The folded text of an earlier import | It stays. Read the paragraph below. |
| A comment on a task an earlier run wrote | Skipped. The task matches by `source_ref`, so the run keeps the row it has, and the comment is already in that row's description. |
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

### Text that an earlier import folded into a description

Before the section table existed, the importer wrote the section name as the
first line of the description, as `Section: Errands`. Before the comment table
existed it wrote every comment there too, under a `Comments:` heading. **A
re-import does not clean either.** The task matches by `source_ref`, so the
importer keeps the row it already has and writes nothing.

There is no migration for it, and there will not be one: a migration would have
to guess which lines of a description are folded text and which are a note that
a person wrote. Edit the description of those tasks, or delete the account file
and import again from Todoist.

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
list of open defects. The phone app captures with the Quick Settings tile, edits
every field of a task, acts on a picked set of tasks, and reaches the six views
of the browser, one view per project and any filter you type. It has no
notification and no background sync.

**The phone is in the household since 2026-09-02.** Settings takes an
invitation code and the phone becomes a second account with its own inbox and
its own device token. A task carries an assignee and a section, "Assigned to
me" appears in the drawer once two people share a list, and the app acts on the
`reset` field, so a phone whose shared list was taken back drops its copy. The
phone still has no comments and no shopping layout.

**No attachment.** A comment is a row of its own, with an author, and the
`comment:` term reads it. A file has nowhere to go: no part of this build stores
one.

**The full-text index holds no comment text.** `search:` reads the title and the
description, and `comment:` reads the comment table. The two terms therefore
mean two different things, and a comment search is a LIKE rather than an index.

**Shopping mode is in the browser and not on the phone.** The phone holds the
list, the sections and the assignee, and it draws them as a list.

**The calendar has no hour grid.** The week view is seven day columns, so a
task with a time sorts to the top of its day rather than sitting at its hour.

**The browser holds no query engine.** The rows are in IndexedDB and the filter
grammar is evaluated over them in JavaScript, held to the server by a shared
corpus. A query that the server answers with an index is a walk over the rows
here.

**The restore is rehearsed on a laptop, not against the real bucket.**
`scripts/restore-drill.sh` runs the whole path against its own MinIO in Docker
and it passes. Nobody has restored from the bucket the deployment writes to,
which is the run that also proves the credentials.

**Start date, deadline and duration have columns but little UI.** The detail
sheet edits the start date and the deadline. The filter language reads them.
Nothing else uses them.

**`created:` fails in the browser,** because the sync payload carries no
creation date. Every other term of the grammar answers the same everywhere.

Next, in order: an hour grid on the week calendar, attachments, and the phone
reaching what the browser reaches. [BACKLOG.md](BACKLOG.md) holds every
knowingly unfinished thing with its reason.

The real Todoist import already ran, on 2026-08-25: 17 projects, 250 tasks, 63
labels, 0 failed commands, one HTTP request, 34 milliseconds. It also found
seven repeat rules that did not convert, which is why the converter now
normalises the abbreviated `ev` keyword.
