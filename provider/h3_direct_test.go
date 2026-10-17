package main

import (
	"testing"

	"github.com/urnetwork/connect"
)

func TestH3EnabledParsesTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false}, {"off", false}, {"0", false}, {"false", false}, {"no", false}, {"garbage", false},
		{"on", true}, {"ON", true}, {" 1 ", true}, {"true", true}, {"Yes", true},
	} {
		t.Setenv("URNETWORK_H3", tc.value)
		if got := h3Enabled(); got != tc.want {
			t.Fatalf("URNETWORK_H3=%q: h3Enabled=%v, want %v", tc.value, got, tc.want)
		}
	}
}

// H3 is for the direct identity only. A proxied identity must never be eligible,
// whatever the switch says: runH3 opens a host UDP socket, which would send its
// QUIC from the host instead of its proxy.
func TestH3NeverEligibleForAProxiedIdentity(t *testing.T) {
	proxied := &connect.ProxySettings{Network: "tcp", Address: "127.0.0.1:1080"}

	for _, env := range []string{"on", ""} {
		t.Setenv("URNETWORK_H3", env)
		if platformTransportSettingsFor(proxied, false).EnableH3 {
			t.Fatal("a proxied identity was eligible for H3")
		}
		if platformTransportSettingsFor(proxied, true).EnableH3 {
			t.Fatal("an identity with proxy settings was eligible even though flagged native")
		}
		if platformTransportSettingsFor(nil, false).EnableH3 {
			t.Fatal("a non-native identity without proxy settings was eligible")
		}
		// the beside-H1 auxiliary is the direct identity's opt-in (h3 on)
		if got := platformTransportSettingsFor(nil, true).EnableH3; got != (env == "on") {
			t.Fatalf("URNETWORK_H3=%q: direct EnableH3=%v", env, got)
		}
		for _, settings := range []*connect.PlatformTransportSettings{
			platformTransportSettingsFor(proxied, false),
			platformTransportSettingsFor(proxied, true),
			platformTransportSettingsFor(nil, false),
		} {
			if len(settings.ModePreferences) != 1 || settings.ModePreferences[connect.TransportModeH1] != 1 {
				t.Fatalf("a proxied identity must stay on H1 only, got %v", settings.ModePreferences)
			}
		}
	}
}

// An identity's settings are the engine defaults apart from the transport mode
// policy, which only the direct identity widens, and the auxiliary flag, which is
// the opt-in `h3 on`.
func TestPlatformTransportSettingsAreTheDefaultsExceptEligibility(t *testing.T) {
	t.Setenv("URNETWORK_H3", "")
	want := connect.DefaultPlatformTransportSettings()
	proxied := &connect.ProxySettings{Network: "tcp", Address: "127.0.0.1:1080"}
	for _, c := range []struct {
		name string
		got  *connect.PlatformTransportSettings
		h3   bool
	}{
		{"proxied", platformTransportSettingsFor(proxied, false), false},
		{"direct", platformTransportSettingsFor(nil, true), false},
	} {
		if c.got.EnableH3 != c.h3 || c.got.H3Port != want.H3Port ||
			c.got.ReconnectTimeout != want.ReconnectTimeout || c.got.PingTimeout != want.PingTimeout ||
			c.got.AuthTimeout != want.AuthTimeout || c.got.TransportBufferSize != want.TransportBufferSize {
			t.Fatalf("%s: settings differ from the engine defaults beyond eligibility: %+v vs %+v", c.name, c.got, want)
		}
	}
}

