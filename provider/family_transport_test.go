package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/internal/promtext"
)

func TestFamilyServiceUrl(t *testing.T) {
	cases := []struct {
		in   string
		ver  int
		want string
	}{
		{"wss://connect.bringyour.com", 4, "wss://connect-v4.bringyour.com"},
		{"wss://connect.bringyour.com", 6, "wss://connect-v6.bringyour.com"},
		{"https://api.example.com:8443/path?x=1", 6, "https://api-v6.example.com:8443/path?x=1"},
		{"connect.example.com", 4, "connect-v4.example.com"},
		{"wss://127.0.0.1", 4, ""},
		{"wss://127.0.0.1:8080", 4, ""},
		{"wss://[::1]:8080", 6, ""},
		{"wss://localhost", 4, ""},
		{"wss://connect-v4.example.com", 4, ""},
		{"wss://connect-v6.example.com", 6, ""},
		{"wss://connect.example.com", 5, ""},
		{"wss://connect.example.com", 0, ""},
		{"", 4, ""},
		{"wss://", 4, ""},
		{"wss://user@connect.example.com", 4, ""},
	}
	for _, c := range cases {
		if got := familyServiceUrl(c.in, c.ver); got != c.want {
			t.Errorf("familyServiceUrl(%q, %d) = %q, want %q", c.in, c.ver, got, c.want)
		}
	}
}

func TestIsUsableGlobalIpv6(t *testing.T) {
	for addr, want := range map[string]bool{
		"2001:4860::1":        true,
		"2a01::1":             true,
		"2606:65c0:40:277::1": true,
		"fd7a:115c:a1e0::1":   false, // Tailscale ULA
		"fe80::1":             false,
		"::1":                 false,
		"2001:db8::1":         false,
		"2002::1":             false,
		"2001:0::1":           false,
		"64:ff9b::102:304":    false,
		"64:ff9b:1::1":        false,
		"::ffff:1.2.3.4":      false,
		"::102:304":           false,
		"100.64.0.1":          false, // CGNAT is a v4 range
		"10.0.0.1":            false,
	} {
		if got := isUsableGlobalIpv6(net.ParseIP(addr)); got != want {
			t.Errorf("isUsableGlobalIpv6(%s) = %t, want %t", addr, got, want)
		}
	}
	if isUsableGlobalIpv6(nil) {
		t.Error("nil must not be usable")
	}
}

func TestProbeIpv6With(t *testing.T) {
	good := func() (net.IP, error) { return net.ParseIP("2001:4860::1"), nil }
	for name, c := range map[string]struct {
		supported bool
		source    func() (net.IP, error)
		wantOk    bool
		wantWhy   string
	}{
		"unsupported": {false, good, false, "no-global-v6-address"},
		"no route": {true, func() (net.IP, error) { return nil, net.UnknownNetworkError("x") },
			false, "no-v6-route"},
		"ula source": {true, func() (net.IP, error) { return net.ParseIP("fd7a:115c:a1e0::1"), nil },
			false, "v6-source-not-global"},
		"good": {true, good, true, "ok"},
	} {
		ok, why := probeIpv6With(func(int) bool { return c.supported }, c.source)
		if ok != c.wantOk || why != c.wantWhy {
			t.Errorf("%s: probe = (%t, %q), want (%t, %q)", name, ok, why, c.wantOk, c.wantWhy)
		}
	}
}

