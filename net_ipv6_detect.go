package connect

import (
	"net"
	"time"
)

// hostHasGlobalIpv6 reports whether this host can reach the internet over IPv6.
// It is a variable so tests can pin it. A UDP "connect" sends no packet: it
// succeeds only if the kernel has a route, and the source address it picks tells
// whether that route is a real global address (a Tailscale or other ULA/private
// address does not count, nor does a link-local one).
var hostHasGlobalIpv6 = detectGlobalIpv6

func detectGlobalIpv6() bool {
	c, err := net.DialTimeout("udp6", "[2606:4700:4700::1111]:53", 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	la, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || la.IP == nil || la.IP.To4() != nil {
		return false
	}
	return la.IP.IsGlobalUnicast() && !la.IP.IsPrivate()
}

// defaultDohIpVersion returns 0 (both families) when the host has global IPv6
// and 4 otherwise, so an IPv4-only host behaves exactly as it always did while a
// dual-stack host can still resolve when its IPv4 path to the resolvers fails.
func defaultDohIpVersion() int {
	if hostHasGlobalIpv6() {
		return 0
	}
	return 4
}
