package connect

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// T30's change must not move a single outcome for any packet that is not a v6
// packet WITH an extension header. legacyParseIpv6 is the body of parseIpv6
// before the walker wiring, frozen here as the reference: this differential
// test asserts the two agree on a table, and FuzzIpParseSurfaces asserts the
// same on every input it generates outside the extension-chain family.

func legacyParseIpv6(ipPacket []byte) (ipProtocol ipProtocolNumber, sourceIp net.IP, destinationIp net.IP, transport []byte, ok bool) {
	if len(ipPacket) < Ipv6HeaderSize {
		return
	}
	payloadByteCount := int(binary.BigEndian.Uint16(ipPacket[4:6]))
	if len(ipPacket) < Ipv6HeaderSize+payloadByteCount {
		return
	}
	ipProtocol = ipProtocolNumber(ipPacket[6])
	sourceIp = net.IP(ipPacket[8:24])
	destinationIp = net.IP(ipPacket[24:40])
	transport = ipPacket[Ipv6HeaderSize : Ipv6HeaderSize+payloadByteCount]
	ok = true
	return
}

func assertParseAgrees(t *testing.T, name string, packet []byte) {
	t.Helper()
	legacyProtocol, legacySource, legacyDestination, legacyTransport, legacyOk := legacyParseIpv6(packet)
	newProtocol, newSource, newDestination, newTransport, newOk := parseIpv6(packet)
	version6 := len(packet) >= Ipv6HeaderSize && packet[0]>>4 == 6
	if version6 && isIpv6ExtensionHeader(packet[6]) {
		// the extension family is the change itself; the held parse tests
		// cover it
		return
	}
	if version6 {
		// the strong guarantee: for a v6 packet without an extension chain
		// the walker wiring is invisible
		if legacyOk != newOk {
			t.Fatalf("%s: ok = %v, legacy %v", name, newOk, legacyOk)
		}
		if !legacyOk {
			return
		}
		if legacyProtocol != newProtocol || !bytes.Equal(legacySource, newSource) ||
			!bytes.Equal(legacyDestination, newDestination) || !bytes.Equal(legacyTransport, newTransport) {
			t.Fatalf("%s: parseIpv6 diverged", name)
		}
		return
	}
	// non-v6 bytes never reach parseIpv6 in production (every caller gates on
	// the version first); the guarantee there is that the new code never
	// accepts what the reference rejected. The reference itself would parse
	// some v4 garbage the version guard now rejects, which is why it is only
	// this one-way check.
	if newOk && !legacyOk {
		t.Fatalf("%s: parseIpv6 accepted bytes the reference rejected", name)
	}
}

func TestParseIpv6DifferentialNoExtensionHeaders(t *testing.T) {
	udp := make([]byte, UdpHeaderSize+4)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	udpPlain := testIpv6Packet(testIpv6Src, testIpv6Dst, nil, byte(ipProtocolNumberUdp), udp)

	overDeclared := testIpv6Packet(testIpv6Src, testIpv6Dst, nil, byte(ipProtocolNumberUdp), udp)
	binary.BigEndian.PutUint16(overDeclared[4:6], uint16(len(overDeclared)+1))

	table := map[string][]byte{
		"plain udp":     udpPlain,
		"plain tcp syn": testIpv6Tcp(testIpv6Src, testIpv6Dst, nil, 40002, 443, tcpFlagSyn, 7, nil),
		"short":         udpPlain[:Ipv6HeaderSize+4],
		"over declared": overDeclared,
		"nil":           nil,
		"short v4":      testingUdp4Packet("10.0.0.1", "203.0.113.7", 53, nil),
		"long v4":       testingUdp4Packet("10.0.0.1", "203.0.113.7", 53, make([]byte, 40)),
	}
	for name, packet := range table {
		assertParseAgrees(t, name, packet)
	}
}
