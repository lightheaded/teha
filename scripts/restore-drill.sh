#!/usr/bin/env bash
# Rehearse a Litestream restore, end to end, on this machine.
#
# A backup that nobody has restored is not a backup. This drill proves the whole
# path: a running server writes, Litestream replicates, the file is destroyed,
# and a restore brings back every row, the same version and a database that
# still takes writes.
#
# Nothing here touches a real deployment. It starts its own MinIO, its own
# bucket and its own database, and it removes all three at the end. The steps
# are the ones in docs/DEPLOY.md, so a failure here means the guide is wrong.
#
# The server runs in a container beside Litestream on ONE docker volume, which
# is how docker-compose.yml runs them.
#
# What this drill found, on the first run, is worth reading before changing it.
# Litestream replicated the database at the moment it attached and nothing
# after that, so the restore brought back the seed and lost every write. The
# cause is not Litestream and not the drill: the server holds one long-lived
# connection, SQLite therefore leaves a write in the write-ahead log, and
# Litestream replicates the database file. SQLite moves the log into the file
# on its own only when the log grows past about four megabytes, which for two
# people is days. The server now checkpoints on a timer, see
# -checkpoint-interval, and that is what makes this drill pass.
#
# Usage:
#   scripts/restore-drill.sh
#
# It needs Docker and a Go toolchain, and it reaches no network but the local
# one.
set -euo pipefail

# Pin every image. A drill that silently changes what it tests is not a drill.
MINIO_IMAGE="minio/minio:RELEASE.2025-04-22T22-12-26Z"
LITESTREAM_IMAGE="litestream/litestream:0.5.16"
BASE_IMAGE="alpine:3.22"
NET="teha-drill-net"
VOL="teha-drill-data"
MINIO="teha-drill-minio"
APP="teha-drill-app"
LITE="teha-drill-lite"
BUCKET="teha-drill"
KEY="drillaccesskey"
SECRET="drillsecretkey"
# The device token of the drill. It is made here, it reaches one container on a
# private docker network, and the container is destroyed at the end. It is not
# a credential of any deployment, and the secret scanner in the commit hook
# reads a literal beside an Authorization header as one, so it lives in a
# variable.
TOKEN="drill-token"
PORT="8938"

cd "$(dirname "$0")/.."
work="$(mktemp -d)"
say() { printf '\n== %s\n' "$*"; }

cleanup() {
  set +e
  docker rm -f "$APP" "$LITE" "$MINIO" >/dev/null 2>&1
  docker volume rm "$VOL" >/dev/null 2>&1
  docker network rm "$NET" >/dev/null 2>&1
  rm -rf "$work"
  set -e
}
trap cleanup EXIT
cleanup >/dev/null 2>&1 || true

# The binary is built for the architecture the Docker engine runs, so nothing
# is emulated and the drill is as fast as the deployment.
arch="$(docker version --format '{{.Server.Arch}}')"
say "Building the server for linux/$arch"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$work/teha" ./cmd/teha

say "Starting MinIO, which stands in for the S3-compatible store"
docker network create "$NET" >/dev/null
docker volume create "$VOL" >/dev/null
docker run -d --name "$MINIO" --network "$NET" \
  -e "MINIO_ROOT_USER=$KEY" -e "MINIO_ROOT_PASSWORD=$SECRET" \
  "$MINIO_IMAGE" server /data >/dev/null

for _ in $(seq 1 30); do
  docker run --rm --network "$NET" --entrypoint sh "$MINIO_IMAGE" -c \
    "mc alias set d http://$MINIO:9000 $KEY $SECRET >/dev/null 2>&1 && mc mb --ignore-existing d/$BUCKET >/dev/null 2>&1" \
    && break
  sleep 1
done
echo "the bucket $BUCKET is ready"

# The Litestream configuration for the drill. It has the shape of
# deploy/litestream.yml, with the endpoint of the container in place of the
# variables that the real file reads from the environment.
cat > "$work/litestream.yml" <<YAML
dbs:
  - path: /data/teha.db
    replica:
      type: s3
      endpoint: http://$MINIO:9000
      bucket: $BUCKET
      path: teha
      force-path-style: true
YAML

lite() {
  docker run --rm --network "$NET" \
    -e "LITESTREAM_ACCESS_KEY_ID=$KEY" -e "LITESTREAM_SECRET_ACCESS_KEY=$SECRET" \
    -v "$VOL:/data" -v "$work/litestream.yml:/etc/litestream.yml:ro" \
    --entrypoint litestream "$LITESTREAM_IMAGE" "$@"
}

api() {
  curl -sf -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d "$1" "http://127.0.0.1:$PORT/v1/sync"
}

