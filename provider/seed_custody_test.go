package main

// Platform capability probe for private seed file custody.
//
// The sn crv4 package implements seed file custody only where its build-tagged
// implementation does (linux, darwin); everywhere else LoadSeedFile refuses
// with "private seed file custody is unsupported on this platform" instead of
// falling back to a replacing writer. Tests that write a seed file and expect
// the product to load it back can only run where that custody exists, so they
// ask the implementation itself rather than switching on runtime.GOOS: the
// probe follows the crv4 build tags even if the supported set changes.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
)

// requireSeedFileCustody skips the calling test unless this platform can load
// a private seed file back. It is the first call in every provider test that
// depends on seed file custody.
func requireSeedFileCustody(t *testing.T) {
	t.Helper()
	probe := bytes.Repeat([]byte{0x11}, 32)
	path := filepath.Join(t.TempDir(), "custody-probe.seed")
	if err := os.WriteFile(path, probe, 0o600); err != nil {
		t.Fatalf("write seed custody probe: %v", err)
	}
	seed, err := crv4.LoadSeedFile(path)
	if err != nil {
		if strings.Contains(err.Error(), "unsupported on this platform") {
			t.Skipf("platform has no private seed file custody: %v", err)
		}
		t.Fatalf("seed custody probe failed on a platform that claims support: %v", err)
	}
	if !bytes.Equal(seed[:], probe) {
		t.Fatalf("seed custody probe did not round-trip the probe seed")
	}
}
