package main

// Dual-stack platform transport for the direct identity.
//
// A direct identity normally holds one family-agnostic platform transport, so
// the platform sees whichever family the dial happened to pick. On a host with
// a real global IPv6 address the identity instead runs connect's family group:
// one transport pinned to IPv4, one pinned to IPv6, and a parked family-agnostic
// standby that stands in only if neither pin connects. Holding both families
// lets the platform categorize the provider as dual-stack.
//
// Only the direct identity ever gets the group. A proxied identity reaches the
// platform through its proxy, and a pinned transport dials directly from the
// host, so a proxied identity stays on the single legacy transport (H1, and
// whatever family its proxy exits from). H3, when the operator has it on, stays
// available to the direct group: each pinned transport opens a UDP socket of its
// own family.
//
// The operator picks the families with the `ip_version` control key (dual, ipv4,
// ipv6 or legacy); legacy is the pre-dual-stack behavior, one family-agnostic
// transport.

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect"
)

// providerPlatformTransport is the slice of a platform transport that
// the provider's consumers need. Both *connect.PlatformTransport and
// *connect.FamilyPlatformTransportGroup satisfy it.
type providerPlatformTransport interface {
	IsConnected() bool
	ConnectedNotify() <-chan struct{}
	SetAuth(auth *connect.ClientAuth)
	Kick()
	Close()
}

var (
	_ providerPlatformTransport = (*connect.PlatformTransport)(nil)
	_ providerPlatformTransport = (*connect.FamilyPlatformTransportGroup)(nil)
)

// familyServiceUrl is the family-pinned form of a platform service url: the
// first host label gets a -v4 or -v6 suffix (connect becomes connect-v4). It
// returns "" when no family name can exist: an IP literal, a single-label host,
// a label that already carries a family suffix, an unparsable url, or a version
// other than 4 or 6.
func familyServiceUrl(serviceUrl string, ipVersion int) string {
	if ipVersion != 4 && ipVersion != 6 {
		return ""
	}
	suffix := "-v4"
	if ipVersion == 6 {
		suffix = "-v6"
	}
	scheme := ""
	rest := serviceUrl
	if i := strings.Index(serviceUrl, "://"); 0 <= i {
		scheme = serviceUrl[:i+3]
		rest = serviceUrl[i+3:]
	}
	if rest == "" {
		return ""
	}
	hostPort := rest
	tail := ""
	if i := strings.IndexAny(rest, "/?#"); 0 <= i {
		hostPort = rest[:i]
		tail = rest[i:]
	}
	if strings.ContainsAny(hostPort, "@ ") {
		return ""
	}
	host, port := hostPort, ""
	if strings.HasPrefix(hostPort, "[") {
		// a bracketed host is an IPv6 literal
		return ""
	}
	if i := strings.LastIndex(hostPort, ":"); 0 <= i {
		host, port = hostPort[:i], hostPort[i:]
	}
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	label, domain, ok := strings.Cut(host, ".")
	if !ok || label == "" || domain == "" {
		return ""
	}
	if strings.HasSuffix(label, "-v4") || strings.HasSuffix(label, "-v6") {
		return ""
	}
	return scheme + label + suffix + "." + domain + port + tail
}

