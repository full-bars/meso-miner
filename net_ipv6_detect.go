package connect

import (
	"net"
	"sync"
	"time"
)

// hostHasGlobalIpv6 reports whether this host can really reach the internet over
// IPv6. It is a variable so tests can pin it. A route and a global source address
// are not enough (a VPS can have both and a filtered upstream), so the check
// sends one tiny DNS query to a public resolver and needs an answer. The result
// is cached, so DefaultDohSettings and dnsResolverAddrs can call it freely.
var hostHasGlobalIpv6 = cachedGlobalIpv6

const ipv6DetectTtl = 10 * time.Minute

var ipv6Detect struct {
	mu      sync.Mutex
	at      time.Time
	has     bool
	checked bool
}

func cachedGlobalIpv6() bool {
	ipv6Detect.mu.Lock()
	defer ipv6Detect.mu.Unlock()
	if ipv6Detect.checked && time.Since(ipv6Detect.at) < ipv6DetectTtl {
		return ipv6Detect.has
	}
	ipv6Detect.has = detectGlobalIpv6()
	ipv6Detect.at = time.Now()
	ipv6Detect.checked = true
	return ipv6Detect.has
}

// dnsProbeQuery is a minimal query for the root NS records.
var dnsProbeQuery = []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01}

func detectGlobalIpv6() bool {
	c, err := net.DialTimeout("udp6", "[2606:4700:4700::1111]:53", 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	la, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || la.IP == nil || la.IP.To4() != nil || !la.IP.IsGlobalUnicast() || la.IP.IsPrivate() {
		return false
	}
	// a Tailscale or other ULA/private address does not count, nor does a
	// link-local one; and a global one only counts if an answer comes back
	c.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, err := c.Write(dnsProbeQuery); err != nil {
		return false
	}
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	return err == nil && 2 <= n && buf[0] == 0x12 && buf[1] == 0x34
}

// defaultDohIpVersion returns 0 (both families) when the host has working global
// IPv6 and 4 otherwise, so an IPv4-only host behaves exactly as it always did
// while a dual-stack host can still resolve when its IPv4 path to the resolvers
// fails.
func defaultDohIpVersion() int {
	if hostHasGlobalIpv6() {
		return 0
	}
	return 4
}
