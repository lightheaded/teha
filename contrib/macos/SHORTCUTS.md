# A global hotkey on macOS

The goal is one key press, one line of text, one task. Two recipes reach it.
Recipe A runs the `teha` binary and gives you the full quick add syntax.
Recipe B talks to the server over HTTP and needs no binary, so the same
shortcut also works on an iPhone or an iPad.

Every example uses the default address `http://127.0.0.1:8637`. Change the
address if your server runs on another machine.

## Recipe A: Apple Shortcuts runs the binary

1. Install the client. See `docs/CLIENTS.md`.
2. Open the Shortcuts app and make a new shortcut. Name it `Add task`.
3. Add the action **Ask for Input**.
   - Input type: `Text`
   - Prompt: `New task`
4. Add the action **Run Shell Script**.
   - Shell: `/bin/bash`
   - Input: `Provided Input`
   - Pass input: `as arguments`
   - Script:

     ```bash
     export TEHA_SERVER=http://127.0.0.1:8637
     /usr/local/bin/teha add "$1"
     ```

5. Add the action **Show Notification** and give it the output of the script.
   The notification confirms what the parser understood.
6. Open the shortcut details on the right. Select **Add Keyboard Shortcut** and
   press your chord, for example Control-Option-Space.

The hotkey now works in every application. A shell script from Shortcuts starts
with a short `PATH` and without your shell profile, so give the binary an
absolute path and set `TEHA_SERVER` in the script. The client reads the token
from `~/.config/teha/token`, so the token stays out of the shortcut.

The script `teha-quickadd.sh` in this directory does the same with its own
dialog. Use it when your launcher runs a script but has no input action.

## Recipe B: a URL action, with no binary

The server takes one command per task. The URL action sends it directly.

1. Add the action **Ask for Input** and name the result `Title`.
2. Add the action **Get Contents of URL**.
   - URL: `http://127.0.0.1:8637/v1/sync`
   - Method: `POST`
   - Headers:
     - `Authorization`: `Bearer YOUR_TOKEN`
     - `Content-Type`: `application/json`
   - Request Body: `JSON`, with this shape:

     ```json
     {
       "since": 4611686018427387904,
       "commands": [
         {
           "uuid": "sc-<Unique>",
           "type": "task_add",
           "args": { "id": "t_<Unique>", "title": "<Title>", "due_date": "2026-09-01" }
         }
       ]
     }
     ```

3. Put your `Title` variable in the `title` field.
4. Make the `Unique` value. Shortcuts has no UUID action, so combine two
   actions: **Current Date** formatted as `yyyyMMddHHmmss`, then **Random
   Number** between 100000 and 999999. Join the two. The `uuid` field makes a
   retry safe, so a value that repeats drops the second task.
5. Give the shortcut a keyboard shortcut, as in step 6 of recipe A.

Two limits apply to this recipe:

- The server stores what you send. It runs no quick add parser, so `tomorrow`
  and `p1` stay inside the title. Send `due_date`, `due_time`, `priority`,
  `project` and `labels` as fields.
- The high `since` value asks for no data back, which keeps the answer small.
  Send `"since": 0` when you want the whole account in the answer.

## Keep the token safe

A shortcut with a token in a header syncs to iCloud. Do not share that
shortcut, and do not put the token in a shortcut that other people can open.
Recipe A holds no token at all, so prefer recipe A on a Mac.