// The control key beats the environment in both directions, and an unset key
// falls back to the environment.
func TestResolveH3ControlKeyBeatsTheEnvironment(t *testing.T) {
	for _, c := range []struct {
		name string
		env  string
		key  string // "" = unset
		want bool
	}{
		{"unset, env off", "", "", false},
		{"unset, env on", "on", "", true},
		{"key on, env off", "", "on", true},
		{"key off, env on", "on", "off", false},
		{"key on, env on", "on", "on", true},
	} {
		t.Setenv("URNETWORK_H3", c.env)
		state := newControlState()
		if c.key != "" {
			if err := state.set("h3", c.key); err != nil {
				t.Fatalf("%s: set: %v", c.name, err)
			}
		}
		if got := resolveH3(state); got != c.want {
			t.Fatalf("%s: resolveH3=%v, want %v", c.name, got, c.want)
		}
	}
	if resolveH3(nil) != h3Enabled() {
		t.Fatal("a nil state must fall back to the environment")
	}
}

func restoreH3Gate(t *testing.T) {
	t.Helper()
	previous := connect.H3Enabled()
	t.Cleanup(func() { connect.SetH3Enabled(previous) })
}

// The h3 key reaches the running process: set applies at once, a persisted
// value is replayed at startup in both directions, and clear hands the
// decision back to the environment.
func TestH3ControlKeyDrivesTheLiveGate(t *testing.T) {
	restoreH3Gate(t)
	t.Setenv("URNETWORK_H3", "")

	if needsRestart("h3") {
		t.Fatal("h3 is live: needsRestart must be false")
	}
	for _, bad := range []string{"maybe", "", "-1", "true"} {
		if err := validateControlValue("h3", bad); err == nil {
			t.Fatalf("h3=%q must be rejected", bad)
		}
	}
	for _, ok := range []string{"on", "off", "ON", "direct", "all", "16"} {
		if err := validateControlValue("h3", ok); err != nil {
			t.Fatalf("h3=%q: %v", ok, err)
		}
	}

	if err := applyLiveSideEffect("h3", "on"); err != nil || !connect.H3Enabled() {
		t.Fatalf("set on: err=%v enabled=%v", err, connect.H3Enabled())
	}
	if err := applyLiveSideEffect("h3", "off"); err != nil || connect.H3Enabled() {
		t.Fatalf("set off: err=%v enabled=%v", err, connect.H3Enabled())
	}
	// direct is the automatic fallbacks without the beside-H1 auxiliary
	if err := applyLiveSideEffect("h3", "direct"); err != nil || connect.H3Enabled() {
		t.Fatalf("set direct: err=%v enabled=%v", err, connect.H3Enabled())
	}

	// clear returns to the environment, not to a hardcoded off
	t.Setenv("URNETWORK_H3", "on")
	if err := applyLiveDefault("h3"); err != nil || !connect.H3Enabled() {
		t.Fatalf("clear with env on: err=%v enabled=%v", err, connect.H3Enabled())
	}
	t.Setenv("URNETWORK_H3", "")
	if err := applyLiveDefault("h3"); err != nil || connect.H3Enabled() {
		t.Fatalf("clear with env off: err=%v enabled=%v", err, connect.H3Enabled())
	}

	// startup replay: a persisted off beats the env var, an unset key follows it
	state := newControlState()
	t.Setenv("URNETWORK_H3", "on")
	applyPersistedRuntimeTuning(state)
	if !connect.H3Enabled() {
		t.Fatal("unset key with env on: the gate must follow the environment at startup")
	}
	if err := state.set("h3", "off"); err != nil {
		t.Fatal(err)
	}
	applyPersistedRuntimeTuning(state)
	if connect.H3Enabled() {
		t.Fatal("a persisted off must beat URNETWORK_H3=on at startup")
	}
}

