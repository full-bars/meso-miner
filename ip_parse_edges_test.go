package connect

import (
	"encoding/binary"
	"net"
	"testing"
)

// the malformed-input edges of the parse surfaces that consume untrusted
// bytes: the ipv6 extension walker, the icmp unreachable reader and
// ParseIpPathWithPayload. The walker's good-path suite lives in
// ip_ipv6_ext_test.go; this pins the length-field, truncation and terminal
// edges it does not, and adds a fuzz target whose seeds run as a plain unit
// test.

func TestWalkIpv6ExtensionHeadersMalformed(t *testing.T) {
	resetIcmpGate(t)
	udp := make([]byte, UdpHeaderSize+4)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))

	// an options header whose length field runs past the packet (the bytes
	// are physically absent; only the field is wrong)
	overrun := testIpv6Packet(testIpv6Src, testIpv6Dst, []testIpv6Ext{{kind: ipv6NextHeaderHopByHop}}, byte(ipProtocolNumberUdp), udp)
	overrun[Ipv6HeaderSize+1] = 200
	if _, ok := walkIpv6ExtensionHeaders(overrun); ok {
		t.Fatal("a length-field overrun was walked")
	}

	// an authentication header whose length field runs past the packet
	auth := testIpv6Packet(testIpv6Src, testIpv6Dst, []testIpv6Ext{{kind: ipv6NextHeaderAuthentication}}, byte(ipProtocolNumberUdp), udp)
	auth[Ipv6HeaderSize+1] = 200
	if _, ok := walkIpv6ExtensionHeaders(auth); ok {
		t.Fatal("an authentication length-field overrun was walked")
	}

	// a packet ending inside the fragment header
	fragment := testIpv6FragmentPacket(testIpv6Src, testIpv6Dst, false, byte(ipProtocolNumberUdp), 0, true, 9, make([]byte, 16))
	cut := append([]byte(nil), fragment[:Ipv6HeaderSize+4]...)
	binary.BigEndian.PutUint16(cut[4:6], 4)
	if _, ok := walkIpv6ExtensionHeaders(cut); ok {
		t.Fatal("a truncated fragment header was walked")
	}

	// shorter than the base header, and a v6-looking nibble only
	if _, ok := walkIpv6ExtensionHeaders(make([]byte, 20)); ok {
		t.Fatal("a packet shorter than the base header was walked")
	}

	// an options header with a zero length field is exactly 8 bytes and legal
	zero := testIpv6Packet(testIpv6Src, testIpv6Dst, []testIpv6Ext{{kind: ipv6NextHeaderHopByHop}}, byte(ipProtocolNumberUdp), udp)
	walk, ok := walkIpv6ExtensionHeaders(zero)
	if !ok || walk.nextHeader != ipProtocolNumberUdp || walk.transportOffset != Ipv6HeaderSize+8 {
		t.Fatalf("zero-length options header = %+v, %t", walk, ok)
	}

	// the mobility, host-identity and shim6 kinds are walked like the rest
	for _, kind := range []byte{ipv6NextHeaderMobility, ipv6NextHeaderHostIdentity, ipv6NextHeaderShim6} {
		packet := testIpv6Packet(testIpv6Src, testIpv6Dst, []testIpv6Ext{{kind: kind}}, byte(ipProtocolNumberUdp), udp)
		walk, ok := walkIpv6ExtensionHeaders(packet)
		if !ok || walk.nextHeader != ipProtocolNumberUdp || walk.transportOffset != Ipv6HeaderSize+8 {
			t.Fatalf("kind %d walk = %+v, %t", kind, walk, ok)
		}
	}
}

// ParseIpPathWithPayload's own rejection contract for malformed v6 input:
// truncated, over-declared, or not version 6 at all.
func TestParseIpPathRejectsMalformedV6(t *testing.T) {
	resetIcmpGate(t)
	truncated := make([]byte, 20)
	truncated[0] = 0x60
	overDeclared := make([]byte, Ipv6HeaderSize+4)
	overDeclared[0] = 0x60
	binary.BigEndian.PutUint16(overDeclared[4:6], 40)
	badVersion := make([]byte, Ipv6HeaderSize)
	badVersion[0] = 0x70
	for name, packet := range map[string][]byte{
		"truncated":     truncated,
		"over declared": overDeclared,
		"bad version":   badVersion,
	} {
		if _, _, err := ParseIpPathWithPayload(packet); err == nil {
			t.Fatalf("%s: the packet parsed", name)
		}
	}
}

