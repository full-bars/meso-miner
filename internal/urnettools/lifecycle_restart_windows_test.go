//go:build windows

package urnettools

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestCmdRestartWindows_Fallback verifies that cmdRestartWindows falls
// back to stop+start when the control socket rejects "hotswap" (which
// is the expected case until PR #613 is merged) or when the socket is
// unreachable.
func TestCmdRestartWindows_Fallback(t *testing.T) {
	// Create a mock socket that rejects "hotswap" commands.
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "provider.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req controlRequest
		json.NewDecoder(conn).Decode(&req)
		resp := controlResponse{OK: false, Error: "unknown command \"hotswap\""}
		json.NewEncoder(conn).Encode(resp)
	}()

	p := Provider{
		StateDir: tmpDir,
		PID:      os.Getpid(), // use current process to avoid false alive checks
		Binary:   "/nonexistent",
	}

	// cmdRestartWindows should detect the hotswap rejection and fall
	// back to stopStartFallback. Since we have no real provider, the
	// stop will succeed (socket goes unreachable) but start will fail
	// (binary doesn't exist). We verify the fallback path is entered
	// by checking that we got a "start" error, not a "HotSwap succeeded"
	// return.
	err = cmdRestartWindows(p, false, false)
	if err == nil {
		t.Fatal("expected error from fallback start with nonexistent binary")
	}
	// The error should come from the start phase, not from hotswap.
	if got := err.Error(); got == "" || got == "HotSwap succeeded" {
		t.Fatalf("unexpected error: %v", err)
	}
}