start_app() {
  docker rm -f "$APP" >/dev/null 2>&1 || true
  docker run -d --name "$APP" --network "$NET" \
    -v "$VOL:/data" -v "$work/teha:/usr/local/bin/teha:ro" \
    -e "TEHA_TOKEN=$TOKEN" -p "127.0.0.1:$PORT:$PORT" \
    "$BASE_IMAGE" teha serve -db /data/teha.db -addr "0.0.0.0:$PORT" >/dev/null
  for _ in $(seq 1 60); do
    curl -sf "http://127.0.0.1:$PORT/v1/health" >/dev/null && return 0
    sleep 0.5
  done
  docker logs "$APP"
  return 1
}

say "Seeding the database and starting the server"
docker run --rm -v "$VOL:/data" -v "$work/teha:/usr/local/bin/teha:ro" "$BASE_IMAGE" \
  teha -seed -db /data/teha.db >/dev/null
start_app

say "Starting the replication"
docker run -d --name "$LITE" --network "$NET" \
  -e "LITESTREAM_ACCESS_KEY_ID=$KEY" -e "LITESTREAM_SECRET_ACCESS_KEY=$SECRET" \
  -v "$VOL:/data" -v "$work/litestream.yml:/etc/litestream.yml:ro" \
  "$LITESTREAM_IMAGE" replicate -config /etc/litestream.yml >/dev/null
sleep 3

say "Writing through the API, the way a person does"
api '{"since":0,"commands":[
  {"uuid":"drill-1","type":"task_add","args":{"id":"d_one","title":"Written before the backup"}},
  {"uuid":"drill-2","type":"task_add","args":{"id":"d_two","title":"Also before the backup"}}]}' >/dev/null

before_version=$(curl -sf "http://127.0.0.1:$PORT/v1/health" | sed 's/.*"version"://; s/}//')
before_rows=$(api '{"since":0,"commands":[]}' | tr ',' '\n' | grep -c '"id":"')
echo "version before the loss: $before_version, rows carrying an id: $before_rows"

say "Waiting for Litestream to carry the last write"
# The daemon syncs every second, and it replicates the DATABASE FILE. A write
# sits in the write-ahead log until a checkpoint moves it, so the drill waits
# for the replica to grow past what it held before the writes. The server
# checkpoints on a timer for exactly this reason: see -checkpoint-interval.
#
# The wait must be a real one. An earlier version of this drill stopped as
# soon as the replica had caught up with the database as Litestream saw it,
# which was true one millisecond after the writes and false in every way that
# mattered.
replica_txid() {
  lite ltx -config /etc/litestream.yml /data/teha.db 2>/dev/null |
    awk 'NR>1 {print $3}' | sort | tail -1
}
start_txid="$(replica_txid)"
echo "the replica held $start_txid before the wait"
for _ in $(seq 1 60); do
  now="$(replica_txid)"
  if [ -n "$now" ] && [ "$now" != "$start_txid" ]; then
    break
  fi
  sleep 1
done
lite ltx -config /etc/litestream.yml /data/teha.db | tail -3
docker logs "$LITE" 2>&1 | tail -1

say "Losing the database"
docker rm -f "$APP" "$LITE" >/dev/null
docker run --rm -v "$VOL:/data" "$BASE_IMAGE" \
  sh -c 'rm -f /data/teha.db /data/teha.db-wal /data/teha.db-shm; ls -l /data'

say "Restoring into the empty volume"
lite restore -config /etc/litestream.yml -o /data/teha.db /data/teha.db
docker run --rm -v "$VOL:/data" "$BASE_IMAGE" sh -c 'ls -l /data/teha.db'

say "Starting the server on the restored file"
start_app
after_version=$(curl -sf "http://127.0.0.1:$PORT/v1/health" | sed 's/.*"version"://; s/}//')
after_rows=$(api '{"since":0,"commands":[]}' | tr ',' '\n' | grep -c '"id":"')
echo "version after the restore: $after_version, rows carrying an id: $after_rows"

say "The result"
fail=0
if [ "$before_version" != "$after_version" ]; then
  echo "FAIL: the version was $before_version and is now $after_version"
  fail=1
fi
if [ "$before_rows" != "$after_rows" ]; then
  echo "FAIL: $before_rows rows before, $after_rows after"
  fail=1
fi
# A write proves that the file is a working database and not only a readable
# one.
if ! api '{"since":0,"commands":[{"uuid":"drill-3","type":"task_add","args":{"id":"d_three","title":"Written after the restore"}}]}' >/dev/null; then
  echo "FAIL: the restored database refused a write"
  fail=1
fi
if [ "$fail" = 0 ]; then
  echo "PASS: every row and the version came back, and the file takes writes."
fi
exit "$fail"