func TestUseFamilyTransportsDecisionOrder(t *testing.T) {
	const allowed = "wss://connect.bringyour.com"
	proxy := &connect.ProxySettings{Address: "127.0.0.1:1"}
	probeOk := func() (bool, string) { return true, "ok" }

	defer func(saved func() (bool, string)) { hostIpv6Probe = saved }(hostIpv6Probe)
	for name, c := range map[string]struct {
		proxy   *connect.ProxySettings
		native  bool
		url     string
		probe   func() (bool, string)
		wantUse bool
		reason  string
	}{
		"all good":    {nil, true, allowed, probeOk, true, "dualstack"},
		"proxied":     {proxy, true, allowed, probeOk, false, "proxied-identity"},
		"not native":  {nil, false, allowed, probeOk, false, "not-native"},
		"ip literal":  {nil, true, "wss://127.0.0.1", probeOk, false, "no-family-urls"},
		"custom host": {nil, true, "wss://connect.example.com", probeOk, false, "connect-host-not-allowlisted"},
		// a failed v6 probe no longer pins the launch to the legacy transport:
		// the group's v6 pin holds this identity on IPv4 until a global v6
		// address appears, and the host-network-change kick re-dials it
		"probe says no": {nil, true, allowed, func() (bool, string) { return false, "v6-source-not-global" },
			true, "dualstack"},
	} {
		hostIpv6Probe = c.probe
		use, reason := useFamilyTransports(c.proxy, c.native, c.url)
		if use != c.wantUse || reason != c.reason {
			t.Errorf("%s: got (%t, %q), want (%t, %q)", name, use, reason, c.wantUse, c.reason)
		}
	}
}

// The H3 socket of a transport pinned to IPv6 must be a v6 socket and any other
// a v4 socket, so the direct identity's H3 works on both pins of the group.
func TestNewProviderPlatformTransportExactlyOneConcrete(t *testing.T) {
	for _, useFamily := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		clientStrategySettings := connect.DefaultClientStrategySettings()
		clientStrategy := connect.NewClientStrategy(ctx, clientStrategySettings)
		settings := connect.DefaultPlatformTransportSettings()
		defaultBudget := settings.PlatformTransportBudget
		transport, legacy, group := newProviderPlatformTransport(
			ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test"),
			"wss://connect.bringyour.com",
			&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
			settings, useFamily,
		)
		if transport == nil {
			t.Fatalf("useFamily=%t: nil transport interface", useFamily)
		}
		if (legacy != nil) == (group != nil) {
			t.Fatalf("useFamily=%t: legacy=%v group=%v, want exactly one", useFamily, legacy != nil, group != nil)
		}
		// only the group gets a budget sized for three transports
		if useFamily == (settings.PlatformTransportBudget == defaultBudget) {
			t.Fatalf("useFamily=%t: the default budget must be replaced for the group and kept otherwise", useFamily)
		}
		if useFamily {
			if group == nil || providerPlatformTransport(group) != transport {
				t.Fatal("the group must be the returned transport")
			}
		} else if legacy == nil || providerPlatformTransport(legacy) != transport {
			t.Fatal("the legacy transport must be the returned transport")
		}
		transport.Close()
		cancel()
		clientStrategy.Close()
	}
}

// The renewal watcher keeps working with an unset transport.
func TestRenewalTransportAuthSetterNilIsNil(t *testing.T) {
	var cfg proxyJWTWatcherConfig
	if cfg.Transport != nil {
		t.Fatal("an unset Transport must be a nil interface")
	}
}

type familyTestPlatform struct {
	server *httptest.Server
	mu     sync.Mutex
	intent []string
	conns  []*websocket.Conn
}

// newFamilyTestPlatform is a websocket platform on one loopback family that
// echoes the first frame (the auth frame) so a transport reads as connected.
func newFamilyTestPlatform(t *testing.T, network string, hostPort string) *familyTestPlatform {
	t.Helper()
	listener, err := net.Listen(network, hostPort)
	if err != nil {
		return nil
	}
	platform := &familyTestPlatform{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	platform.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		platform.mu.Lock()
		platform.intent = append(platform.intent, r.Header.Get(connect.HeaderIpFamily))
		platform.conns = append(platform.conns, ws)
		platform.mu.Unlock()
		first := true
		for {
			messageType, message, err := ws.ReadMessage()
			if err != nil {
				ws.Close()
				return
			}
			if first {
				first = false
				if err := ws.WriteMessage(messageType, message); err != nil {
					ws.Close()
					return
				}
			}
		}
	}))
	platform.server.Listener = listener
	platform.server.Start()
	t.Cleanup(func() {
		platform.mu.Lock()
		for _, ws := range platform.conns {
			ws.Close()
		}
		platform.mu.Unlock()
		platform.server.Close()
	})
	return platform
}

