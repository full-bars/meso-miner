package connect

import (
	mathrand "math/rand"
	"net"
)

// Address helpers shared by the packet translation and queue tests. They are
// the same helpers upstream keeps beside its dual stack transport tests.

// testLoopbackHost is the loopback literal in url-host form: v6 is bracketed.
func testLoopbackHost(ipVersion int) string {
	if ipVersion == 6 {
		return "[::1]"
	}
	return "127.0.0.1"
}

// testLoopbackUdpAddr is an unbound (port 0) loopback udp address for the
// version.
func testLoopbackUdpAddr(ipVersion int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(testLoopbackIp(ipVersion)), Port: 0}
}

// testUnspecifiedIp is the wildcard address for the version.
func testUnspecifiedIp(ipVersion int) net.IP {
	if ipVersion == 6 {
		return net.IPv6zero
	}
	return net.IPv4zero
}

// testIndexedIp is the address whose last byte is `index` in the version's
// zero network (0.0.0.index or ::index): a distinct, stable peer key.
func testIndexedIp(ipVersion int, index byte) net.IP {
	ip := testUnspecifiedIp(ipVersion)
	ip = append(net.IP(nil), ip...)
	ip[len(ip)-1] = index
	return ip
}

// testRandomIp draws a random address of the version, for tests that key
// state by peer address.
func testRandomIp(ipVersion int) net.IP {
	ip := make(net.IP, net.IPv4len)
	if ipVersion == 6 {
		ip = make(net.IP, net.IPv6len)
	}
	mathrand.Read(ip)
	return ip
}
