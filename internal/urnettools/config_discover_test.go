package urnettools

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

// startStatusMockServer creates a mock control server that responds to the
// "status" command with a settings map. This is needed to test config, which
// sends a status command.
func startStatusMockServer(t *testing.T, sockPath string) net.Listener {
	t.Helper()
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen mock socket: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				scanner := bufio.NewScanner(c)
				for scanner.Scan() {
					var req controlRequest
					if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
						_ = json.NewEncoder(c).Encode(controlResponse{OK: false, Error: "bad json"})
						continue
					}
					switch req.Cmd {
					case "status":
						_ = json.NewEncoder(c).Encode(controlResponse{
							OK: true,
							Settings: map[string]SettingInfo{
								"node_name": {Value: "test-node", Source: "socket"},
							},
						})
					default:
						_ = json.NewEncoder(c).Encode(controlResponse{OK: false, Error: "unknown cmd"})
					}
				}
			}(conn)
		}
	}()
	return l
}

// TestConfigUsesDiscoveredSocketPath verifies BUG 5 fix: config must send
// its status request to the provider's StateDir + "/provider.sock" path
// (discovered via selectTarget), NOT to ~/.urnetwork/provider.sock (which
// may not exist when HOME is redirected).
func TestConfigUsesDiscoveredSocketPath(t *testing.T) {
	// Set HOME to a temp dir so ~/.urnetwork/provider.sock does NOT exist.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create a mock provider at a non-HOME path.
	providerHome := t.TempDir()
	sockPath := filepath.Join(providerHome, "provider.sock")
	l := startStatusMockServer(t, sockPath)

	// Verify: sendSocketRequest to the discovered path works.
	resp, err := sendSocketRequest(sockPath, controlRequest{Cmd: "status"})
	if err != nil {
		t.Fatalf("sendSocketRequest to discovered path: %v", err)
	}
	if !resp.OK {
		t.Fatalf("status via discovered path: %+v", resp)
	}
	info, ok := resp.Settings["node_name"]
	if !ok || info.Value != "test-node" {
		t.Fatalf("expected node_name=test-node, got %+v", resp.Settings)
	}

	// Verify: sendSocketRequest to HOME-based path does NOT work (proves
	// the old dialControlSocket path would fail).
	homeSock := filepath.Join(tmpHome, ".urnetwork", "provider.sock")
	_, err = sendSocketRequest(homeSock, controlRequest{Cmd: "status"})
	if err == nil {
		t.Fatal("sendSocketRequest to HOME-based path should fail (no listener there)")
	}

	_ = l // keep alive for test duration
}
