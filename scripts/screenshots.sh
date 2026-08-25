#!/usr/bin/env bash
# Capture the README screenshots, reproducibly.
#
# The browser runs inside one pinned container, so a laptop and a CI runner
# produce identical bytes. A difference therefore means that the app changed,
# which is the only signal worth failing a build over. A screenshot check that
# fails because a font rendered differently teaches everyone to ignore it.
#
# The server binary is cross-compiled on the host instead of inside the
# container. It is static and CGO-free, so the bytes are the same either way,
# and it saves downloading a Go toolchain on every run.
#
# Usage:
#   scripts/screenshots.sh            write docs/screenshots/*.png
#   scripts/screenshots.sh --check    fail if the committed images are stale
#
# Refresh them whenever the web app changes, and commit the result.
set -euo pipefail

# Pin all three. Bumping any one of them rewrites every image, so do that on its
# own commit and say so in the message.
IMAGE="mcr.microsoft.com/playwright:v1.62.1-noble"
PLAYWRIGHT_VERSION="1.62.1"
SEED_DATE="2026-08-25"

cd "$(dirname "$0")/.."
mode="${1:-write}"

out="docs/screenshots"
tmp=""
if [ "$mode" = "--check" ]; then
  tmp="$(mktemp -d)"
  out=".screenshots-check"
  mkdir -p "$out"
fi

echo "Building the server for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o .screenshots-teha ./cmd/teha
# .screenshots-check is NOT removed here. On a failure CI uploads it as an
# artifact, so that the author can see and copy what the app renders now.
trap 'rm -rf .screenshots-teha "${tmp:-/nonexistent}"' EXIT

# linux/amd64 explicitly: the image has no arm64 browser build, and an emulated
# run renders text differently, which is exactly the drift this job avoids.
docker run --rm --platform linux/amd64 \
  -v "$PWD:/w" -w /w \
  -e SEED_DATE="$SEED_DATE" \
  -e OUT_DIR="/w/$out" \
  -e PLAYWRIGHT_VERSION="$PLAYWRIGHT_VERSION" \
  "$IMAGE" bash -eu -c '
    db=$(mktemp -d)/shots.db
    ./.screenshots-teha -seed -seed-date "$SEED_DATE" -db "$db" >/dev/null
    # -dev turns authentication off, so the browser needs no token. The server
    # listens inside this container only.
    ./.screenshots-teha serve -db "$db" -addr 127.0.0.1:8837 -dev >/tmp/server.log 2>&1 &
    for _ in $(seq 1 60); do
      curl -sf http://127.0.0.1:8837/v1/health >/dev/null && break
      sleep 0.5
    done
    curl -sf http://127.0.0.1:8837/v1/health >/dev/null || { cat /tmp/server.log; exit 1; }

    # The image carries the browsers, not the npm package. Install the matching
    # version into a scratch directory; PLAYWRIGHT_BROWSERS_PATH already points
    # at the browsers baked into the image, so nothing is downloaded twice.
    mkdir -p /tmp/pw && cd /tmp/pw
    npm init -y >/dev/null 2>&1
    npm install --no-audit --no-fund --silent "playwright@$PLAYWRIGHT_VERSION"
    cd /w
    PW_MODULE=/tmp/pw/node_modules/playwright/index.mjs \
    TEHA_URL=http://127.0.0.1:8837 \
      node scripts/screenshots.mjs
  '

if [ "$mode" = "--check" ]; then
  if diff -rq docs/screenshots "$out" >/dev/null 2>&1; then
    echo "The screenshots match the app."
    rm -rf "$out"
  else
    echo "The screenshots are stale. The app changed and the images did not." >&2
    diff -rq docs/screenshots "$out" >&2 || true
    echo >&2
    echo "Refresh them and commit the result:" >&2
    echo "    scripts/screenshots.sh" >&2
    exit 1
  fi
fi
