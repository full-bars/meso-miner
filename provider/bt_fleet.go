// The fleet command group (manifest, register, publish, bind, sign, status,
// revoke). Ported from the sn miner's fleet.go and friends; the subcommand
// dispatch lives in fleet.go (fleetCommand). Grammar is final in main.go.
//
// bind-head and unbind-head stay as they are beside this group: they are the
// pre-fleet EVM-only commands, kept for compatibility (docs carry the one-line
// deprecation note).
package main

import (
	"fmt"
	"os"

	"github.com/docopt/docopt-go"
)

// fleetCmd owns the top-level error surface of the group, matching the fix
// tree's other command handlers (bind-head, wallet challenge, ...).
func fleetCmd(opts docopt.Opts) {
	if err := fleetCommand(opts); err != nil {
		fmt.Printf("fleet failed: %s\n", err)
		os.Exit(1)
	}
}
