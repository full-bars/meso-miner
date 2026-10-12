package connect

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

func withHostIpv6(t *testing.T, has bool) {
	old := hostHasGlobalIpv6
	hostHasGlobalIpv6 = func() bool { return has }
	t.Cleanup(func() { hostHasGlobalIpv6 = old })
}

func TestDohIpVersionFollowsHostIpv6(t *testing.T) {
	withHostIpv6(t, true)
	if v := DefaultDohSettings().IpVersion; v != 0 {
		t.Fatalf("dual-stack host: IpVersion %d, want 0", v)
	}
	withHostIpv6(t, false)
	if v := DefaultDohSettings().IpVersion; v != 4 {
		t.Fatalf("IPv4-only host must keep IpVersion 4, got %d", v)
	}
}

func TestDefaultIpv6DohServersAreWellFormed(t *testing.T) {
	rs := DefaultDnsResolverSettings()
	if len(rs.RemoteDohServersIpv6) != len(rs.RemoteDohServersIpv4) {
		t.Fatalf("each operator needs a v6 endpoint: %d v4, %d v6", len(rs.RemoteDohServersIpv4), len(rs.RemoteDohServersIpv6))
	}
	for _, s := range rs.RemoteDohServersIpv6 {
		u, err := url.Parse(s.Url)
		if err != nil || u.Scheme != "https" {
			t.Fatalf("bad v6 doh url %q: %v", s.Url, err)
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil || ip.To4() != nil {
			t.Fatalf("v6 doh server %q must be an IPv6 literal", s.Url)
		}
		if s.Format != DohFormatJson && s.Format != DohFormatWire {
			t.Fatalf("v6 doh server %q has no format", s.Url)
		}
	}
	for _, a := range rs.LocalDnsIpv6 {
		if ip := net.ParseIP(a); ip == nil || ip.To4() != nil {
			t.Fatalf("local v6 dns %q is not IPv6", a)
		}
	}
}

func TestDualStackSettingsConsultBothFamilies(t *testing.T) {
	withHostIpv6(t, true)
	st := DefaultDohSettings()
	servers := remoteDohServers(st, st.IpVersion)
	var v4, v6 int
	for _, s := range servers {
		if strings.Contains(s.Url, "[") {
			v6++
		} else {
			v4++
		}
	}
	if v4 == 0 || v6 == 0 {
		t.Fatalf("dual-stack must use both families, got v4=%d v6=%d", v4, v6)
	}
	addrs := dnsResolverAddrs(st, false, "")
	var has6 bool
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() == nil {
			has6 = true
		}
	}
	if !has6 {
		t.Fatalf("local dns addrs missing IPv6: %v", addrs)
	}
	// a v4-only host must not see any v6 server
	withHostIpv6(t, false)
	st = DefaultDohSettings()
	for _, s := range remoteDohServers(st, st.IpVersion) {
		if strings.Contains(s.Url, "[") {
			t.Fatalf("IPv4-only host would consult a v6 doh server: %s", s.Url)
		}
	}
}

func TestDetectGlobalIpv6DoesNotPanicOrBlock(t *testing.T) {
	_ = detectGlobalIpv6() // result depends on the host; it must return promptly
}