// h3_datagram is live and has no environment default: unset means off, a
// persisted on is replayed at startup, and clearing returns to off.
func TestH3DatagramControlKeyDrivesTheOffer(t *testing.T) {
	previous := connect.H3DatagramsEnabled()
	t.Cleanup(func() { connect.SetH3DatagramsEnabled(previous) })

	if needsRestart("h3_datagram") {
		t.Fatal("h3_datagram is live: needsRestart must be false")
	}
	for _, bad := range []string{"maybe", "", "1", "true"} {
		if err := validateControlValue("h3_datagram", bad); err == nil {
			t.Fatalf("h3_datagram=%q must be rejected", bad)
		}
	}
	if err := applyLiveSideEffect("h3_datagram", "on"); err != nil || !connect.H3DatagramsEnabled() {
		t.Fatalf("set on: err=%v enabled=%v", err, connect.H3DatagramsEnabled())
	}
	if err := applyLiveSideEffect("h3_datagram", "off"); err != nil || connect.H3DatagramsEnabled() {
		t.Fatalf("set off: err=%v enabled=%v", err, connect.H3DatagramsEnabled())
	}
	// clearing returns to the default, which is on as upstream
	if err := applyLiveDefault("h3_datagram"); err != nil || !connect.H3DatagramsEnabled() {
		t.Fatalf("clear: err=%v enabled=%v", err, connect.H3DatagramsEnabled())
	}

	state := newControlState()
	applyPersistedRuntimeTuning(state)
	if !connect.H3DatagramsEnabled() {
		t.Fatal("an unset key must leave the offer on (the default, as upstream)")
	}
	if err := state.set("h3_datagram", "on"); err != nil {
		t.Fatal(err)
	}
	applyPersistedRuntimeTuning(state)
	if !connect.H3DatagramsEnabled() {
		t.Fatal("a persisted on must be replayed at startup")
	}
	if err := state.set("h3_datagram", "off"); err != nil {
		t.Fatal(err)
	}
	applyPersistedRuntimeTuning(state)
	if connect.H3DatagramsEnabled() {
		t.Fatal("a persisted off must be replayed at startup")
	}
}

// h3_datagram_send is live and has no environment default: unset means off, a
// persisted on is replayed at startup in both directions, and clearing returns to
// off. It is read per message, so it needs no restart and no reconnect.
func TestH3DatagramSendControlKeyDrivesTheSendGate(t *testing.T) {
	previous := connect.H3DatagramSendEnabled()
	t.Cleanup(func() { connect.SetH3DatagramSendEnabled(previous) })

	if needsRestart("h3_datagram_send") {
		t.Fatal("h3_datagram_send is live: needsRestart must be false")
	}
	for _, bad := range []string{"maybe", "", "1", "true"} {
		if err := validateControlValue("h3_datagram_send", bad); err == nil {
			t.Fatalf("h3_datagram_send=%q must be rejected", bad)
		}
	}
	if err := applyLiveSideEffect("h3_datagram_send", "on"); err != nil || !connect.H3DatagramSendEnabled() {
		t.Fatalf("set on: err=%v enabled=%v", err, connect.H3DatagramSendEnabled())
	}
	if err := applyLiveSideEffect("h3_datagram_send", "off"); err != nil || connect.H3DatagramSendEnabled() {
		t.Fatalf("set off: err=%v enabled=%v", err, connect.H3DatagramSendEnabled())
	}
	// clearing returns to the default, which is on as upstream
	if err := applyLiveDefault("h3_datagram_send"); err != nil || !connect.H3DatagramSendEnabled() {
		t.Fatalf("clear: err=%v enabled=%v", err, connect.H3DatagramSendEnabled())
	}

	state := newControlState()
	applyPersistedRuntimeTuning(state)
	if !connect.H3DatagramSendEnabled() {
		t.Fatal("an unset key must leave datagram send on (the default, as upstream)")
	}
	if err := state.set("h3_datagram_send", "on"); err != nil {
		t.Fatal(err)
	}
	applyPersistedRuntimeTuning(state)
	if !connect.H3DatagramSendEnabled() {
		t.Fatal("a persisted on must be replayed at startup")
	}
	if err := state.set("h3_datagram_send", "off"); err != nil {
		t.Fatal(err)
	}
	applyPersistedRuntimeTuning(state)
	if connect.H3DatagramSendEnabled() {
		t.Fatal("a persisted off must be replayed at startup")
	}
}
