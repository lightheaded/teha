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

## Limits

- The app polls. It calls sync when a screen opens and on a pull-to-refresh. It
  does not read `GET /v1/events`, and it has no background sync and no
  notifications.
- The task list shows two views: Today and All open. There is no project view
  and no filter field yet, although the binding exposes `CompileFilter`.
- A task row supports one action: complete or reopen. There is no edit screen,
  no delete and no subtask view.
- Cleartext HTTP is permitted, because a self-hosted server often runs on a
  private address with no certificate. Use HTTPS on any server that leaves your
  own network.
- The app trusts a user certificate authority. That helps a private CA and it
  also widens the attack surface.
- A schema change destroys the local database and pulls it again. Send the
  outbox before an upgrade that changes the schema.

## Before the first build

The Android workflow at `.github/workflows/android.yml` needs a push with the
`workflow` scope. To unblock it, do these steps in order:

1. Refresh the token scope with `gh auth refresh -h github.com -s workflow`, or
   restore SSH access.
2. Run `git push origin android`. Two commits stay on the branch until the
   scope is correct. They add `.github/workflows/android.yml` and
   `.github/workflows/identity.yml`.
3. Create the upload keystore. Keep it forever, and keep a backup off the
   machine.

   ```sh
   keytool -genkeypair -v -keystore teha-release.jks -alias teha \
     -keyalg RSA -keysize 4096 -validity 10000
   ```

4. Add two repository secrets:

   | Secret | Value |
   |---|---|
   | `TEHA_KEYSTORE_BASE64` | `base64 -i teha-release.jks` |
   | `TEHA_KEYSTORE_PASSWORD` | the store password |

   The workflow uses the store password as the key password. The alias is
   `teha`.

5. Merge the branch into `main`. The release job then builds, signs and
   publishes the first APK.

The release job fails when a secret is absent. That is deliberate. A silent
skip produces a green run that ships nothing.