// the walker is now consulted by ParseIpPath via parseIpv6 (T30), so the
// held upstream test TestParseIpPathIpv6ExtensionHeaders replaces the T27
// placeholder that named this gap.

// the seeds run under plain `go test`; `go test -fuzz=FuzzIpParseSurfaces`
// explores further. Invariants: nothing panics, a walked chain stays inside
// the packet, and a parsed payload cannot be longer than its input.
func FuzzIpParseSurfaces(f *testing.F) {
	udp := make([]byte, UdpHeaderSize+4)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	f.Add([]byte{})
	f.Add(make([]byte, 20))
	f.Add(testIpv6Packet(testIpv6Src, testIpv6Dst, nil, byte(ipProtocolNumberUdp), udp))
	f.Add(testIpv6Packet(testIpv6Src, testIpv6Dst, testIpv6ChainAll, byte(ipProtocolNumberUdp), udp))
	f.Add(testIpv6FragmentPacket(testIpv6Src, testIpv6Dst, true, byte(ipProtocolNumberUdp), 0, false, 0x11223344, udp))
	f.Add(testIpv6FragmentPacket(testIpv6Src, testIpv6Dst, false, byte(ipProtocolNumberUdp), 8, true, 7, make([]byte, 16)))
	{
		path := &IpPath{Version: 4, Protocol: IpProtocolUdp, SourceIp: net.ParseIP("10.0.0.9").To4(), SourcePort: 42001, DestinationIp: net.ParseIP("203.0.113.7").To4(), DestinationPort: 443}
		// a real pool packet as a seed; the corpus copies it, then the
		// buffer returns to the pool
		seed := ipOosUdpPacket(path, []byte("seed"))
		f.Add(seed)
		MessagePoolReturn(seed)
	}
	{
		path := &IpPath{Version: 6, Protocol: IpProtocolTcp, SequenceNumber: 7, SourceIp: net.ParseIP("fd00::9"), SourcePort: 42001, DestinationIp: net.ParseIP("2001:db8::7"), DestinationPort: 443}
		signal, ok := ipOosUnreachable(path)
		if !ok {
			f.Fatal("the unreachable seed builder failed")
		}
		f.Add(signal)
		MessagePoolReturn(signal)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		// T30: outside the v6 extension-chain family the wiring must be
		// invisible for v6, and for non-v6 bytes the new code must never
		// accept what the pre-wiring reference rejected. legacyParseIpv6
		// (see the differential test file) is that reference.
		{
			version6 := len(packet) >= Ipv6HeaderSize && packet[0]>>4 == 6
			if !(version6 && isIpv6ExtensionHeader(packet[6])) {
				legacyProtocol, _, _, _, legacyOk := legacyParseIpv6(packet)
				newProtocol, _, _, _, newOk := parseIpv6(packet)
				if version6 {
					if legacyOk != newOk || (legacyOk && legacyProtocol != newProtocol) {
						t.Fatalf("parseIpv6 diverged from the pre-wiring reference")
					}
				} else if newOk && !legacyOk {
					t.Fatalf("parseIpv6 accepted bytes the pre-wiring reference rejected")
				}
			}
		}
		if walk, ok := walkIpv6ExtensionHeaders(packet); ok {
			if walk.payloadEnd > len(packet) ||
				walk.transportOffset > walk.payloadEnd ||
				walk.transportOffset < Ipv6HeaderSize {
				t.Fatalf("walk out of bounds: %+v over %d bytes", walk, len(packet))
			}
			if isIpv6ExtensionHeader(byte(walk.nextHeader)) && !walk.fragmented {
				// a non-atomic fragment's next header byte is the start of
				// the fragmentable part, unvalidated by design
				t.Fatalf("the walk stopped on an extension header: %+v", walk)
			}
		}
		ipv6TransportOffset(packet)
		isIpFragmentPacket(packet)
		ipParseIcmpUnreachable(packet)
		if ipPath, payload, err := ParseIpPathWithPayload(packet); err == nil {
			if len(payload) > len(packet) {
				t.Fatalf("payload %d bytes exceeds the %d byte input", len(payload), len(packet))
			}
			if ipPath != nil && ipPath.Protocol != IpProtocolTcp && ipPath.Protocol != IpProtocolUdp {
				t.Fatalf("parsed path has protocol %v", ipPath.Protocol)
			}
		}
	})
}
