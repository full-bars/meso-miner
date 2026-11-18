// Package testsocket supplies short temporary directories for socket tests.
package testsocket

import (
	"os"
	"runtime"
	"testing"
)

// Dir returns a temporary directory whose Unix path does not depend on TMPDIR.
func Dir(t testing.TB) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "ut")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