func (self *familyTestPlatform) url() string {
	return "ws://" + self.server.Listener.Addr().String()
}

func (self *familyTestPlatform) intents() []string {
	self.mu.Lock()
	defer self.mu.Unlock()
	return append([]string(nil), self.intent...)
}

func familyTestWait(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// The group, built the way the provider builds it, connects one transport per
// family and declares each, stays up when only one family is left, and the
// status reporter publishes then clears its snapshot.
func TestFamilyGroupLoopbackEndToEnd(t *testing.T) {
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
	// H3 is on for the direct identity in production; the fake platform speaks
	// only websocket, so keep the loopback test to H1
	settings.EnableH3 = false
	settings.FamilyEvents = true
	logger := &familyTestLogger{}
	settings.Log = logger
	statsBefore := connect.FamilyTransportStatsNow()
	groupSettings := connect.DefaultFamilyPlatformTransportGroupSettings()
	groupSettings.StandbyDelay = time.Hour

	group := newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test"),
		platform4.url(), platform4.url(), platform6.url(),
		&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
		settings, groupSettings,
	)
	defer group.Close()
	stop := startFamilyStatusReporter(ctx, group)

	if !familyTestWait(15*time.Second, func() bool {
		return 0 < len(platform4.intents()) && 0 < len(platform6.intents())
	}) {
		t.Fatalf("each family server must see a pinned connection: v4=%v v6=%v",
			platform4.intents(), platform6.intents())
	}
	// a platform transport opens several connections (a plain one does too), so
	// check that every one a pin makes declares that pin's family and that the
	// parked standby, which would declare none, has not dialed
	for _, intent := range platform4.intents() {
		if intent != "4" {
			t.Errorf("a connection to the v4 platform declared family %q", intent)
		}
	}
	for _, intent := range platform6.intents() {
		if intent != "6" {
			t.Errorf("a connection to the v6 platform declared family %q", intent)
		}
	}
	if !familyTestWait(15*time.Second, group.IsConnected) {
		t.Fatal("the group never reported connected")
	}
	if !familyTestWait(5*time.Second, func() bool {
		snapshot := familyTransportLast.Load()
		return snapshot != nil && snapshot.HasIpv4 && snapshot.HasIpv6
	}) {
		t.Fatal("the status reporter never published a snapshot")
	}

	// lose the v6 platform: the group stays connected on v4
	platform6.mu.Lock()
	for _, ws := range platform6.conns {
		ws.Close()
	}
	platform6.mu.Unlock()
	platform6.server.Close()
	time.Sleep(500 * time.Millisecond)
	if !group.IsConnected() {
		t.Error("the group must stay connected while the v4 pin is up")
	}

	// the per-family counters and the log lines saw both families connect and
	// the v6 session end
	if !familyTestWait(5*time.Second, func() bool {
		after := connect.FamilyTransportStatsNow()
		return statsBefore.Families[1].Modes[0].Connects < after.Families[1].Modes[0].Connects &&
			sumDrops(statsBefore.Families[1]) < sumDrops(after.Families[1])
	}) {
		t.Errorf("the v6 pin's connect and drop were not counted: %+v", connect.FamilyTransportStatsNow().Families[1])
	}
	after := connect.FamilyTransportStatsNow()
	if after.Families[0].Modes[0].Connects <= statsBefore.Families[0].Modes[0].Connects {
		t.Error("the v4 pin's connect was not counted")
	}
	if after.Families[0].Mismatches != statsBefore.Families[0].Mismatches || after.Families[1].Mismatches != statsBefore.Families[1].Mismatches {
		t.Error("a pinned transport reported connecting over the other family")
	}
	if after.Families[0].Up != 1 {
		t.Errorf("the v4 pin should have one session up, has %d", after.Families[0].Up)
	}
	lines := strings.Join(logger.snapshot(), "")
	for _, want := range []string{"[ipv6] v4 h1 connected carrier=websocket remote=127.0.0.1:", "[ipv6] v6 h1 connected carrier=websocket remote=[::1]:", "[ipv6] v6 h1 session ended lived="} {
		if !strings.Contains(lines, want) {
			t.Errorf("log is missing %q:\n%s", want, lines)
		}
	}
	var b strings.Builder
	writeFamilyTransportMetrics(&b)
	if errs := promtext.Lint(b.String()); len(errs) != 0 {
		t.Errorf("promtext lint: %v\n%s", errs, b.String())
	}
	for _, want := range []string{
		`urnet_dualstack_connects_total{family="ipv6",mode="h1"}`,
		`urnet_dualstack_drops_total{family="ipv6",mode="h1",cause=`,
		`urnet_dualstack_session_lifetime_seconds_bucket{family="ipv4",le="+Inf"}`,
		`urnet_dualstack_pinned_down_events_total`,
		`urnet_dualstack_standby_activations_total`,
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("metrics are missing %q", want)
		}
	}
	t.Logf("sample log lines:\n%s", lines)

	stop()
	if familyTransportLast.Load() != nil {
		t.Error("stopping the reporter must clear the published snapshot")
	}
}

