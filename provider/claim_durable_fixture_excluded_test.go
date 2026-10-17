//go:build !(linux || darwin)

// The durable fixture helpers provision real linux/darwin kernel custody;
// on other platforms the tests that use them skip with an explicit reason
// so cross-target vet sees complete test packages.
package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func claimQueueTestContext(t testing.TB, ctx context.Context, paths ...string) context.Context {
	t.Helper()
	t.Skip("the durable fixture requires linux or darwin kernel custody")
	return ctx
}

func claimRetainedTestStore(t *testing.T, cfg *ClaimDaemonConfig, entry *ClaimQueueEntry) *claimQueueStore {
	t.Helper()
	t.Skip("the durable fixture requires linux or darwin kernel custody")
	return nil
}

func reconcileClaimEntryTest(t *testing.T, ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry) (string, error) {
	t.Helper()
	t.Skip("the durable fixture requires linux or darwin kernel custody")
	return "", nil
}

func reconcileSignedClaimTest(t *testing.T, ctx context.Context, cfg *ClaimDaemonConfig, entry *ClaimQueueEntry) (string, error) {
	t.Helper()
	t.Skip("the durable fixture requires linux or darwin kernel custody")
	return "", nil
}

func rebroadcastSignedClaimTest(t *testing.T, ctx context.Context, cfg *ClaimDaemonConfig, tx *types.Transaction, from common.Address) (bool, error) {
	t.Helper()
	t.Skip("the durable fixture requires linux or darwin kernel custody")
	return false, nil
}
