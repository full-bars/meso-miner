package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJWTStoreMixedBatchReportsAllChanges(t *testing.T) {
	s := newClientJWTStore(filepath.Join(t.TempDir(), "store"))
	e := clientJWTEntry{ByClientJWT: "old", MintedAt: time.Now()}
	if err := s.Put("old", e); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("replace", e); err != nil {
		t.Fatal(err)
	}
	changed := e
	changed.ByClientJWT = "new"
	out := captureTlog(t, func() {
		if err := s.flushBatchLocked(map[string]clientJWTEntry{"new": e, "replace": changed}, []string{"old", "replace"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"added=1", "removed=1", "updated=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in mixed change counts: %s", want, out)
		}
	}
}
