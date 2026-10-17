// Claim discovery and preflight GETs retain read deadlines independently of
// signing and submission. Caller admission deadlines always take precedence.
package main

import (
	"context"
	"errors"

	"github.com/urnetwork/connect"
)

// The upstream sdk strategy settings carried a request-allowance and a retry
// count field this provider's connect strategy does not have; the daemon keeps
// the module defaults and relies on the finite retry owner in
// claim_read_retry.go for pacing and status recovery.
func defaultClaimReadStrategySettings() *connect.ClientStrategySettings {
	return connect.DefaultClientStrategySettings()
}

// Capture the selected epoch before retry. Reconstruct only the GET arguments;
// no queue mutation, proof decision, signing or transaction send enters the loop.
func readClaimPoolWithRetry(ctx context.Context, api claimAPI, epoch int64, hooks claimReadRetryHooks) (*connect.SnPoolClaimResult, error) {
	if epoch < 0 {
		return nil, errors.New("claim pool read has a negative epoch")
	}
	return retryClaimApiRead(ctx, hooks, func(readCtx context.Context) (*connect.SnPoolClaimResult, error) {
		return api.SnPoolClaimSyncWithContext(readCtx, &connect.SnPoolClaimArgs{Epoch: uint64(epoch)})
	})
}
