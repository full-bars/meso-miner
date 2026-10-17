package connect

import (
	"strings"
	"testing"
)

func registerDegradedProxy(index int, address string) string {
	key := address
	RegisterProxy(index, address, key)
	markProxyUp(index)
	markProxyDown(index) // was up, now down: degraded
	return key
}

// A proxy waiting its turn under the attempt budget is not broken. The reaper,
// the dead-proxy cleanup and the URL-cache eviction must not read it as degraded
// or dead, or an outage would be followed by culling half the fleet.
func TestParkedProxyReadsAsConnectingNotDegraded(t *testing.T) {
	ResetProxyHealthForTesting()
	key := registerDegradedProxy(7, "10.0.0.7:1080")

	if len(DegradedProxies()) != 1 || !IsDegraded(key) {
		t.Fatal("setup: the proxy should read as degraded before it is parked")
	}

	markProxyParked(7, true)

	if len(DegradedProxies()) != 0 {
		t.Fatal("a parked proxy must not be a reaper candidate")
	}
	if IsDegraded(key) {
		t.Fatal("a parked proxy must not read as degraded")
	}
	_, dead, degraded, _, connecting := ProxyHealthSnapshot()
	if len(dead) != 0 || len(degraded) != 0 {
		t.Fatalf("a parked proxy is neither dead nor degraded: dead=%v degraded=%v", dead, degraded)
	}
	if len(connecting) != 1 || !strings.Contains(connecting[0], "10.0.0.7") {
		t.Fatalf("a parked proxy is waiting to connect, got %v", connecting)
	}
	if got := ProxyHealthByKey()[key].Health; got != "connecting" {
		t.Fatalf("health status = %q, want connecting", got)
	}

	markProxyParked(7, false)
	if len(DegradedProxies()) != 1 || !IsDegraded(key) {
		t.Fatal("once released it is judged on its own record again")
	}
}

func TestParkedNeverConnectedProxyIsNotDead(t *testing.T) {
	ResetProxyHealthForTesting()
	RegisterProxy(3, "10.0.0.3:1080", "10.0.0.3:1080")
	markProxyDown(3) // never up, connecting window closed: dead
	if _, dead, _, _, _ := ProxyHealthSnapshot(); len(dead) != 1 {
		t.Fatalf("setup: expected a dead proxy, got %v", dead)
	}

	markProxyParked(3, true)
	if _, dead, _, _, connecting := ProxyHealthSnapshot(); len(dead) != 0 || len(connecting) != 1 {
		t.Fatalf("a parked proxy must not be reported dead: dead=%v connecting=%v", dead, connecting)
	}
}

func TestParkingAnUnknownProxyIsHarmless(t *testing.T) {
	ResetProxyHealthForTesting()
	markProxyParked(99, true)
	markProxyParked(99, false)
}