// isUsableGlobalIpv6 is true for an address a platform can reach a provider by:
// global unicast, and none of the private, link-local, documentation, 6to4,
// Teredo, NAT64, v4-mapped or v4-compatible ranges. connect's own probe accepts
// a ULA such as Tailscale's fd7a::/48, so the provider applies this stricter test to the
// source address the kernel would pick.
func isUsableGlobalIpv6(ip net.IP) bool {
	if ip == nil || ip.To4() != nil || ip.To16() == nil {
		return false
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range unusableIpv6Ranges {
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

var unusableIpv6Ranges = func() []*net.IPNet {
	ranges := []*net.IPNet{}
	for _, s := range []string{
		"2001:db8::/32",  // documentation
		"2002::/16",      // 6to4
		"2001::/32",      // Teredo
		"64:ff9b::/96",   // NAT64
		"64:ff9b:1::/48", // local-use NAT64
		"::ffff:0:0/96",  // v4-mapped
		"::/96",          // v4-compatible
	} {
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			panic(err)
		}
		ranges = append(ranges, cidr)
	}
	return ranges
}()

// hostIpv6Probe is a seam for tests.
var hostIpv6Probe = probeHostIpv6Usable

// probeHostIpv6Usable asks the kernel, without sending a packet, whether this
// host has a global IPv6 source address and a route: a connected UDP socket does
// route and source-address selection only. It answers "can the kernel source a
// global v6 address", not "does v6 egress work".
func probeHostIpv6Usable() (bool, string) {
	return probeIpv6With(
		connect.FamilySupported,
		func() (net.IP, error) {
			conn, err := net.DialUDP("udp6", nil, &net.UDPAddr{IP: net.ParseIP("2606:4700:4700::1111"), Port: 443})
			if err != nil {
				return nil, err
			}
			defer conn.Close()
			return conn.LocalAddr().(*net.UDPAddr).IP, nil
		},
	)
}

func probeIpv6With(familySupported func(int) bool, localSource func() (net.IP, error)) (bool, string) {
	if !familySupported(6) {
		return false, "no-global-v6-address"
	}
	source, err := localSource()
	if err != nil {
		return false, "no-v6-route"
	}
	if !isUsableGlobalIpv6(source) {
		return false, "v6-source-not-global"
	}
	return true, "ok"
}

// familyTransportHostAllowed limits v1 to the one platform host whose -v4 and
// -v6 names were verified live. A custom --connect_url or test backend stays on
// the legacy transport until its family names are verified.
func familyTransportHostAllowed(connectUrl string) bool {
	rest := connectUrl
	if i := strings.Index(rest, "://"); 0 <= i {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#:"); 0 <= i {
		rest = rest[:i]
	}
	return strings.EqualFold(rest, "connect.bringyour.com")
}

// useFamilyTransports decides, for one identity launch, whether to build the
// dual-stack group. The checks run in a fixed order so the reason is
// deterministic. The host v6 probe does not decide: a node that boots before
// SLAAC finishes, or during a v6 outage, builds the group anyway and its v6
// pin holds this identity on IPv4 until a global v6 address appears - the
// pin's own hold and the host-network-change kick re-dial it, so the identity
// joins dual stack later without a relaunch.
func useFamilyTransports(proxySettings *connect.ProxySettings, isNative bool, connectUrl string) (bool, string) {
	if proxySettings != nil {
		return false, "proxied-identity"
	}
	if !isNative {
		return false, "not-native"
	}
	if familyServiceUrl(connectUrl, 4) == "" || familyServiceUrl(connectUrl, 6) == "" {
		return false, "no-family-urls"
	}
	if !familyTransportHostAllowed(connectUrl) {
		return false, "connect-host-not-allowlisted"
	}
	if ok, why := hostIpv6Probe(); !ok {
		// the group is still right: the v6 pin holds until the host has a
		// global v6 route. Note the launch-time state so an operator can see
		// why the v6 pin is quiet (the decision metric stays "dualstack").
		tlog("[ipv6] dual-stack group at launch; the v6 pin holds until a global v6 address appears (probe: %s)\n", why)
	}
	return true, "dualstack"
}

// newProviderPlatformTransport builds the identity's platform transport: the
// family group when useFamily, else exactly the single transport the provider
// has always built. Exactly one of legacy and group is non-nil, and transport
// is the matching non-nil interface value (never a typed nil).
func newProviderPlatformTransport(
	ctx context.Context,
	clientStrategySettings *connect.ClientStrategySettings,
	clientStrategy *connect.ClientStrategy,
	routeManager *connect.RouteManager,
	connectUrl string,
	auth *connect.ClientAuth,
	settings *connect.PlatformTransportSettings,
	useFamily bool,
) (transport providerPlatformTransport, legacy *connect.PlatformTransport, group *connect.FamilyPlatformTransportGroup) {
	if !useFamily {
		legacy = connect.NewPlatformTransport(ctx, clientStrategy, routeManager, connectUrl, auth, settings)
		return legacy, legacy, nil
	}
	// The default budget admits one carrier of each kind, and the group's
	// carrier mix differs by stack: dual runs both pins and the standby, a
	// fixed stack runs the one transport it uses. Size the budget for the
	// launch stack's transport count. It is admission accounting, not an
	// allocation.
	stack := initialFamilyStack()
	settings.PlatformTransportBudget = familyGroupBudgetForStack(stack)
	group = newProviderFamilyGroup(
		ctx, clientStrategySettings, clientStrategy, routeManager,
		connectUrl, familyServiceUrl(connectUrl, 4), familyServiceUrl(connectUrl, 6),
		auth, settings, groupSettingsFor(stack),
	)
	return group, nil, group
}

// newProviderFamilyGroup is the group constructor with explicit family urls, so
// a test can point the pins at loopback servers (familyServiceUrl refuses
// localhost and IP literals).
func newProviderFamilyGroup(
	ctx context.Context,
	clientStrategySettings *connect.ClientStrategySettings,
	clientStrategy *connect.ClientStrategy,
	routeManager *connect.RouteManager,
	connectUrl string,
	urlV4 string,
	urlV6 string,
	auth *connect.ClientAuth,
	settings *connect.PlatformTransportSettings,
	groupSettings *connect.FamilyPlatformTransportGroupSettings,
) *connect.FamilyPlatformTransportGroup {
	return connect.NewFamilyPlatformTransportGroup(
		ctx, clientStrategySettings, clientStrategy, routeManager,
		connectUrl, urlV4, urlV6, auth, connect.TransportModeAuto, settings, groupSettings,
	)
}

// familyTransportSnapshot is the last observed state of the direct identity's
// family group, read by /metrics. It is nil when the identity runs the legacy
// single transport.
type familyTransportSnapshot struct {
	Ipv4          connect.PlatformTransportState
	HasIpv4       bool
	Ipv6          connect.PlatformTransportState
	HasIpv6       bool
	Standby       connect.PlatformTransportState
	StandbyActive bool
}

var familyTransportLast atomic.Pointer[familyTransportSnapshot]

// familyTransportDecision is the last launch decision, read by /metrics.
type familyTransportDecision struct {
	Use    bool
	Reason string
}

var familyTransportDecisionLast atomic.Pointer[familyTransportDecision]

func recordFamilyTransportDecision(use bool, reason string) {
	familyTransportDecisionLast.Store(&familyTransportDecision{Use: use, Reason: reason})
	if use {
		tlog("[ipv6] direct identity platform transports: dualstack group\n")
	} else {
		tlog("[ipv6] direct identity platform transports: legacy single (reason=%s)\n", reason)
	}
}

// startFamilyStatusReporter samples the group's per-transport state every 30s
// and on each group connectivity change, publishes it for /metrics, and logs
// only when the (v4, v6, standby) tuple changes. The returned stop cancels the
// sampler, waits for it, and clears the published snapshot.
func startFamilyStatusReporter(ctx context.Context, group *connect.FamilyPlatformTransportGroup) (stop func()) {
	reporterCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go connect.HandleError(func() {
		defer wg.Done()
		var last familyTransportSnapshot
		first := true
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			notify := group.ConnectedNotify()
			status := group.Status()
			snapshot := familyTransportSnapshot{
				Ipv4: status.Ipv4, HasIpv4: status.HasIpv4,
				Ipv6: status.Ipv6, HasIpv6: status.HasIpv6,
				Standby: status.Standby, StandbyActive: status.StandbyActive,
			}
			familyTransportLast.Store(&snapshot)
			if first || snapshot != last {
				tlog("[ipv6] family transports v4=%s v6=%s standby=%s active=%t\n",
					snapshot.Ipv4, snapshot.Ipv6, snapshot.Standby, snapshot.StandbyActive)
				last = snapshot
				first = false
			}
			select {
			case <-reporterCtx.Done():
				return
			case <-ticker.C:
			case <-notify:
			}
		}
	})
	return func() {
		cancel()
		wg.Wait()
		familyTransportLast.Store(nil)
	}
}

// writeFamilyTransportMetrics emits the dual-stack decision and, when the group
// runs, each transport's state. The reason set is closed (see
// useFamilyTransports) so label cardinality stays bounded.
func writeFamilyTransportMetrics(b *strings.Builder) {
	writeFamilyStatsMetrics(b)
	decision := familyTransportDecisionLast.Load()
	if decision == nil {
		return
	}
	mode := "legacy"
	if decision.Use {
		mode = "group"
	}
	fmt.Fprintf(b, "# HELP urnet_dualstack_mode Direct identity platform transport mode, value always 1: group (v4 and v6 pins plus standby) or legacy (single), with the reason.\n")
	fmt.Fprintf(b, "# TYPE urnet_dualstack_mode gauge\n")
	fmt.Fprintf(b, "urnet_dualstack_mode{mode=%s,reason=%s} 1\n", connect.PrometheusLabelValue(mode), connect.PrometheusLabelValue(decision.Reason))

	if group := familyGroupLive.Load(); group != nil {
		fmt.Fprintf(b, "# HELP urnet_ip_version The operator's family choice (control key ip_version, urnet-tools set ip-version): 1 for the active one of dual, ipv4, ipv6, legacy.\n")
		fmt.Fprintf(b, "# TYPE urnet_ip_version gauge\n")
		for _, mode := range []connect.FamilyStackMode{connect.FamilyStackDual, connect.FamilyStackIpv4, connect.FamilyStackIpv6, connect.FamilyStackLegacy} {
			value := 0
			if group.Stack() == mode {
				value = 1
			}
			fmt.Fprintf(b, "urnet_ip_version{mode=%s} %d\n", connect.PrometheusLabelValue(string(mode)), value)
		}
	}

	snapshot := familyTransportLast.Load()
	if snapshot == nil {
		return
	}
	fmt.Fprintf(b, "# HELP urnet_dualstack_transport_state Per-transport state of the dual-stack group: 1 for the state the transport is in.\n")
	fmt.Fprintf(b, "# TYPE urnet_dualstack_transport_state gauge\n")
	write := func(transport string, has bool, state connect.PlatformTransportState) {
		if !has {
			return
		}
		fmt.Fprintf(b, "urnet_dualstack_transport_state{transport=%s,state=%s} 1\n",
			connect.PrometheusLabelValue(transport), connect.PrometheusLabelValue(state.String()))
	}
	write("ipv4", snapshot.HasIpv4, snapshot.Ipv4)
	write("ipv6", snapshot.HasIpv6, snapshot.Ipv6)
	write("standby", true, snapshot.Standby)
	active := 0
	if snapshot.StandbyActive {
		active = 1
	}
	fmt.Fprintf(b, "# HELP urnet_dualstack_standby_active Whether the family-agnostic standby is dialing because no pinned transport is connected.\n")
	fmt.Fprintf(b, "# TYPE urnet_dualstack_standby_active gauge\n")
	fmt.Fprintf(b, "urnet_dualstack_standby_active %d\n", active)
}

// familyBudgetReferenceByteCount is connect's reference memory budget, used
// when no process-wide memory target is set.
const familyBudgetReferenceByteCount = 64 << 20

// familyGroupBudgetForStack is the transport budget for one identity's family
// group running the given stack: the default sizing scaled for the number of
// transports the stack enables - three for dual (both pins and the
// family-agnostic standby), one for any fixed stack (the single pin it uses,
// or the agnostic alone for legacy). It is admission accounting, not an
// allocation, and it is sized at launch: a live stack change applies the new
// sizing at the next identity launch.
func familyGroupBudgetForStack(stack connect.FamilyStackMode) *connect.PlatformTransportBudget {
	target := connect.MemoryBudget()
	if target <= 0 {
		target = familyBudgetReferenceByteCount
	}
	transports := 1
	if stack == connect.FamilyStackDual {
		transports = 3
	}
	return connect.NewPlatformTransportBudgetForMemoryTarget(connect.ByteCount(transports) * target)
}

// writeFamilyStatsMetrics exports the native identity's per-family counters
// (connect/transport_family_stats.go). family is ipv4, ipv6, or any (a
// family-agnostic transport: the standby, or the single legacy transport), mode
// is the carrier (h1, h3) and cause is the close cause. Every label set is
// closed. The series stay absent until the native identity has a transport.
func writeFamilyStatsMetrics(b *strings.Builder) {
	stats := connect.FamilyTransportStatsNow()
	active := false
	for _, family := range stats.Families {
		if family.Modes[0].Attempts+family.Modes[1].Attempts+uint64(family.Up) != 0 {
			active = true
		}
	}
	if !active {
		return
	}
	header := func(name string, kind string, help string) {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	perMode := func(name string, help string, value func(connect.FamilyModeSnapshot) uint64) {
		header(name, "counter", help)
		for _, family := range stats.Families {
			for m, modeName := range stats.ModeNames {
				fmt.Fprintf(b, "%s{family=%s,mode=%s} %d\n", name,
					connect.PrometheusLabelValue(family.Family), connect.PrometheusLabelValue(modeName), value(family.Modes[m]))
			}
		}
	}
	perMode("urnet_dualstack_connect_attempts_total", "Platform connect attempts of the native identity's transports, by family and carrier.",
		func(s connect.FamilyModeSnapshot) uint64 { return s.Attempts })
	perMode("urnet_dualstack_connects_total", "Platform connects that authenticated, by family and carrier.",
		func(s connect.FamilyModeSnapshot) uint64 { return s.Connects })
	perMode("urnet_dualstack_connect_failures_total", "Platform connect attempts that failed, by family and carrier.",
		func(s connect.FamilyModeSnapshot) uint64 { return s.Failures })

	header("urnet_dualstack_drops_total", "counter", "Sessions that ended on their own (not our shutdown or the operator gate), by family, carrier and close cause.")
	for _, family := range stats.Families {
		for m, modeName := range stats.ModeNames {
			for c, causeName := range stats.CauseNames {
				fmt.Fprintf(b, "urnet_dualstack_drops_total{family=%s,mode=%s,cause=%s} %d\n",
					connect.PrometheusLabelValue(family.Family), connect.PrometheusLabelValue(modeName),
					connect.PrometheusLabelValue(causeName), family.Modes[m].Drops[c])
			}
		}
	}
	perFamily := func(name string, kind string, help string, value func(connect.FamilyTransportSnapshot) string) {
		header(name, kind, help)
		for _, family := range stats.Families {
			fmt.Fprintf(b, "%s{family=%s} %s\n", name, connect.PrometheusLabelValue(family.Family), value(family))
		}
	}
	perFamily("urnet_dualstack_sessions_up", "gauge", "Platform sessions up right now, by family.",
		func(f connect.FamilyTransportSnapshot) string { return fmt.Sprintf("%d", f.Up) })
	perFamily("urnet_dualstack_connected_seconds_total", "counter", "Seconds with at least one session up, by family (overlapping carriers count once).",
		func(f connect.FamilyTransportSnapshot) string { return fmt.Sprintf("%.3f", f.ConnectedSeconds) })
	perFamily("urnet_dualstack_last_connect_timestamp_seconds", "gauge", "Unix time of the last connect, by family; 0 if none yet.",
		func(f connect.FamilyTransportSnapshot) string { return fmt.Sprintf("%d", f.LastConnectUnix) })
	perFamily("urnet_dualstack_last_drop_timestamp_seconds", "gauge", "Unix time of the last session drop, by family; 0 if none yet.",
		func(f connect.FamilyTransportSnapshot) string { return fmt.Sprintf("%d", f.LastDropUnix) })
	perFamily("urnet_dualstack_family_mismatch_total", "counter", "Pinned transports that connected over the other family. Anything above 0 is a bug.",
		func(f connect.FamilyTransportSnapshot) string { return fmt.Sprintf("%d", f.Mismatches) })

	header("urnet_dualstack_session_lifetime_seconds", "histogram", "Lifetime of ended sessions, by family.")
	for _, family := range stats.Families {
		label := connect.PrometheusLabelValue(family.Family)
		for i, bound := range stats.LifetimeBounds {
			fmt.Fprintf(b, "urnet_dualstack_session_lifetime_seconds_bucket{family=%s,le=\"%d\"} %d\n", label, bound, family.LifetimeBuckets[i])
		}
		fmt.Fprintf(b, "urnet_dualstack_session_lifetime_seconds_bucket{family=%s,le=\"+Inf\"} %d\n", label, family.LifetimeBuckets[len(stats.LifetimeBounds)])
		fmt.Fprintf(b, "urnet_dualstack_session_lifetime_seconds_sum{family=%s} %.3f\n", label, family.LifetimeSumSeconds)
		fmt.Fprintf(b, "urnet_dualstack_session_lifetime_seconds_count{family=%s} %d\n", label, family.LifetimeCount)
	}

	group := stats.Group
	scalar := func(name string, kind string, help string, value string) {
		header(name, kind, help)
		fmt.Fprintf(b, "%s %s\n", name, value)
	}
	down := 0
	if group.PinnedDown {
		down = 1
	}
	scalar("urnet_dualstack_pinned_down", "gauge", "1 while no pinned (IPv4 or IPv6) transport is connected, after the first connect.", fmt.Sprintf("%d", down))
	scalar("urnet_dualstack_pinned_down_events_total", "counter", "Times every pinned transport was down at once.", fmt.Sprintf("%d", group.PinnedDownEvents))
	scalar("urnet_dualstack_pinned_down_seconds_total", "counter", "Seconds with no pinned transport connected.", fmt.Sprintf("%.3f", group.PinnedDownSeconds))
	scalar("urnet_dualstack_pinned_down_longest_seconds", "gauge", "Longest single stretch with no pinned transport connected.", fmt.Sprintf("%.3f", group.PinnedLongestSecond))
	scalar("urnet_dualstack_standby_activations_total", "counter", "Times the family-agnostic standby started dialing.", fmt.Sprintf("%d", group.StandbyActivations))
	scalar("urnet_dualstack_standby_active_seconds_total", "counter", "Seconds the standby has been dialing.", fmt.Sprintf("%.3f", group.StandbyActiveSeconds))
}

// The running native identity's family group, for the live `ip_version` control
// key and /metrics. Nil when the identity runs the single legacy transport.
var familyGroupLive atomic.Pointer[connect.FamilyPlatformTransportGroup]

// applyIpVersion applies the `ip_version` control key (dual, ipv4, ipv6, legacy) to
// the running group. With no group yet (the node is not dual-stack, or the
// identity has not launched) the value is only stored; the launch reads it.
func applyIpVersion(value string) error {
	mode, err := connect.ParseFamilyStackMode(value)
	if err != nil {
		return err
	}
	group := familyGroupLive.Load()
	if group == nil {
		tlog("⚙️ [control] ip_version=%s stored; no dual-stack group is running, it applies at the next identity launch\n", mode)
		return nil
	}
	previous := group.SetStack(mode)
	// the group budget is sized for the launch stack's transport count (see
	// familyGroupBudgetForStack) and has no live resize; a changed stack
	// applies its new sizing at the next identity launch
	tlog("⚙️ [control] applied ip_version=%s (was %s); the transport budget resizes at the next identity launch\n", mode, previous)
	return nil
}

// initialFamilyStack is the persisted `ip_version` choice for a freshly built group.
func initialFamilyStack() connect.FamilyStackMode {
	if globalControlState != nil {
		if value, ok := globalControlState.get("ip_version"); ok {
			if mode, err := connect.ParseFamilyStackMode(value); err == nil {
				return mode
			}
		}
	}
	return connect.FamilyStackDual
}

// groupSettingsFor is the default group settings starting in the given stack.
func groupSettingsFor(stack connect.FamilyStackMode) *connect.FamilyPlatformTransportGroupSettings {
	groupSettings := connect.DefaultFamilyPlatformTransportGroupSettings()
	groupSettings.Stack = stack
	return groupSettings
}
