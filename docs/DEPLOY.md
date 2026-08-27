# Deploy teha

This page tells you how to run the server. The server is one static binary. It
serves the API, the web app and the MCP endpoint from one SQLite file.

## Before you start

You need one of these:

- Docker 24 or later, for a single container.
- Docker Compose v2, for the server and a backup sidecar.
- A Kubernetes cluster with a default storage class, for the cluster example.

The container image holds no shell. The image runs as user 65532. The root
filesystem stays read-only in the cluster example.

## Environment variables

| Name | Default | Function |
|---|---|---|
| `TEHA_ADDR` | `127.0.0.1:8637` | The listen address. The image sets `0.0.0.0:8637`. |
| `TEHA_DB` | `teha.db` | The path to the SQLite file. The image sets `/data/teha.db`. |
| `TEHA_TOKEN` | empty | The device token. An empty value makes the server print a new token at each start. |
| `TEHA_RP_ID` | empty | The WebAuthn relying-party id, a bare domain such as `teha.example`. An empty value reads it from the request host. |
| `TEHA_ORIGIN` | empty | The origin the web app is served from, such as `https://teha.example`. An empty value builds it from the request host. |
| `TEHA_TRUST_FORWARDED` | off | Read the client address from `X-Forwarded-For`. Turn it on only behind a proxy that writes that header. |

Each variable has a flag with the same function: `-addr`, `-db`, `-token`,
`-rp-id`, `-origin` and `-trust-forwarded`. The flag wins over the variable.

Two more flags help during development:

- `-dev` turns off authentication and prints debug logs. Use `-dev` on a
  private machine only.
- `-seed` writes example data into an empty database and exits.

