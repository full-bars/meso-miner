package main

// fleet_support.go carries the two provider helpers the ported fleet files
// need from outside their own file set. Both are ports of the sn miner
// originals (miner/durable_volume.go and miner/run.go's providerStateDir):
// the fix tree has no run.go to hold them yet, and the fleet group is their
// first consumer.
//
// Family note (#3693 split): minerStorageContext and providerStateDir are
// shared with the claim-daemon families. If those land the same helpers on
// their branches, keep one copy at merge time.

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/durablevolume"
)

// Parsing never enrolls storage, infers a mount, or chooses a synthetic Host.
func minerStorageContext(ctx context.Context, opts docopt.Opts) context.Context {
	path, hash := fleetOpt(opts, "--durable-volumes"), fleetOpt(opts, "--durable-volumes-sha256")
	if path != "" || hash != "" {
		return durablevolume.WithReference(ctx, durablevolume.Reference{Path: path, Sha256: hash})
	}
	return ctx
}

// Resolves the shared auth, network, proxy and provider state directory.
// URNETWORK_STATE_DIR overrides ~/.urnetwork; resolution creates no files.
func providerStateDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("URNETWORK_STATE_DIR")); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".urnetwork"), nil
}
