#!/bin/bash
# Quick add for macOS: one dialog, one task, no window to find.
#
# Bind this script to a hotkey with Apple Shortcuts (see SHORTCUTS.md), with
# Raycast (see add-task.sh) or with any launcher that runs a shell script.
#
# The dialog comes from osascript, so it appears over the front application and
# the keyboard goes straight into it. A cancel writes nothing.
set -u

# TEHA_BIN lets a person keep the binary outside the PATH of a launcher. A
# launcher often starts with a short PATH, so the default is an absolute path.
TEHA=${TEHA_BIN:-/usr/local/bin/teha}
export TEHA_SERVER=${TEHA_SERVER:-http://127.0.0.1:8637}

if [ ! -x "$TEHA" ]; then
  TEHA=$(command -v teha) || {
    osascript -e 'display alert "teha is not installed" message "Put the teha binary in /usr/local/bin, or set TEHA_BIN."'
    exit 1
  }
fi

# notify shows a line without a click. The text goes in as an argument, so a
# task title with a quotation mark cannot break the AppleScript.
notify() {
  osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "teha"' -e 'end run' "$1" >/dev/null 2>&1
}

line=$(osascript \
  -e 'display dialog "New task" with title "teha" default answer "" buttons {"Cancel", "Add"} default button "Add"' \
  -e 'text returned of result' 2>/dev/null)

# A cancel or an empty line is not a failure: the person changed their mind.
if [ -z "${line// /}" ]; then
  exit 0
fi

if out=$("$TEHA" add "$line" 2>&1); then
  notify "$out"
  exit 0
fi
notify "$out"
exit 1