func sumDrops(f connect.FamilyTransportSnapshot) uint64 {
	var total uint64
	for _, mode := range f.Modes {
		for _, n := range mode.Drops {
			total += n
		}
	}
	return total
}

// familyTestLogger records Infof lines from the transports' goroutines.
type familyTestLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *familyTestLogger) Info(_ ...any) {}
func (l *familyTestLogger) Infof(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *familyTestLogger) Warningf(string, ...any) {}
func (l *familyTestLogger) Errorf(string, ...any)   {}
func (l *familyTestLogger) V(int32) connect.Verbose { return familyTestVerbose{} }
func (l *familyTestLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

type familyTestVerbose struct{}

func (familyTestVerbose) Enabled() bool        { return false }
func (familyTestVerbose) Info(...any)          {}
func (familyTestVerbose) Infof(string, ...any) {}

func TestFamilyTransportMetricsLint(t *testing.T) {
	familyTransportDecisionLast.Store(&familyTransportDecision{Use: true, Reason: "dualstack"})
	familyTransportLast.Store(&familyTransportSnapshot{HasIpv4: true, HasIpv6: true})
	defer familyTransportDecisionLast.Store(nil)
	defer familyTransportLast.Store(nil)

	var b strings.Builder
	writeFamilyTransportMetrics(&b)
	body := b.String()
	for _, want := range []string{
		`urnet_dualstack_mode{mode="group",reason="dualstack"} 1`,
		`urnet_dualstack_transport_state{transport="ipv4",state=`,
		`urnet_dualstack_transport_state{transport="standby",state=`,
		`urnet_dualstack_standby_active 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	}
	if errs := promtext.Lint(body); len(errs) != 0 {
		t.Errorf("promtext lint: %v\n%s", errs, body)
	}

	familyTransportDecisionLast.Store(&familyTransportDecision{Use: false, Reason: "proxied-identity"})
	familyTransportLast.Store(nil)
	b.Reset()
	writeFamilyTransportMetrics(&b)
	if !strings.Contains(b.String(), `urnet_dualstack_mode{mode="legacy",reason="proxied-identity"} 1`) ||
		strings.Contains(b.String(), "urnet_dualstack_transport_state") {
		t.Errorf("legacy path metrics wrong:\n%s", b.String())
	}
}

// The identity launch must decide the transport shape from the family decision
// and never build a single transport directly, so a source edit cannot silently
// bypass the dual-stack path.
func TestIdentityLaunchBuildsPlatformTransportThroughFamilyDecision(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "useFamilyTransports(proxySettings, isNative, connectUrl)") {
		t.Fatal("the identity launch must decide with useFamilyTransports")
	}
	if !strings.Contains(text, "newProviderPlatformTransport(") {
		t.Fatal("the identity launch must build through newProviderPlatformTransport")
	}
	if strings.Contains(text, "connect.NewPlatformTransport(") {
		t.Error("the identity launch must not build a single platform transport directly")
	}
}

func TestIpVersionControlKeyValidation(t *testing.T) {
	for _, good := range []string{"dual", "ipv4", "ipv6", "legacy", "v4", "v6"} {
		if err := validateControlValue("ip_version", good); err != nil {
			t.Errorf("ip_version=%q must be accepted: %v", good, err)
		}
	}
	for _, bad := range []string{"both", "single", "off", "auto", "ipv8", "yes"} {
		if err := validateControlValue("ip_version", bad); err == nil {
			t.Errorf("ip_version=%q must be rejected", bad)
		}
	}
	if needsRestart("ip_version") {
		t.Error("dualstack is live: needsRestart must be false")
	}
	// with no group running the value is only stored
	if err := applyLiveSideEffect("ip_version", "v6"); err != nil {
		t.Errorf("applying with no group must not fail: %v", err)
	}
}

// The operator's family choice moves the live group between dual stack, ipv4
// only, ipv6 only and legacy without a restart, and a fixed choice never
// falls back to the other family.
func TestFamilyGroupStackModes(t *testing.T) {
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
	groupSettings.StandbyDelay = 300 * time.Millisecond
	group := newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test-stack"),
		platform4.url(), platform4.url(), platform6.url(),
		&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
		settings, groupSettings,
	)
	defer group.Close()

	waitStatus := func(what string, cond func(connect.FamilyPlatformTransportGroupStatus) bool) {
		t.Helper()
		if !familyTestWait(15*time.Second, func() bool { return cond(group.Status()) }) {
			t.Fatalf("%s: status %+v", what, group.Status())
		}
	}
	connected := connect.PlatformTransportStateConnected
	disabled := connect.PlatformTransportStateDisabled

	waitStatus("dual: both pins connected", func(s connect.FamilyPlatformTransportGroupStatus) bool {
		return s.Ipv4 == connected && s.Ipv6 == connected && !s.StandbyActive
	})
	if group.Stack() != connect.FamilyStackDual {
		t.Fatalf("a new group must start in dual, has %s", group.Stack())
	}

	group.SetStack(connect.FamilyStackIpv4)
	waitStatus("v4 only", func(s connect.FamilyPlatformTransportGroupStatus) bool {
		return s.Ipv4 == connected && s.Ipv6 == disabled && !s.StandbyActive
	})

	group.SetStack(connect.FamilyStackIpv6)
	waitStatus("v6 only", func(s connect.FamilyPlatformTransportGroupStatus) bool {
		return s.Ipv6 == connected && s.Ipv4 == disabled && !s.StandbyActive
	})
	// a fixed v6 must not fall back to the standby even with v6 held for a while
	time.Sleep(2 * groupSettings.StandbyDelay)
	if group.Status().StandbyActive {
		t.Fatal("v6 only must never start the standby")
	}

	group.SetStack(connect.FamilyStackLegacy)
	waitStatus("legacy: the family-agnostic transport alone", func(s connect.FamilyPlatformTransportGroupStatus) bool {
		return s.Ipv4 == disabled && s.Ipv6 == disabled && s.StandbyActive && s.Standby == connected
	})

	group.SetStack(connect.FamilyStackDual)
	waitStatus("dual again: both pins, standby stands down", func(s connect.FamilyPlatformTransportGroupStatus) bool {
		return s.Ipv4 == connected && s.Ipv6 == connected && !s.StandbyActive
	})
}

// A group started in a fixed stack never dials the excluded family, not even once.
func TestFamilyGroupStartsInTheChosenStack(t *testing.T) {
	platform4 := newFamilyTestPlatform(t, "tcp4", "127.0.0.1:0")
	platform6 := newFamilyTestPlatform(t, "tcp6", "[::1]:0")
	if platform4 == nil || platform6 == nil || !connect.FamilySupported(6) {
		t.Skip("needs IPv4 and IPv6 loopback and a usable IPv6 stack")
	}
	for _, stack := range []connect.FamilyStackMode{connect.FamilyStackIpv6, connect.FamilyStackIpv4, connect.FamilyStackLegacy} {
		before4, before6 := len(platform4.intents()), len(platform6.intents())
		ctx, cancel := context.WithCancel(context.Background())
		clientStrategySettings := connect.DefaultClientStrategySettings()
		clientStrategy := connect.NewClientStrategy(ctx, clientStrategySettings)
		settings := connect.DefaultPlatformTransportSettings()
		settings.EnableH3 = false
		groupSettings := groupSettingsFor(stack)
		groupSettings.StandbyDelay = 200 * time.Millisecond
		group := newProviderFamilyGroup(
			ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test-start-stack"),
			platform4.url(), platform4.url(), platform6.url(),
			&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
			settings, groupSettings,
		)
		if !familyTestWait(15*time.Second, group.IsConnected) {
			t.Fatalf("%s: the group never connected: %+v", stack, group.Status())
		}
		time.Sleep(600 * time.Millisecond)
		got4, got6 := len(platform4.intents())-before4, len(platform6.intents())-before6
		switch stack {
		case connect.FamilyStackIpv6:
			if got4 != 0 || got6 == 0 {
				t.Errorf("ipv6 stack: v4 platform saw %d connections, v6 saw %d", got4, got6)
			}
		case connect.FamilyStackIpv4:
			if got6 != 0 || got4 == 0 {
				t.Errorf("ipv4 stack: v4 platform saw %d connections, v6 saw %d", got4, got6)
			}
		case connect.FamilyStackLegacy:
			// the family-agnostic transport dials the plain url (the v4 server here)
			// and declares no family
			if got6 != 0 || got4 == 0 {
				t.Errorf("legacy stack: v4 platform saw %d connections, v6 saw %d", got4, got6)
			}
			for _, intent := range platform4.intents()[before4:] {
				if intent != "" {
					t.Errorf("legacy stack declared family %q", intent)
				}
			}
		}
		group.Close()
		clientStrategy.Close()
		cancel()
	}
}

// The group's standby and pinned-down stretches feed process-wide counters. A
// stack set while the standby is active, and a group close, must end those
// stretches; otherwise the active seconds grow for the life of the process and
// dashboards read a permanent outage.
func TestFamilyGroupStackSwitchEndsTheStandbyStretch(t *testing.T) {
	platform := newFamilyTestPlatform(t, "tcp4", "127.0.0.1:0")
	if platform == nil {
		t.Skip("needs IPv4 loopback")
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
	groupSettings.StandbyDelay = 200 * time.Millisecond
	// both pins point at a closed port, so the standby (the live platform)
	// takes over after the delay
	dead := "ws://127.0.0.1:1"
	group := newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, connect.NewRouteManager(ctx, "test-standby-stretch"),
		platform.url(), dead, dead,
		&connect.ClientAuth{ByJwt: "testing", InstanceId: connect.NewId(), AppVersion: "testing"},
		settings, groupSettings,
	)
	if !familyTestWait(15*time.Second, func() bool { return group.Status().StandbyActive }) {
		group.Close()
		t.Fatalf("the standby never started: %+v", group.Status())
	}
	group.SetStack(connect.FamilyStackIpv4)
	if !familyTestWait(15*time.Second, func() bool { return !group.Status().StandbyActive }) {
		group.Close()
		t.Fatalf("the standby never stood down: %+v", group.Status())
	}
	time.Sleep(300 * time.Millisecond)
	before := connect.FamilyTransportStatsNow().Group.StandbyActiveSeconds
	time.Sleep(1200 * time.Millisecond)
	after := connect.FamilyTransportStatsNow().Group.StandbyActiveSeconds
	if 0.5 < after-before {
		group.Close()
		t.Fatalf("the standby active seconds still grow after the stack fixed the standby off (%.2f -> %.2f)", before, after)
	}
	group.Close()
	if connect.FamilyTransportStatsNow().Group.PinnedDown {
		t.Fatal("a closed group left the pinned-down stretch open")
	}
}
