package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJWTStoreFailedRenameNotCompleted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	s := newClientJWTStore(p)
	out := captureTlog(t, func() {
		if err := s.Put("x", clientJWTEntry{MintedAt: time.Now()}); err == nil {
			t.Fatal("expected failure")
		}
	})
	if s.flushes != 0 || strings.Contains(out, "[jwt-store] write") {
		t.Fatalf("failed rename counted as completed: flushes=%d log=%s", s.flushes, out)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(p), ".client_jwts-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temp file leaked: %v, %v", matches, err)
	}
}

func TestJWTStorePrunedDiskEntriesStayPruned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "store")
	now := time.Now()
	fresh := clientJWTEntry{MintedAt: now}
	stale := clientJWTEntry{MintedAt: now.Add(-clientJWTStaleAfter - time.Hour)}
	b, err := json.MarshalIndent(map[string]clientJWTEntry{"fresh": fresh, "stale": stale, "legacy": {}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	s := newClientJWTStore(p)
	if err := s.Put("new", fresh); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]clientJWTEntry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["stale"]; ok {
		t.Fatal("stale disk entry resurrected")
	}
	if _, ok := got["legacy"]; !ok {
		t.Fatal("legacy entry removed")
	}
	if !got["new"].MintedAt.Equal(fresh.MintedAt) {
		t.Fatal("mint time changed")
	}
}
