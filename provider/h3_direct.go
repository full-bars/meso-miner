package main

import (
	"os"
	"strings"
	"time"

	"github.com/urnetwork/connect"
)

// h3Enabled reports whether the operator opted in to H3 (QUIC) with
// URNETWORK_H3. It is OFF unless the variable is on, 1, true or yes. This is
// only the startup default for the runtime gate: the h3 control key overrides it
// and can be changed without a restart (see resolveH3).
func h3Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("URNETWORK_H3"))) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// resolveH3 is whether H3 should be running: the h3 control key when it has
// been set, otherwise URNETWORK_H3.
func resolveH3(state *controlState) bool {
	if state != nil {
		if v, ok := state.get("h3"); ok {
			return strings.EqualFold(v, "on")
		}
	}
	return h3Enabled()
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// h3Eligible reports whether this identity may ever run H3: no proxy involved
// at all.
//
// Stage A is the direct (non-proxied) identity only: runH3 opens a UDP socket on
// the host, which is the right socket for an identity that reaches the platform
// from the host's own address and the wrong one for a proxied identity, whose
// QUIC would leave from the host instead of its proxy. A proxied identity never
// gets H3 from the switch. Eligible is not running: the runtime gate
// (connect.SetH3Enabled, driven by the h3 control key or URNETWORK_H3) decides
// whether an eligible identity actually dials.
func h3Eligible(proxySettings *connect.ProxySettings, isNative bool) bool {
	return isNative && proxySettings == nil
}

// platformTransportSettingsFor returns the platform transport settings for an
// identity under the current h3 setting (see h3Mode): the engine defaults plus the
// transport mode policy. The direct identity gets the fallbacks in every mode but
// off, and is the only one that can be the beside-H1 auxiliary (EnableH3). A
// proxied identity gets them only when the setting admits it, and then its QUIC
// is relayed through its proxy (H3PacketConnFactory), never opened on the host.
func platformTransportSettingsFor(proxySettings *connect.ProxySettings, isNative bool) *connect.PlatformTransportSettings {
	settings := connect.DefaultPlatformTransportSettings()
	applyProviderDialBackoff(settings)
	setting := resolveH3Mode(globalControlState, currentProxyResolution())
	direct := h3Eligible(proxySettings, isNative)
	proxyEligible := false
	if proxySettings != nil {
		proxyEligible = h3ProxyEligible(setting, proxySettings.Key())
		recordH3Launch(proxySettings.Key(), proxyEligible)
		if proxyEligible {
			settings.H3PacketConnFactory = newH3PacketConnFactory(proxySettings)
		}
	}
	settings.ModePreferences = h3ModePreferences(setting, direct, proxyEligible)
	settings.EnableH3 = direct && setting.Mode == h3ModeOn
	return settings
}

// applyProviderDialBackoff gives an identity whose dials keep failing a second,
// longer backoff tier. A provider runs hundreds of proxy identities and many of
// them stay dead for hours; at the 60 s cap each one still redials every
// ~45 s, a full TLS handshake with post-quantum key generation apiece (measured
// 2026-10-15: ~5.8 H1+ attempts per second, 96% failing, on a 1,100 proxy node).
// After 10 consecutive failures while the platform is reachable, an identity
// waits up to 10 minutes between dials. Any success, network change or operator
// toggle resets it, and a platform outage never enters it.
// URNETWORK_DIAL_LONG_BACKOFF=off restores the single 60 s cap.
func applyProviderDialBackoff(settings *connect.PlatformTransportSettings) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("URNETWORK_DIAL_LONG_BACKOFF")), "off") {
		return
	}
	settings.ReconnectLongAfter = 10
	settings.ReconnectLongMaxTimeout = 10 * time.Minute
}
