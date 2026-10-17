//go:build tools

// The vendored substrate RPC client is anchored here so the module graph keeps
// it between the plumbing commit and the family branches that import it. The
// npipe adapter needs no anchor: only the client's Windows-tagged files
// import it, and go mod tidy keeps it for that reason. Not built into any
// binary.
package main

import (
	_ "github.com/centrifuge/go-substrate-rpc-client/v4/client"
)
