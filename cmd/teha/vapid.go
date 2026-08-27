// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// printVAPIDKeys makes one keypair and prints it.
//
// The public key is not a secret. It goes into the deployment as
// TEHA_VAPID_PUBLIC_KEY, and the server hands it to every browser.
//
// The private key IS a secret. It signs the request to the push service, so
// whoever holds it can push to every subscribed device of this account. It goes
// into the encrypted store described in docs/DEV-SECRETS.md and reaches the
// server only as TEHA_VAPID_PRIVATE_KEY. It must never enter this repository,
// a container image, a log line or a shell history file.
//
// The private key prints once, here, because a keypair that nobody can read is
// of no use. Whoever runs this command puts it straight into the store.
func printVAPIDKeys() int {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "teha: cannot make a VAPID keypair: %v\n", err)
		return 1
	}
	fmt.Printf(`A new VAPID keypair. Both keys are base64url, with no padding.

TEHA_VAPID_PUBLIC_KEY=%s

  Not a secret. Put it in the deployment beside TEHA_ADDR and TEHA_DB. The
  server hands it to every browser that subscribes.

TEHA_VAPID_PRIVATE_KEY=%s

  A SECRET. Whoever holds it can push to every device of this account. Put it
  in the encrypted store now, and let it reach the server only through the
  environment:

    sops ~/.config/teha/secrets.enc.yaml     # add vapid_private_key

  Never commit it, never paste it into a chat, and never pass it as a command
  argument: an argument is visible in the process list to every other process.

Changing the keypair invalidates every subscription. Each browser must then
subscribe again, so keep the pair for the life of the deployment.
`, public, private)
	return 0
}
