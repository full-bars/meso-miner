//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPidsMaxMembershipSelectsHierarchy(t *testing.T) {
	unified, legacy := t.TempDir(), t.TempDir()
	for _, tc := range []struct{ root, value string }{{unified, "512"}, {legacy, "16"}} {
		dir := filepath.Join(tc.root, "service")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pids.max"), []byte(tc.value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		membership string
		want       uint64
	}{
		{"0::/service\n", 512},
		{"4:cpu,pids:/service\n", 16},
		{"3:cpu:/service\n", 0},
		{"0::/service\n4:pids:/service\n", 16},
	} {
		if got := pidsMaxForMembership(tc.membership, unified, legacy); got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.membership, got, tc.want)
		}
	}
}
