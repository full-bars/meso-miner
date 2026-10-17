// Missing provisioned state roots must not be recreated on a fallback device.
// These controls exercise the original stores without a signer or live route.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// An empty replacement queue must not erase the persisted relayer nonce floor.
func TestClaimQueueMissingVolumeDoesNotCreateFreshCustody(t *testing.T) {
	missingVolume := filepath.Join(t.TempDir(), "synthetic-missing-volume")
	store, err := newClaimQueueStore(filepath.Join(missingVolume, "claims"))
	if store != nil {
		if closeErr := store.close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	_, stateErr := os.Lstat(missingVolume)
	if err == nil || !errors.Is(stateErr, os.ErrNotExist) {
		t.Fatalf("missing volume became fresh claim custody: open=%v state=%v", err, stateErr)
	}
}
