// The claim daemon (claim-daemon --config) and the daemon2 stub. The command
// is ported from the sn miner's claim_daemon.go and its queue/admission/
// custody/storage subsystem; the v2 daemon family owns claim-daemon-v2 and
// its claim_v2_* files.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/stabi"
)

// stSettlementVault holds the shared abigen packers/unpackers for
// STSettlementVault: the entitlement and leafClaimed reads, and the Claimed,
// ClaimPaid and ClaimPaymentDeferred event decoders the claim queue
// reconciles with. The sn miner keeps this binding in its sn.go; the v33
// provider owns it here because the claim daemon is its first consumer.
var stSettlementVault = stabi.NewSTSettlementVault()

// claimDaemonCmd implements `provider claim-daemon --config=<path>
// [--durable-volumes=<path> --durable-volumes-sha256=<hash>]`.
func claimDaemonCmd(opts docopt.Opts) {
	configPath, _ := opts.String("--config")
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		fmt.Printf("claim-daemon: --config=<path> is required\n")
		os.Exit(1)
	}
	// runClaimDaemon owns the process event and signal handling; the storage
	// context carries only the immutable durable-volume declaration.
	if err := runClaimDaemon(claimDaemonStorageContext(context.Background(), opts), configPath); err != nil {
		fmt.Printf("claim-daemon: %s\n", err)
		os.Exit(1)
	}
}

// claimDaemonV2Cmd implements `provider claim-daemon-v2 --config=<path>
// [--v1-config=<path>] [--threshold-rao=<rao>] [--claim-now]`. The durable
// volume declaration rides the context, as the v1 command does.
func claimDaemonV2Cmd(opts docopt.Opts) {
	if err := runClaimDaemonV2Command(claimDaemonStorageContext(context.Background(), opts), opts); err != nil {
		fmt.Printf("claim-daemon-v2: %s\n", err)
		os.Exit(1)
	}
}
