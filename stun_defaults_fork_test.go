package connect

import (
	"strings"
	"testing"
	"time"
)

// The fork's old fixed list carried servers that no longer answer (openrelay
// times out on both ports, stunprotocol no longer resolves), so every ICE
// gather waited on them. The defaults must never offer them again, must bound
// the gather wait, and the health probe must cover the whole sampled pool.
func TestDefaultWebRtcSettingsOmitDeadStunServersAndBoundTheGather(t *testing.T) {
	settings := DefaultWebRtcSettings()
	if len(settings.IceServerUrls) != 0 {
		t.Fatalf("the default must sample the pool, not pin servers: %v", settings.IceServerUrls)
	}
	for _, url := range settings.IceServerPoolUrls {
		if strings.Contains(url, "openrelay.metered.ca") || strings.Contains(url, "stunprotocol.org") {
			t.Fatalf("a known-dead STUN server is back in the pool: %s", url)
		}
	}
	if settings.StunGatherTimeout <= 0 || settings.StunGatherTimeout > 2*time.Second {
		t.Fatalf("the gather wait must be bounded to 2 s, got %v", settings.StunGatherTimeout)
	}
	for i := 0; i < 50; i += 1 {
		for _, url := range settings.selectIceServerUrls() {
			if strings.Contains(url, "openrelay") || strings.Contains(url, "stunprotocol") {
				t.Fatalf("a session drew a dead server: %s", url)
			}
		}
	}
}

func TestStunProbeCoversTheWholePoolWhenNothingIsPinned(t *testing.T) {
	settings := DefaultWebRtcSettings()
	endpoints := stunProbeEndpoints(settings)
	covered := map[string]bool{}
	for _, url := range endpoints {
		host, port := stunEndpoint(url)
		covered[host+":"+port] = true
	}
	for _, url := range settings.IceServerPoolUrls {
		host, port := stunEndpoint(url)
		if !covered[host+":"+port] {
			t.Fatalf("pool server %s is not probed", url)
		}
	}
	pinned := DefaultWebRtcSettings()
	pinned.IceServerUrls = []string{"stun:pinned.example:3478"}
	for _, url := range stunProbeEndpoints(pinned) {
		if strings.Contains(url, "stun.l.google.com") {
			t.Fatalf("a pinned override must replace the pool in the probe too: %s", url)
		}
	}
}
