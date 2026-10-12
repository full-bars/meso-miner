package main

import (
	"context"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// While the backend is failing, every proxy fails its API probe and its auth, so
// nothing about a proxy's own record can be trusted: evicting then (with a 24h
// blacklist) would empty the pool of good proxies. These tests pin the three
// automatic eviction paths to the outage freeze.

func freezeForTest(t *testing.T, frozen bool) {
	t.Helper()
	prev := degradedReaperFrozen
	degradedReaperFrozen = func() bool { return frozen }
	t.Cleanup(func() { degradedReaperFrozen = prev })
}

func TestRunProxyURLCleanupOnce_HoldsOffDuringAnOutage(t *testing.T) {
	withTempHome(t)
	if err := writeProxyURLState(&ProxyURLState{Cache: map[string]ProxyURLEntry{"4.4.4.4:1080": {}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeProxyState(&ProxyState{Proxies: map[string]ProxyEntry{
		"4.4.4.4:1080": {Health: "dead", Source: "url"},
	}}); err != nil {
		t.Fatal(err)
	}

	freezeForTest(t, true)
	if removed := runProxyURLCleanupOnce("url"); removed != 0 {
		t.Fatalf("removed %d proxies during an outage", removed)
	}
	if st, _ := readProxyURLState(); len(st.Cache) != 1 {
		t.Fatal("the URL cache lost a proxy during an outage")
	}

	freezeForTest(t, false)
	if removed := runProxyURLCleanupOnce("url"); removed != 1 {
		t.Fatalf("after the outage the dead proxy should be removed, removed=%d", removed)
	}
}

func TestRunURLProxyReaperOnce_DoesNotDemoteOrBlacklistDuringAnOutage(t *testing.T) {
	withTempHome(t)
	dead := "127.0.0.1:1" // refuses at once, like every proxy's API stage in an outage
	good := "127.0.0.1:2"
	if err := writeProxyURLState(&ProxyURLState{Cache: map[string]ProxyURLEntry{
		// a once-good proxy whose stale re-probe now fails
		dead: {ProbeOK: true, LastProbe: time.Now().Add(-4 * time.Hour)},
		// an unproven proxy one failure short of the blacklist
		good: {ProbeOK: false, ProbeFails: proxyAPIMaxFails - 1},
	}}); err != nil {
		t.Fatal(err)
	}

	freezeForTest(t, true)
	runURLProxyReaperOnce(context.Background(), "", 0)

	st, err := readProxyURLState()
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := st.Cache[dead]; !ok || !e.ProbeOK || e.ProbeFails != 0 {
		t.Fatalf("a once-good proxy was demoted during an outage: %+v ok=%v", e, ok)
	}
	if e, ok := st.Cache[good]; !ok || e.ProbeFails != proxyAPIMaxFails-1 {
		t.Fatalf("a failing probe was counted during an outage: %+v ok=%v", e, ok)
	}
	if len(st.Blacklist) != 0 {
		t.Fatalf("proxies were blacklisted during an outage: %v", st.Blacklist)
	}

	// the same probes after the outage count as before
	freezeForTest(t, false)
	runURLProxyReaperOnce(context.Background(), "", 0)
	st, _ = readProxyURLState()
	if _, blacklisted := st.Blacklist[good]; !blacklisted {
		t.Fatal("after the outage a proxy at the failure limit must still be blacklisted")
	}
}

// A proxy that is still connecting, or parked waiting its turn under the attempt
// budget, has no probe verdict worth acting on.
func TestRunURLProxyReaperOnce_IgnoresProxiesStillConnectingOrParked(t *testing.T) {
	withTempHome(t)
	addr := "127.0.0.1:3"
	if err := writeProxyURLState(&ProxyURLState{Cache: map[string]ProxyURLEntry{
		addr: {ProbeOK: false, ProbeFails: proxyAPIMaxFails - 1},
	}}); err != nil {
		t.Fatal(err)
	}
	freezeForTest(t, false)
	prev := liveProxyHealthByKey
	liveProxyHealthByKey = func() map[string]connect.ProxyHealthStatus {
		return map[string]connect.ProxyHealthStatus{addr: {Health: "connecting"}}
	}
	t.Cleanup(func() { liveProxyHealthByKey = prev })

	runURLProxyReaperOnce(context.Background(), "", 0)
	st, _ := readProxyURLState()
	if _, ok := st.Cache[addr]; !ok || len(st.Blacklist) != 0 {
		t.Fatalf("a connecting/parked proxy was blacklisted: cache=%v blacklist=%v", st.Cache, st.Blacklist)
	}
}

func TestProxyGiveUpCounts_NotDuringAnOutage(t *testing.T) {
	freezeForTest(t, true)
	if proxyGiveUpMayCount() {
		t.Fatal("auth failures during an outage must not count toward evicting a proxy")
	}
	freezeForTest(t, false)
	if !proxyGiveUpMayCount() {
		t.Fatal("outside an outage give-ups count as before")
	}
}
