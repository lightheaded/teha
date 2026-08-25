# Local development secrets

The importer needs a Todoist API token. A token in a plain file is a token that
leaks, so it lives encrypted with [SOPS](https://getsops.io) and
[age](https://age-encryption.org), and the age private key lives in a password
manager.

**This applies to local development only.** The server in a cluster reads its
own secret from the cluster, not from this file. Nothing here belongs in a
container image or in a repository.

## What the setup looks like

```
~/.config/teha/
  age.key            the private key, mode 600, also stored in the password manager
  age.pub            the public recipient, not secret
  .sops.yaml         the rule that says which recipient encrypts a *.enc.yaml file
  todoist.enc.yaml   the token, encrypted
```

The repository never holds any of these files.

## Set it up once

Install the tools:

```sh
brew install sops age
```

Make the directory and the key:

```sh
mkdir -p ~/.config/teha && chmod 700 ~/.config/teha
cd ~/.config/teha
age-keygen -o age.key && chmod 600 age.key
age-keygen -y age.key > age.pub
```

Write the rule that tells SOPS which recipient to use:

```sh
cat > ~/.config/teha/.sops.yaml <<EOF
creation_rules:
  - path_regex: \.enc\.yaml$
    age: $(cat ~/.config/teha/age.pub)
EOF
```

Put the token into an encrypted file. The plain value never reaches the shell
history, because it arrives from a file that is deleted afterward:

```sh
printf 'todoist_token: %s\n' "$(tr -d '\n' < /path/to/token)" > ~/.config/teha/todoist.enc.yaml
chmod 600 ~/.config/teha/todoist.enc.yaml
cd ~/.config/teha && SOPS_AGE_KEY_FILE=$PWD/age.key sops -e -i todoist.enc.yaml
shred -u /path/to/token
```

Store the private key in the password manager, so a new machine needs no copy
over the network:

```sh
op document create ~/.config/teha/age.key \
  --title "<your item title>" --vault "<your vault>" --file-name teha-age.key
```

## Use it

Read the token straight into the command that needs it. The value stays in a
pipe, so no file and no shell history holds it:

```sh
cd ~/.config/teha
export SOPS_AGE_KEY_FILE=$HOME/.config/teha/age.key

teha import --db ~/teha.db \
  --token "$(sops -d todoist.enc.yaml | sed -n 's/^todoist_token: //p')"
```

A shell function makes this a habit rather than a lookup:

```sh
# in ~/.zshrc
teha-token() {
  SOPS_AGE_KEY_FILE=$HOME/.config/teha/age.key \
    sops -d "$HOME/.config/teha/todoist.enc.yaml" | sed -n 's/^todoist_token: //p'
}
# then: teha import --db ~/teha.db --token "$(teha-token)"
```

## On a new machine

```sh
mkdir -p ~/.config/teha && chmod 700 ~/.config/teha
op document get "<your item title>" --vault "<your vault>" > ~/.config/teha/age.key
chmod 600 ~/.config/teha/age.key
age-keygen -y ~/.config/teha/age.key > ~/.config/teha/age.pub
# copy .sops.yaml and todoist.enc.yaml across, or run the setup again
```

## Rules

- Never commit `age.key`, `age.pub`, `todoist.enc.yaml` or any decrypted value.
- Never pass a token as a command line argument in a script that others read,
  and never echo one into a log.
- Rotate the Todoist token in the Todoist settings when a machine is lost. Then
  repeat the encryption step above.
- The teha device token is a different secret. It is printed once at the first
  server start, and it belongs in the password manager as well.
