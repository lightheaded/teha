#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title Add task
# @raycast.mode compact

# Optional parameters:
# @raycast.icon ✅
# @raycast.packageName teha
# @raycast.argument1 { "type": "text", "placeholder": "Buy milk tomorrow p1 #Home @call", "percentEncoded": false }

# Documentation:
# @raycast.description Capture one task in teha. The line takes today, tomorrow, friday, 24.12, at 9:30, p1, #Project, @label and "every monday".
# @raycast.author lightheaded
# @raycast.authorURL https://github.com/lightheaded/teha

# Copy this file into a Raycast script directory (Raycast: Extensions ->
# Scripts -> Add Script Directory), then run "Add task".
set -u

TEHA=${TEHA_BIN:-/usr/local/bin/teha}
export TEHA_SERVER=${TEHA_SERVER:-http://127.0.0.1:8637}

if [ ! -x "$TEHA" ]; then
  TEHA=$(command -v teha) || { echo "teha is not installed"; exit 1; }
fi

exec "$TEHA" add "$1"
