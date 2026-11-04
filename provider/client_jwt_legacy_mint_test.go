package main

import (
	"os"
	"path/filepath"
	"testing"
)

// An entry with no mint time predates the field. Dropping it on load would
// discard every identity in an existing store on the first start after an
// upgrade, so the operator pays a full re-auth for the whole proxy list.
func TestClientJWTStoreKeepsLegacyEntriesWithoutMintedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".client_jwts.json")
	data := []byte(`{"legacy":{"by_client_jwt":"legacy-jwt","client_id":"c-legacy"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newClientJWTStore(path)
	entry, ok := store.Get("legacy")
	if !ok {
		t.Fatal("an entry with no minted_at was dropped on load; an upgrade would force every proxy to re-auth")
	}
	if entry.ClientID != "c-legacy" {
		t.Fatalf("ClientID = %q, want c-legacy", entry.ClientID)
	}
}
