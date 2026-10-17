// Persistent daemon owners consume an immutable operational declaration. Each
// actual store chooses its explicit daemon or owner-local API. Ported from the
// sn miner's durable_volume.go; the fleet family owns its own declaration
// helper for the fleet commands.
package main

import (
	"context"
	"strings"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/durablevolume"
)

// Parsing never enrolls storage, infers a mount, or chooses a synthetic Host.
func claimDaemonStorageContext(ctx context.Context, opts docopt.Opts) context.Context {
	path := claimDaemonOption(opts, "--durable-volumes")
	hash := claimDaemonOption(opts, "--durable-volumes-sha256")
	if path != "" || hash != "" {
		return durablevolume.WithReference(ctx, durablevolume.Reference{Path: path, Sha256: hash})
	}
	return ctx
}

// Only a real declaration enrolls the immutable reference; an absent flag
// stays an empty string rather than an empty enrollment.
func claimDaemonOption(opts docopt.Opts, name string) string {
	value, _ := opts.String(name)
	return strings.TrimSpace(value)
}
