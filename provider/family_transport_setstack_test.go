package main

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Concurrent ip_version sets must leave the pin switches in step with the
// stack they read: two racing SetStack calls used to interleave the stack
// assignment with the pin switch-overs, so the last assignment could lose to
// the other call's switches and the pins would dial a family the stack
// excluded (or park one it wants). The seam holds one apply open after its
// assignment: a concurrent set that finishes while it is held was applied
// unsynchronized.
func TestFamilyGroupSetStackConcurrentSetsKeepPinsInStep(t *testing.T) {
	platform4 := newFamilyTestPlatform(t, "tcp4", "127.0.0.1:0")
	platform6 := newFamilyTestPlatform(t, "tcp6", "[::1]:0")
	if platform4 == nil || platform6 == nil || !connect.FamilySupported(6) {
		t.Skip("needs IPv4 and IPv6 loopback and a usable IPv6 stack")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientStrategySettings := connect.DefaultClientStrategySettings()
	clientStrategy := connect.NewClientStrategy(ctx, clientStrategySettings)
	defer clientStrategy.Close()

	settings := connect.DefaultPlatformTransportSettings()
	settings.PinnedReconnectMaxTimeout = 200 * time.Millisecond
	settings.EnableH3 = false
	groupSettings := connect.DefaultFamilyPlatformTransportGroupSettings()
	group := newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test-stack"),
		platform4.url(), platform4.url(), platform6.url(),
		&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
		settings, groupSettings,
	)
	defer group.Close()

	// hold the first apply open after its assignment; a concurrent set must
	// not be able to finish its own application while it is held
	assigned := make(chan struct{})
	release := make(chan struct{})
	var paused atomic.Bool
	previousPause := connect.FamilySetStackApplyPauseForTest(func() {
		if paused.CompareAndSwap(false, true) {
			close(assigned)
			<-release
		}
	})
	defer connect.FamilySetStackApplyPauseForTest(previousPause)

	firstDone := make(chan connect.FamilyStackMode, 1)
	go func() {
		firstDone <- group.SetStack(connect.FamilyStackDual)
	}()
	<-assigned

	secondDone := make(chan connect.FamilyStackMode, 1)
	go func() {
		secondDone <- group.SetStack(connect.FamilyStackIpv4)
	}()
	select {
	case <-secondDone:
		close(release)
		<-firstDone
		t.Fatal("a concurrent SetStack finished while another apply was held: the applications are not serialized")
	case <-time.After(300 * time.Millisecond):
		// serialized as required: the second set is waiting for the first
	}
	close(release)
	<-firstDone
	<-secondDone

	// the serialized end state is the last set's. the unwanted pin's disable is
	// deferred until a wanted transport is connected (break before make), so
	// wait for the pins to settle on that end state
	if stack := group.Stack(); stack != connect.FamilyStackIpv4 {
		t.Fatalf("final stack %s, want ipv4 (the last set)", stack)
	}
	if !familyTestWait(10*time.Second, func() bool {
		ipv4, ipv6 := group.EnabledFamilies()
		return ipv4 && !ipv6
	}) {
		ipv4, ipv6 := group.EnabledFamilies()
		t.Fatalf("final pins ipv4=%t ipv6=%t, want ipv4=true ipv6=false", ipv4, ipv6)
	}

	// and a sequential pair stays consistent too
	group.SetStack(connect.FamilyStackDual)
	if ipv4, ipv6 := group.EnabledFamilies(); !ipv4 || !ipv6 {
		t.Fatalf("dual after ipv4: pins ipv4=%t ipv6=%t", ipv4, ipv6)
	}
}

// newBreakBeforeMakeGroup builds the loopback group the break-before-make tests
// drive. url4 and url6 are the pins' platform urls; the family-agnostic
// transport dials urlStandby. The standby delay is an hour so the standby never
// dials on its own in dual: only a legacy set releases it.
func newBreakBeforeMakeGroup(t *testing.T, name string, logger connect.Logger, urlStandby, url4, url6 string) (context.CancelFunc, *connect.FamilyPlatformTransportGroup) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clientStrategySettings := connect.DefaultClientStrategySettings()
	clientStrategy := connect.NewClientStrategy(ctx, clientStrategySettings)
	settings := connect.DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 200 * time.Millisecond
	settings.PinnedReconnectMaxTimeout = 200 * time.Millisecond
	settings.EnableH3 = false
	settings.Log = logger
	groupSettings := connect.DefaultFamilyPlatformTransportGroupSettings()
	groupSettings.StandbyDelay = time.Hour
	group := newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, name),
		urlStandby, url4, url6,
		&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
		settings, groupSettings,
	)
	t.Cleanup(func() {
		group.Close()
		clientStrategy.Close()
		cancel()
	})
	return cancel, group
}