The Litestream sidecar reads more variables. See
[Back up with Litestream](#back-up-with-litestream).

## How the token works

The token guards every route except `/v1/health`. A client sends the token in
one of two ways:

- An API client and an MCP client send the header
  `Authorization: Bearer <token>`.
- A browser sends the token one time to `/login`. The server stores the token
  in the `teha_token` cookie. The cookie is HTTP-only and same-site.

To make a token, run this command:

```sh
openssl rand -hex 32
```

Keep the token in a secret store. If a token leaks, replace `TEHA_TOKEN` and
restart the server. All clients then log in again.

The token is empty in `-dev` mode. Every request is then allowed.

## Set up passkeys

A passkey is a second way into the same account, for the browser. The token
stays, because every native client and the MCP endpoint use it.

**Set two variables on a public hostname.** A passkey is bound to one
relying-party id for the life of the credential, so name it before the owner
enrols one:

```sh
TEHA_RP_ID=teha.example
TEHA_ORIGIN=https://teha.example
```

Both default to the request host, so a run on `localhost` needs neither. Read
these three rules before you choose the values:

- `TEHA_RP_ID` is a bare domain with no scheme and no port. Set it to the
  registrable domain you will keep. A passkey enrolled under one id does not
  work under another, and the owner must enrol again.
- `TEHA_ORIGIN` carries the scheme and the port the browser sees, for example
  `https://teha.example`. It must match the address bar exactly.
- Serve the app over TLS. A browser makes a passkey in a secure context only:
  https on a real name, or http on a loopback name.

**Turn on the forwarded header behind a proxy.**

```sh
TEHA_TRUST_FORWARDED=1
```

The passkey lockout counts failures per client address. Behind a proxy every
request arrives from the proxy, so without this variable one address carries
every failure. With it, the server reads the last entry of `X-Forwarded-For`,
which is the entry the proxy writes and no client can forge. Turn it on only
when a proxy is in front of the server. A client that reaches the server
directly can otherwise write its own address and escape a ban.

**Enrol the first passkey.** Sign in with the device token, open **Settings**
in the app, type a name and press **Add**. Enrolment asks for the token and
nothing else, so nobody without it can add a passkey.

**What an operator must know.**

- A passkey login sets the `teha_session` cookie for 14 days. The cookie is
  Secure, HTTP-only and same-site.
- The sessions and the lockout counters live in memory. A restart signs every
  browser out and clears the counters. A passkey login is one tap.
- Keep the token. A passkey works in a browser only, and a lost passkey is
  recovered by signing in with the token and enrolling a new one.

## Run with Docker

1. Build the image:

   ```sh
   make docker
   ```

   To set the image tag, pass `VERSION`, for example
   `make docker VERSION=0.1.0`.

2. Make a volume for the database and the attachments:

   ```sh
   docker volume create teha-data
   ```

3. Start the container:

   ```sh
   docker run -d --name teha \
     -e TEHA_TOKEN="$(openssl rand -hex 32)" \
     -p 127.0.0.1:8637:8637 \
     -v teha-data:/data \
     teha:dev
   ```

4. Read the health endpoint:

   ```sh
   curl http://127.0.0.1:8637/v1/health
   ```

The image has its own health probe. The probe calls `/v1/health` every 30
seconds. To read the result, run
`docker inspect --format '{{.State.Health.Status}}' teha`.

The container listens on all interfaces inside the container. The example
publishes the port to the loopback address only. A reverse proxy in front of
the container terminates TLS and adds the security headers.

## Run with Docker Compose

`docker-compose.yml` starts two services: the server and a Litestream sidecar.
Both services mount the same data volume.

1. Write the variables into a file with the name `.env` in the repository root:

   ```sh
   TEHA_TOKEN=replace-with-a-random-token
   LITESTREAM_ACCESS_KEY_ID=replace-with-the-access-key
   LITESTREAM_SECRET_ACCESS_KEY=replace-with-the-secret-key
   LITESTREAM_ENDPOINT=https://s3.example.com
   LITESTREAM_BUCKET=example-bucket
   LITESTREAM_PATH=teha
   ```

   `.gitignore` excludes `.env`. Never commit the file.

2. Start both services:

   ```sh
   docker compose up -d
   ```

3. Read the logs of the sidecar:

   ```sh
   docker compose logs -f litestream
   ```

The sidecar starts after the health probe of the server passes.

## Back up with Litestream

Litestream v0.5.16 copies the write-ahead log of the database to
S3-compatible storage. The sidecar reads `deploy/litestream.yml`. That file
holds no endpoint, no bucket name and no credential. Litestream expands each
`${VAR}` from the environment.

| Name | Function |
|---|---|
| `LITESTREAM_ACCESS_KEY_ID` | The access key. Litestream reads this name on its own. |
| `LITESTREAM_SECRET_ACCESS_KEY` | The secret key. Litestream reads this name on its own. |
| `LITESTREAM_ENDPOINT` | The URL of the S3-compatible service. |
| `LITESTREAM_BUCKET` | The bucket name. |
| `LITESTREAM_PATH` | The prefix inside the bucket. |

The 0.5 line replicates one database to one replica. The configuration
therefore holds one `replica` block.

To list the snapshots in the bucket, run this command:

```sh
docker compose run --rm --entrypoint litestream litestream \
  snapshots -config /etc/litestream.yml /data/teha.db
```

## Restore from a Litestream backup

A restore writes a new database file. Never restore over a file that a running
server holds open.

1. Stop the services:

   ```sh
   docker compose down
   ```

2. Restore the latest state:

   ```sh
   docker compose run --rm --entrypoint litestream litestream \
     restore -config /etc/litestream.yml -o /data/teha.db /data/teha.db
   ```

3. For an earlier point in time, add `-timestamp` with an RFC 3339 value:

   ```sh
   docker compose run --rm --entrypoint litestream litestream \
     restore -config /etc/litestream.yml -timestamp 2026-08-25T12:00:00Z \
     -o /data/teha.db /data/teha.db
   ```

4. Start the services again:

   ```sh
   docker compose up -d
   ```

5. Read the health endpoint. If the endpoint answers `200`, the restore is
   complete.

Test a restore into an empty volume at regular intervals. An untested backup
is not a backup.

## Run in a cluster

`deploy/kubernetes/` holds a generic kustomize example. The example has four
parts: a Deployment, a Service, a PersistentVolumeClaim and a Secret
template.

The example sets these controls:

- One replica, because SQLite allows one writer.
- The `Recreate` strategy, so two pods never hold the same file.
- A read-only root filesystem, with an `emptyDir` volume at `/tmp`.
- `runAsNonRoot`, user 65532, and `RuntimeDefault` for seccomp.
- `drop: ALL` for the Linux capabilities.
- A liveness probe and a readiness probe on `/v1/health`.

The example holds no Ingress and no hostname. The Ingress, the IngressRoute
and the TLS reference belong in the private infrastructure repository. Add the
Litestream sidecar in that repository too. The sidecar mounts the same data
volume and reads the same configuration file.

To deploy the example, do these steps:

1. Copy the directory into the private infrastructure repository.
2. Replace `secret.example.yaml` with an encrypted secret. Every value in the
   template is a placeholder.
3. Set the image reference in `kustomization.yaml`.
4. Apply the result:

   ```sh
   kubectl apply -k deploy/kubernetes
   ```

5. Read the pod status:

   ```sh
   kubectl -n teha get pods
   ```

## Update the server

1. Build the new image.
2. Stop the old container or the old pod.
3. Start the new container or the new pod.

The database schema migrates at start. One replica writes at a time, so the
`Recreate` strategy is necessary. A rolling update starts a second pod before
the first pod stops, and the two pods then compete for the same file.

## Local development

To run the server without authentication, use this target:

```sh
make run
```

The target uses `-dev`, the local address `127.0.0.1:8637` and the database
file `./teha.db`. To write example data, run `make seed`.
