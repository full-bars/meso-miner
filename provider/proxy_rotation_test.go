package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect"
	"golang.org/x/net/proxy"
)

// TestSameAuth distinguishes identical vs changed credentials — the decision
// function behind proxy credential rotation.
func TestSameAuth(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		a := &connect.ProxySettings{}
		b := &connect.ProxySettings{}
		if !sameAuth(a, b) {
			t.Fatal("two nil-auth settings must compare equal")
		}
	})
	t.Run("nil vs set is different", func(t *testing.T) {
		a := &connect.ProxySettings{}
		b := &connect.ProxySettings{Auth: &proxy.Auth{User: "u", Password: "p"}}
		if sameAuth(a, b) {
			t.Fatal("nil auth vs set auth must differ")
		}
	})
	t.Run("same credentials equal", func(t *testing.T) {
		a := &connect.ProxySettings{Auth: &proxy.Auth{User: "test-user", Password: "test-pass"}}
		b := &connect.ProxySettings{Auth: &proxy.Auth{User: "test-user", Password: "test-pass"}}
		if !sameAuth(a, b) {
			t.Fatal("identical credentials must compare equal")
		}
	})
	t.Run("credential rotation detected", func(t *testing.T) {
		oldCred := &connect.ProxySettings{Auth: &proxy.Auth{User: "test-user", Password: "old-pass"}}
		newCred := &connect.ProxySettings{Auth: &proxy.Auth{User: "test-user-rotated", Password: "new-pass"}}
		if sameAuth(oldCred, newCred) {
			t.Fatal("different credentials must not compare equal (LA7 paste incident)")
		}
	})
}

// writeProxyConfigForTest writes a Servers map into the state dir, mirroring
// how paste/add persist entries keyed host:port:user:pass.
func writeProxyConfigForTest(t *testing.T, dir string, servers map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := readProxyConfig()
	if cfg.Servers == nil {
		cfg.Servers = map[string]string{}
	}
	for k, v := range servers {
		cfg.Servers[k] = v
	}
	writeProxyConfig(cfg)
}

// TestProxyAddRotatesCredentials verifies proxyAdd removes an existing
// same-address entry with different credentials when a new one is added, so a
// re-paste becomes a rotation instead of a silent duplicate that the
// address-keyed reload diff ignores (LA7 incident).
func TestProxyAddRotatesCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	writeProxyConfigForTest(t, filepath.Join(dir, ".urnetwork"), map[string]string{
		"192.0.2.4:1080:olduser:oldpass": "",
		"192.0.2.9:1080":                 "",
	})

	opts := docopt.Opts{
		"<key_address>": []string{"192.0.2.4:1080:newuser:newpass"},
		"-f":            true,
	}
	proxyAdd(opts)

	got := readProxyConfig()
	if len(got.Servers) != 2 {
		t.Fatalf("want 2 servers after rotation (replaced 1, kept 1), got %d: %v", len(got.Servers), got.Servers)
	}
	if _, ok := got.Servers["192.0.2.4:1080:newuser:newpass"]; !ok {
		t.Fatalf("new credential entry missing: %v", got.Servers)
	}
	if _, ok := got.Servers["192.0.2.4:1080:olduser:oldpass"]; ok {
		t.Fatalf("old credential entry was not rotated away: %v", got.Servers)
	}
	if _, ok := got.Servers["192.0.2.9:1080"]; !ok {
		t.Fatalf("unrelated proxy must survive: %v", got.Servers)
	}
}