// Break before make, ipv4/ipv6 -> legacy: the standby is released at once, but
// the connected v6 pin must keep carrying the identity until the standby is
// connected. The old code disabled the pins in SetStack itself, tearing down
// the only working transport before its replacement was up.
func TestFamilyGroupSetStackLegacyKeepsPinsUntilStandbyConnects(t *testing.T) {
	platform4 := newFamilyTestPlatform(t, "tcp4", "127.0.0.1:0")
	platform6 := newFamilyTestPlatform(t, "tcp6", "[::1]:0")
	if platform4 == nil || platform6 == nil || !connect.FamilySupported(6) {
		t.Skip("needs IPv4 and IPv6 loopback and a usable IPv6 stack")
	}
	logger := &familyTestLogger{}
	_, group := newBreakBeforeMakeGroup(t, "test-break-before-make-legacy", logger, platform4.url(), platform4.url(), platform6.url())

	connected := connect.PlatformTransportStateConnected
	disabled := connect.PlatformTransportStateDisabled
	// dual with a connected v6 pin
	if !familyTestWait(15*time.Second, func() bool { return group.Status().Ipv6 == connected }) {
		t.Fatalf("the v6 pin never connected: %+v", group.Status())
	}

	group.SetStack(connect.FamilyStackLegacy)
	// the standby has not been released yet (the group loop does that), so the
	// connected v6 pin must still be enabled here
	if _, ipv6 := group.EnabledFamilies(); !ipv6 {
		t.Fatalf("the v6 pin must stay enabled after SetStack(legacy) until the standby is connected")
	}
	// the standby is released at once
	if !familyTestWait(5*time.Second, func() bool { return group.Status().StandbyActive }) {
		t.Fatalf("the standby was not released after SetStack(legacy): %+v", group.Status())
	}
	// once the standby connected, the pins are the ones disabled
	if !familyTestWait(15*time.Second, func() bool {
		status := group.Status()
		ipv4, ipv6 := group.EnabledFamilies()
		return status.Standby == connected && !ipv4 && !ipv6
	}) {
		ipv4, ipv6 := group.EnabledFamilies()
		t.Fatalf("the pins were not disabled once the standby connected: %+v pins ipv4=%t ipv6=%t",
			group.Status(), ipv4, ipv6)
	}
	if status := group.Status(); status.Ipv4 != disabled || status.Ipv6 != disabled {
		t.Fatalf("the disabled pins must read disabled: %+v", status)
	}
	// the deferred application is one log line
	if lines := strings.Join(logger.snapshot(), ""); !strings.Contains(lines, "the deferred pin disable applies to") {
		t.Errorf("the deferred pin disable must be logged:\n%s", lines)
	}
}

// Break before make, fixed family: with the wanted pin down, the connected pin
// of the other family keeps carrying the identity until the wanted pin
// connects; only then is it disabled.
func TestFamilyGroupSetStackFixedFamilyKeepsPinUntilWantedConnects(t *testing.T) {
	platform6 := newFamilyTestPlatform(t, "tcp6", "[::1]:0")
	if platform6 == nil || !connect.FamilySupported(6) {
		t.Skip("needs IPv6 loopback and a usable IPv6 stack")
	}
	// reserve a v4 loopback port with nothing behind it yet: the v4 pin cannot
	// connect until the platform is started there
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil || !connect.FamilySupported(4) {
		t.Skip("needs IPv4 loopback and a usable IPv4 stack")
	}
	url4 := "ws://" + reserved.Addr().String()
	reserved.Close()

	_, group := newBreakBeforeMakeGroup(t, "test-break-before-make-fixed", nil, url4, url4, platform6.url())

	connected := connect.PlatformTransportStateConnected
	disabled := connect.PlatformTransportStateDisabled
	// dual with v6 connected and v4 down
	if !familyTestWait(15*time.Second, func() bool { return group.Status().Ipv6 == connected }) {
		t.Fatalf("the v6 pin never connected: %+v", group.Status())
	}

	group.SetStack(connect.FamilyStackIpv4)
	// the wanted v4 pin is down, so the v6 pin must stay enabled here
	if _, ipv6 := group.EnabledFamilies(); !ipv6 {
		t.Fatalf("the v6 pin must stay enabled after SetStack(ipv4) until the v4 pin connects")
	}
	// and it stays enabled and connected across several group loop passes
	time.Sleep(500 * time.Millisecond)
	if status := group.Status(); status.Ipv6 != connected {
		t.Fatalf("the v6 pin must keep carrying the identity while the wanted v4 pin is down: %+v", status)
	}
	if _, ipv6 := group.EnabledFamilies(); !ipv6 {
		t.Fatalf("the v6 pin must stay enabled while the wanted v4 pin is down")
	}

	// start the v4 platform on the reserved port: once the v4 pin connects,
	// the v6 pin is disabled
	platform4 := newFamilyTestPlatform(t, "tcp4", reserved.Addr().String())
	if platform4 == nil {
		t.Fatalf("could not start the v4 platform at %s", reserved.Addr().String())
	}
	if !familyTestWait(20*time.Second, func() bool {
		status := group.Status()
		_, ipv6 := group.EnabledFamilies()
		return status.Ipv4 == connected && status.Ipv6 == disabled && !ipv6
	}) {
		ipv4, ipv6 := group.EnabledFamilies()
		t.Fatalf("the v6 pin was not disabled after the v4 pin connected: %+v pins ipv4=%t ipv6=%t",
			group.Status(), ipv4, ipv6)
	}
}

// Break before make has nothing to break with nothing connected: the disable
// applies at once.
func TestFamilyGroupSetStackDisablesUnconnectedPinAtOnce(t *testing.T) {
	dead := "ws://127.0.0.1:1"
	_, group := newBreakBeforeMakeGroup(t, "test-break-before-make-nothing", nil, dead, dead, dead)

	if group.IsConnected() {
		t.Fatal("nothing must be connected")
	}
	group.SetStack(connect.FamilyStackIpv6)
	if ipv4, ipv6 := group.EnabledFamilies(); ipv4 || !ipv6 {
		t.Fatalf("with nothing connected the unwanted pin must be disabled at once: ipv4=%t ipv6=%t", ipv4, ipv6)
	}
}
