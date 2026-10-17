package connect

import (
	"context"
	"strings"
	"testing"
	"time"
)

func resetFramedUpgradeCache(t *testing.T) {
	t.Helper()
	framedUpgradeCache.Lock()
	clear(framedUpgradeCache.until)
	framedUpgradeCache.Unlock()
	t.Cleanup(func() {
		framedUpgradeCache.Lock()
		clear(framedUpgradeCache.until)
		framedUpgradeCache.Unlock()
	})
}

var rejectedUpgrade = &HTTPUpgradeError{Reason: "rejected", StatusCode: 426}

const upgradeTestAddress = "wss://platform.example.invalid/"

// A rejection seen through one route (a proxy or middlebox that does not pass the
// framed upgrade) must not turn H1+ off on any other route.
func TestFramedUpgradeRejectionIsScopedToItsRoute(t *testing.T) {
	if !H1PlusAvailable() {
		t.Skip("h1+ is switched off in this build")
	}
	resetFramedUpgradeCache(t)
	proxyA, proxyB := "route-a", "route-b"

	recordFramedUpgradeFailureFor(proxyA, upgradeTestAddress, H1FramerProtocol, rejectedUpgrade)

	if framedUpgradePermittedFor(proxyA, upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("the route that saw the rejection must skip the upgrade")
	}
	if !framedUpgradePermittedFor(proxyB, upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("another route must still try the upgrade")
	}
	if !framedUpgradePermittedFor("", upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("the shared (direct) route must still try the upgrade")
	}
}

// The shared route keeps the behavior of a single path: a rejection there blocks
// that route only, not the scoped ones.
func TestFramedUpgradeSharedRouteIsItsOwnScope(t *testing.T) {
	if !H1PlusAvailable() {
		t.Skip("h1+ is switched off in this build")
	}
	resetFramedUpgradeCache(t)

	recordFramedUpgradeFailureFor("", upgradeTestAddress, H1FramerProtocol, rejectedUpgrade)

	if framedUpgradePermittedFor("", upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("the shared route must skip the upgrade after its own rejection")
	}
	if !framedUpgradePermittedFor("route-a", upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("a scoped route must not inherit the shared route's rejection")
	}
	// the exported wrappers are the shared route
	if FramedUpgradePermitted(upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("FramedUpgradePermitted is the shared route")
	}
}

// Only unsupported-protocol evidence is cached, per route, and it expires.
func TestFramedUpgradeOtherFailuresAreNotCached(t *testing.T) {
	if !H1PlusAvailable() {
		t.Skip("h1+ is switched off in this build")
	}
	resetFramedUpgradeCache(t)
	for _, status := range []int{401, 403, 500, 503} {
		recordFramedUpgradeFailureFor("route-a", upgradeTestAddress, H1FramerProtocol, &HTTPUpgradeError{Reason: "rejected", StatusCode: status})
	}
	if !framedUpgradePermittedFor("route-a", upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("a failure that is not evidence of an unsupported protocol must not be cached")
	}
	// expiry
	recordFramedUpgradeFailureFor("route-a", upgradeTestAddress, H1FramerProtocol, rejectedUpgrade)
	key := framedUpgradeCacheKey("route-a", upgradeTestAddress, H1FramerProtocol)
	framedUpgradeCache.Lock()
	framedUpgradeCache.until[key] = time.Now().Add(-time.Second)
	framedUpgradeCache.Unlock()
	if !framedUpgradePermittedFor("route-a", upgradeTestAddress, H1FramerProtocol) {
		t.Fatal("an expired rejection must permit the upgrade again")
	}
}

// The route scope must name the route by its stable identity, not by the dialer
// pointer. A dialer-set rebuild (a proxy reload, a recreated strategy) issues
// new pointers for the same route, and a freed pointer can be reused by a
// different route, so a pointer-keyed scope both forgets its own rejection and
// can inherit another route's. The scope also carries the family pin, so the
// dual-stack pins never alias each other, and it stays readable in a log.
func TestUpgradeRouteScopeDiffersPerDialer(t *testing.T) {
	directA := &clientDialer{description: "route-a"}
	directB := &clientDialer{description: "route-b"}
	if upgradeRouteScope(0, directA) == upgradeRouteScope(0, directB) {
		t.Error("two dialers must not share a scope")
	}
	if upgradeRouteScope(0, directA) != upgradeRouteScope(0, directA) {
		t.Error("a dialer's scope must be stable")
	}
	// a rebuilt dialer of the same route keeps the route's scope: this is the
	// property the pointer form cannot have
	if upgradeRouteScope(0, directA) != upgradeRouteScope(0, &clientDialer{description: "route-a"}) {
		t.Errorf("a rebuilt dialer of the same route must keep the route's scope, got %q and %q",
			upgradeRouteScope(0, directA), upgradeRouteScope(0, &clientDialer{description: "route-a"}))
	}

	// the scope is the readable identity form, the shape of String() without
	// the live counters
	if scope := upgradeRouteScope(0, directA); !strings.Contains(scope, "direct|route-a") {
		t.Errorf("a direct dialer's scope must carry its identity, got %q", scope)
	}
	proxyDialer := &clientDialer{
		description: "proxy route",
		settings: &ClientStrategySettings{
			ProxySettings: &ProxySettings{Index: 7, Address: "proxy.example.invalid:443"},
		},
	}
	if scope := upgradeRouteScope(0, proxyDialer); !strings.Contains(scope, "proxy|7|proxy.example.invalid:443|proxy route") {
		t.Errorf("a proxy dialer's scope must carry its index, address and description, got %q", scope)
	}

	// the family pin is part of the route: the same dialer dialed for v4, for
	// v6 and for the family-agnostic path is three routes
	scopeV4 := upgradeRouteScope(4, directA)
	scopeV6 := upgradeRouteScope(6, directA)
	scopeAny := upgradeRouteScope(0, directA)
	if scopeV4 == scopeV6 || scopeV4 == scopeAny || scopeV6 == scopeAny {
		t.Errorf("the family pins must not share a scope: v4=%q v6=%q any=%q", scopeV4, scopeV6, scopeAny)
	}
	if !strings.Contains(scopeV4, "ipv4|") || !strings.Contains(scopeV6, "ipv6|") {
		t.Errorf("the scope must read its family: v4=%q v6=%q", scopeV4, scopeV6)
	}
}

// The family prefix of a scope comes from the strategy that dials: the
// direct-only strategy a family-pinned platform transport owns carries its pin,
// and a family-agnostic strategy carries none. Without this the pins would all
// take the family-agnostic prefix and alias each other in the cache.
func TestUpgradeRouteScopeFollowsStrategyFamilyPin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := DefaultClientStrategySettings()
	for _, pin := range []int{0, 4, 6} {
		var strategy *ClientStrategy
		if pin == 0 {
			strategy = NewClientStrategy(ctx, settings)
		} else {
			strategy = NewDirectClientStrategy(ctx, settings, pin)
		}
		if strategy.ipFamily != pin {
			t.Errorf("a strategy built for family %d carries %d", pin, strategy.ipFamily)
		}
		strategy.Close()
	}
	strategy4 := NewDirectClientStrategy(ctx, settings, 4)
	defer strategy4.Close()
	if scope := upgradeRouteScope(strategy4.ipFamily, &clientDialer{description: "route-a"}); !strings.HasPrefix(scope, "ipv4|direct|route-a") {
		t.Errorf("a v4-pinned strategy's scope must read ipv4, got %q", scope)
	}
}
