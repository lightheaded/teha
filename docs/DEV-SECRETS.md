# Local development secrets

**Every secret this project needs is in one encrypted file on the machine. No
password manager prompt is necessary for any command in this repository.**

If you reach for `op`, stop and read this page. The value is almost certainly
already here, and a prompt interrupts whoever owns the machine.

```sh
scripts/secret --list          # the names
scripts/secret device_token    # one value
```

## Where it lives

```
~/.config/teha/
  age.key            the private key, mode 600. Also in the password manager.
  age.pub            the public recipient. Not secret.
  .sops.yaml         the rule that says which recipient encrypts a *.enc.yaml file
  secrets.enc.yaml   every secret, encrypted
  token              the CLI client's own token file, mode 600
  signing/README.txt the signing key fingerprint. No secret in it.
```

The repository holds none of these files.

| Name | What it is |
|---|---|
| `device_token` | The bearer token for `teha.example`. The same value is in the cluster secret. |
| `todoist_token` | The Todoist API token, for the importer. |
| `keystore_password` | The Android release keystore password. It is also the key password. |
| `keystore_alias` | `teha`. Not a secret, kept here so no command has to guess. |
| `keystore_jks_base64` | The Android release keystore itself, base64 encoded. |

## Read a secret

The helper prints one value to standard output and nothing else:

```sh
export TODOIST_TOKEN="$(scripts/secret todoist_token)"
teha import --token "$(scripts/secret todoist_token)"
```

Never pass a secret as a command argument on a shared machine. An argument is
visible in the process list to every other process. Prefer an environment
variable or a pipe.

To get the keystore back as a real file, for example to check a signature:

```sh
f=$(scripts/secret --file keystore_jks)
...use "$f"...
rm -f "$f"
```

The file is mode 600 in a temporary directory. Remove it when you are done.

`keytool` on macOS is a stub that prints nothing, so a real check needs a
container:

```sh
d=$(mktemp -d)
scripts/secret --file keystore_jks | xargs -I{} cp {} "$d/ks.jks"
scripts/secret keystore_password > "$d/pw"
docker run --rm -v "$d":/w:ro eclipse-temurin:21-jdk-jammy \
  sh -c 'keytool -list -v -keystore /w/ks.jks -storepass "$(cat /w/pw)"'
rm -rf "$d"
```

The password reaches `keytool` from a file inside the container, not as an
argument on the host. An argument would be readable in the host process list.

## Add or change a secret

```sh
cd ~/.config/teha
sops secrets.enc.yaml          # opens the plaintext in $EDITOR, encrypts on save
```

`sops` never writes the plaintext to disk. Do not build the file with a shell
redirect unless you are creating it for the first time.

## What is NOT here, and why

**The age private key.** It decrypts everything else, so a copy of it inside
the encrypted store would be a lock whose key hangs on the lock. It lives in
two places: `~/.config/teha/age.key` on each machine, and the password manager
as the off-machine copy. Losing every copy means losing every secret above.

**The git SSH key and the git signing key.** Both are plain files under
`~/.ssh`, which is how every SSH client expects to find them. Moving them into
SOPS would mean decrypting them to a temporary file for every push.

**The GitHub Actions secrets** `TEHA_KEYSTORE_BASE64` and
`TEHA_KEYSTORE_PASSWORD`. GitHub cannot read a secret back out, so those are
not a backup. They only let CI sign a release.

## Set it up on a new machine

Install the tools:

```sh
brew install sops age        # or: nix profile install nixpkgs#sops nixpkgs#age
```

Make the directory and restore the key from the password manager:

```sh
mkdir -p ~/.config/teha && chmod 700 ~/.config/teha
umask 077
cat > ~/.config/teha/age.key    # paste the key, then press Ctrl-D
age-keygen -y ~/.config/teha/age.key > ~/.config/teha/age.pub
```

`umask 077` before the redirect, not `chmod` after it. A `chmod` afterwards
leaves the key world readable for the moment in between.

Write the encryption rule:

```sh
cat > ~/.config/teha/.sops.yaml <<EOF
creation_rules:
  - path_regex: \.enc\.yaml$
    age: $(cat ~/.config/teha/age.pub)
EOF
```

Copy `secrets.enc.yaml` from another machine. The file is encrypted, so any
channel does: `ssh other 'cat ~/.config/teha/secrets.enc.yaml' >
~/.config/teha/secrets.enc.yaml`, then `chmod 600` it.

Check the result:

```sh
scripts/secret --list
scripts/secret keystore_alias    # prints: teha
```

## The CLI token file

`~/.config/teha/token` is separate on purpose. The `teha` command line client
reads it directly, the way `ssh` reads `~/.ssh/id_rsa`: a mode 600 file in a
mode 700 directory, with no other tool in the path. A released binary must not
depend on `sops`.

It holds the same value as `device_token`, so it can be rebuilt at any time:

```sh
umask 077 && scripts/secret device_token > ~/.config/teha/token
```

## The cluster keeps its own copy

The server in the cluster reads `TEHA_TOKEN` from
`the encrypted store of the cluster` in the private repository, encrypted
to the master age key of that store. Nothing on this page reaches a container image or a
repository.

To read a cluster secret, or to run `kubectl`, use the private repository:

```sh
cd the private repository
sops -d the encrypted store of the cluster
kubectl -n teha get pods
```
