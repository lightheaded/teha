# teha for Android

A client for a self-hosted teha server. It keeps a local copy of the tasks, it
captures a task with no network, and it puts a quick add field in the Quick
Settings panel.

The app is Apache-2.0. See [LICENSE](LICENSE) and
[../LICENSING.md](../LICENSING.md).

## Install

The app ships through [Obtainium](https://github.com/ImranR98/Obtainium). There
is no Play Store listing.

1. Install Obtainium.
2. Add an app in Obtainium. Use the plain repository URL
   `https://github.com/lightheaded/teha`.
3. Accept the defaults. No version extraction, no APK filter and no release
   title filter are necessary. The tag name is the version, so Obtainium reads
   it without help.
4. To install an update without a prompt, pair Shizuku once.

Notes on the mechanism:

- Every GitHub Release in this repository is an Android release. The server
  ships as a container image and never gets a GitHub Release. The "latest"
  pointer therefore always aims at an APK.
- The APK asset name is always `teha-latest.apk`. The permanent link is
  `https://github.com/lightheaded/teha/releases/latest/download/teha-latest.apk`.
- The repository must stay public. Obtainium cannot track a private repository.
  The personal access token field raises the rate limit and gives no private
  access.
- The signing key must never change. A new key breaks the in-place upgrade for
  every install, and each user must then remove the app and install it again.

## First run

1. Open the app.
2. Open Settings.
3. Write the server address, for example `https://tasks.example.org`.
4. Write the device token.
5. Touch **Test connection**. The test calls `GET /v1/health` and then
   `POST /v1/sync` with the token.
6. Touch **Save**.

To add the tile, open the Quick Settings panel, edit the tiles, and drag
**teha add** into the panel.

## Reach a view, a project or any filter

The menu button in the top bar opens the list of views. A swipe from the left
edge does the same. The top bar then names the view on screen.

| Row | Shows |
|---|---|
| Today | Every task due today or earlier |
| Overdue | Every task with a day before today |
| Next 7 days | Every task due inside the week |
| Inbox | The inbox project |
| No date | Every open task with no day |
| Priority 1 | Every p1 task |
| All open | Every open task |
| One row per project | That project, by name |

The first six rows are the six views of the browser, and they carry the same
queries. Under them is one row per project.

The field above the list takes any query of the filter language: `overdue &
#Home`, `%errand & no date`, `search: ferry`. An empty field means every open
task. The phone calls the same Go compiler that the server runs, through the
gomobile binding, so it reads every term the server reads. A query the compiler
refuses keeps the list open and shows the reason under the field, and the reason
names the position that failed.

One term does not work here: `created:`. The local database keeps no creation
date, so the phone says so instead of showing the wrong rows.

Every view sorts the same way. A task with no day goes last, the oldest day
comes first, and priority breaks the tie. An overdue task therefore sits on top
of the Today list.

## Move every overdue task

When the list holds a late task, a bar appears above it and says how many are
late. Touch **Reschedule** and pick a day: Today, Tomorrow, This weekend, Next
week, No date, and each choice shows the day it means.

The bar acts on the list on screen, so it never touches a task the list hides. A
task keeps its time of day, and a repeating task keeps its rule. The snackbar
then offers **Undo**, which puts every task back on the day it had.

## Change a task

Touch a row anywhere except the checkbox. A sheet opens that edits every field
the task has:

| Field | How it works |
|---|---|
| Title, Notes | Free text. It saves half a second after the typing stops. |
| Due | A menu of five presets, and a calendar under **Pick a day…**. |
| Time | Only shown when the task has a day. **No time** takes it off. |
| Priority | p1 to p4. p4 is the default and shows no mark. |
| Project | One entry per project. The inbox reads as Inbox. |
| Section | The headings of that project. It is hidden when the project has none. |
| Who | Everybody the household holds, plus **Nobody**. It is hidden while the household holds one person. |
| Labels | Every label the account has, as chips, plus a field for a new one. |
| Repeat | Four presets and a raw RRULE field. The shared Go engine judges the rule. |
| Starts, Deadline | The same day menu as Due. |
| Sub-tasks | A checkbox each, and a field to add one. A finished child stays, struck through. |

The sheet reads the row from the database, so a sync that lands while it is open
redraws it. Every change goes to the local row and the outbox at once. One sync
goes out when the sheet closes, so every change made while it was open travels
in one request.

**Delete** hides the task and the snackbar offers **Undo**.

## Act on many tasks at once

Long press a row. The row is picked, and the top bar says how many are picked.
A tap then adds another row to the set instead of opening it. Back, or the X in
the top bar, drops the set.

The bar at the bottom replaces the quick add field while a set is picked. It
holds Schedule, Priority, Move, Complete and Delete. Each action drops the set
and the snackbar offers **Undo**.

While a set is picked the checkbox shows membership, not completion. Two
meanings for one control in one moment is how a person completes a task they
meant to select.

## Build

You need these tools:

| Tool | Version |
|---|---|
| JDK | 21 |
| Android SDK platform | 36 |
| Android SDK build tools | 36.0.0 |
| Android NDK | 27.2.12479018 |
| Go | the version in `go.mod` |
| gomobile | the current release |

Gradle needs no separate install. The wrapper in this directory downloads it.

### Step 1 — build the binding

The app does not hold a Kotlin parser. It calls the Go parser through a
gomobile binding. D-002 in [../docs/DECISIONS.md](../docs/DECISIONS.md) gives
the reason.

Run these commands in the repository root:

```sh
go get golang.org/x/mobile/bind
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
mkdir -p android/app/libs
gomobile bind -target=android -androidapi 26 \
  -javapkg io.github.lightheaded \
  -o android/app/libs/teha.aar ./mobile
```

The result is `android/app/libs/teha.aar`. The file is a build output and stays
out of the repository. `go get` adds `golang.org/x/mobile` to `go.mod`. Do not
commit that change, because `go mod tidy` removes it again.

### Step 2 — build the app

```sh
cd android
./gradlew :app:assembleDebug
```

A release build with no keystore produces an unsigned APK. Only a CI run
produces an installable release file.

### Step 3 — run the shared corpus

`parser-fixtures/quickadd.json` is the contract for every teha parser. The Go
test and this test read the same file.

```sh
cd android
./gradlew :app:connectedDebugAndroidTest
```

The test is instrumented and needs a device or an emulator. The binding carries
an Android native library, so a plain JVM test cannot load it.

## Architecture

Room holds the tasks, the projects and the labels. Those three tables are a
cache of the server command log, and a sync rebuilds them. A fourth table is
the outbox. It holds each command that the server has not confirmed, and it is
the only client state that the server cannot rebuild. Every write lands in Room
first and in the outbox second, so a capture works with no network. `POST
/v1/sync` then sends the outbox and reads every row above the local version in
one round trip. A repeated command uuid is a no-op on the server, so a retry is
safe. The user interface is Jetpack Compose with Material 3, and it reads Room
through Kotlin Flow. The quick add field calls the gomobile binding on each
keystroke and shows a chip for each field that the parser found.

A view is a filter string. `Mobile.compileFilterRoom` turns it into a `WHERE`
clause with the names of the Room tables, and a `@RawQuery` DAO method runs that
clause and binds its arguments in order. The compiler is the one the server
runs: it takes the table and column names as a value, so one grammar reads two
databases. D-014 in [../docs/DECISIONS.md](../docs/DECISIONS.md) gives the
reason, and `filter/schema.go` holds the mapping.

## The household

The phone joins a household since 2026-09-02.

1. The owner writes an invitation in the browser and sends the code.
2. Open **Settings**, put the server address in, then type your name and the
   code under **The household** and touch **Join the household**.
3. The server answers with a device token of your own, and the app keeps it in
   the encrypted store. The local cache is dropped first, because what it holds
   belongs to whoever the phone was before, and an unsent command goes with it:
   a capture written by the old account would land in the new account's inbox.
   Send what is queued before you join.

After that the phone is that account: its own inbox, its own reminders, and
only the lists somebody shared with it. **Settings** names everybody in the
house.

A task in a shared list carries **Who**, and **Assigned to me** appears in the
drawer once the household holds two people. A sync answer that says `reset`,
which happens when a list stops being shared, empties the cache and pulls
again, so a list that was taken back leaves the phone.

## Limits

- The app polls. It calls sync when a screen opens and on a pull-to-refresh. It
  does not read `GET /v1/events`, and it has no background sync and no
  notifications.
- The list carries no drag to reorder. The server holds an order key per task,
  and the phone reads it and never writes it. The browser cannot reorder either.
- A comment does not exist on the phone. It is a row on the server since
  2026-09-02, and `comment: words` therefore fails here with a sentence rather
  than searching the description. It needs a Room entity, the rows in the sync
  mapping, and a place in the detail sheet.
- Shopping mode does not exist on the phone. It is a layout of a project view
  in the browser, and the phone draws the list.
- The phone cannot write an invitation and cannot share a list. Both are the
  owner's jobs and both are in the browser.
- An attachment exists nowhere in this build. A notification does not exist on
  the phone alone: the transport is Web Push.
- Cleartext HTTP is permitted, because a self-hosted server often runs on a
  private address with no certificate. Use HTTPS on any server that leaves your
  own network.
- The app trusts a user certificate authority. That helps a private CA and it
  also widens the attack surface.
- A schema change costs one full pull. It is a Room migration now and not a
  destructive one, so the outbox survives an upgrade: it is the one table the
  server cannot rebuild. The destructive fallback stays for a version nobody
  wrote a step for, such as a downgrade.
- Nothing tests the migration itself. Room compares the migrated file with what
  it generates for the entities on every open, so a statement that disagrees is
  an exception at the first start after an upgrade. The statements of version 2
  were read out of the compiled APK on 2026-09-02 and they match character for
  character: `docs/BACKLOG.md` holds the command and what the real test needs.

## Known defects

A review on 2026-08-26 read every source against the Go server contract. It
found nine defects. Seven are fixed. These four are open, and none of them loses
data.

| # | Defect | Effect |
|---|---|---|
| 1 | A capture from the tile starts a sync that `finish()` then cancels | The capture waits for the next app launch. The uuid dedupe covers the resend, so nothing is lost. The fix needs an application-scoped coroutine. |
| 2 | The encrypted preference file and the database open on the main thread | Keystore work and file input happen before the first frame. StrictMode reports it, and a slow device can show an ANR. |
| 3 | A label whose name holds a comma splits into two labels on the phone | Display, and a filter. `%first` finds a task whose label is `first, second`. The quick add parser cannot make such a name, but the MCP server and the importer can. No client can name it in a filter, because a comma is the OR operator. |
| 4 | A 401 shows the same message as a network failure | Nothing points the user at the settings screen to fix the token. |


Read the fix commit for each closed defect, because each one names the failure
it prevents. Defect 5, the outbox order, was closed on 2026-08-26: the query now
reads `ORDER BY createdAt ASC, uuid ASC`. A bulk reschedule writes every command
inside one millisecond, so the stamp alone stopped being enough. The uuid comes
from the shared `id` package, which counts within a millisecond, so it sorts in
creation order and settles the tie at no cost.

## How the release pipeline was set up

These steps ran once, on 2026-08-26. They are here for the day a key or a
runner has to be rebuilt, not as work to do.

1. Push `.github/workflows/**` over SSH, or refresh the token scope with
   `gh auth refresh -h github.com -s workflow`. An HTTPS push of a workflow file
   fails without that scope.
2. Create the upload keystore. Keep it forever, and keep a backup off the
   machine.

   ```sh
   keytool -genkeypair -v -keystore teha-release.jks -alias teha \
     -keyalg RSA -keysize 4096 -validity 10000
   ```

3. Add two repository secrets:

   | Secret | Value |
   |---|---|
   | `TEHA_KEYSTORE_BASE64` | `base64 -i teha-release.jks` |
   | `TEHA_KEYSTORE_PASSWORD` | the store password |

   The workflow uses the store password as the key password. The alias is
   `teha`.

4. Merge the branch into `main`. The release job then builds, signs and
   publishes the APK.

The release job fails when a secret is absent. That is deliberate. A silent
skip produces a green run that ships nothing.
