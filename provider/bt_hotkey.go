// The wallet hotkey commands (challenge, set, status). Ported from the sn
// provider's hotkey_wallet_cmd.go generation; the family implementation lives
// in hotkey_wallet.go and this file replaces the step-0 stub of the entry
// point. The docopt grammar and dispatch in main.go are final.
package main

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect"
)

// "provider wallet hotkey challenge|set|status".
func hotkeyWalletCmd(opts docopt.Opts) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	var err error
	if challenge, _ := opts.Bool("challenge"); challenge {
		err = hotkeyWalletChallenge(ctx, opts, os.Stdout)
	} else if set, _ := opts.Bool("set"); set {
		err = hotkeyWalletSet(ctx, opts, os.Stdout)
	} else {
		err = hotkeyWalletStatus(ctx, opts, os.Stdout)
	}
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallet hotkey: %v\n", err)
		os.Exit(1)
	}
}
