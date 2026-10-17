package connect

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

// the dial-failure answer bytes. Anything here that a source's own stack
// consumes is discarded when the wire checksums are wrong, and no round-trip
// parse in the suite would catch that. These re-sum every checksummed region
// to zero, the same property the tcp and udp checksum tests use, for both
// families and both answer shapes (the icmp unreachable for a capacity
// failure, the rst for a refused connect).

func assertRegionChecksum(t *testing.T, what string, region []byte) {
	t.Helper()
	if sum := checksumFinish(checksumAdd(0, region)); sum != 0 {
		t.Fatalf("%s: checksum does not re-sum to zero (0x%04x)", what, sum)
	}
}

func packetTransportOffset(packet []byte) int {
	if packet[0]>>4 == 4 {
		return int(packet[0]&0x0f) * 4
	}
	return Ipv6HeaderSize
}

// transportChecksumFromPacket re-sums a transport region with the pseudo
// header built from the packet's own addresses: zero means the checksum that
// travels on the wire is correct.
func transportChecksumFromPacket(packet []byte, protocol ipProtocolNumber) uint16 {
	transport := packet[packetTransportOffset(packet):]
	if packet[0]>>4 == 4 {
		return transportChecksum(protocol, net.IP(packet[12:16]), net.IP(packet[16:20]), transport)
	}
	return transportChecksum(protocol, net.IP(packet[8:24]), net.IP(packet[24:40]), transport)
}

func TestDialFailureUnreachableBytesChecksum(t *testing.T) {
	for _, version := range []int{4, 6} {
		for _, protocol := range []IpProtocol{IpProtocolTcp, IpProtocolUdp} {
			path := dialFailureTestPath(protocol, version)
			packet, ok := ipOosUnreachable(path)
			if !ok {
				t.Fatalf("v%d %s: no unreachable was built", version, protocol)
			}
			if int(packet[0]>>4) != version {
				t.Fatalf("v%d %s: the packet is version %d", version, protocol, packet[0]>>4)
			}
			if version == 4 {
				assertRegionChecksum(t, "ipv4 header", packet[:packetTransportOffset(packet)])
				assertRegionChecksum(t, "icmpv4 message", packet[packetTransportOffset(packet):])
			} else {
				if sum := transportChecksumFromPacket(packet, ipProtocolNumberIcmp6); sum != 0 {
					t.Fatalf("v6 %s: the icmpv6 checksum does not re-sum to zero (0x%04x)", protocol, sum)
				}
				declared := int(binary.BigEndian.Uint16(packet[4:6]))
				if declared != len(packet)-Ipv6HeaderSize {
					t.Fatalf("v6 %s: declared payload length %d does not match %d bytes", protocol, declared, len(packet)-Ipv6HeaderSize)
				}
			}
			// the embedded original datagram is present: header plus at
			// least the 8 rfc 792 transport bytes
			body := packet[packetTransportOffset(packet):]
			if len(body) <= icmpUnreachableHeaderSize {
				t.Fatalf("v%d %s: no embedded datagram", version, protocol)
			}
		}
	}
}

func TestDialFailureUnreachableFromClassifierChecksum(t *testing.T) {
	// the classifier is the single path a provider takes; the packet it
	// returns for a capacity failure must be the same valid one
	for _, version := range []int{4, 6} {
		path := dialFailureTestPath(IpProtocolTcp, version)
		action, packet := classifyDialFailure(path, errors.New("test: no route to host"), 0)
		if action != dialFailureUnreachable || packet == nil {
			t.Fatalf("v%d: a capacity failure classified as %v", version, action)
		}
		if version == 4 {
			assertRegionChecksum(t, "ipv4 header", packet[:packetTransportOffset(packet)])
			assertRegionChecksum(t, "icmpv4 message", packet[packetTransportOffset(packet):])
		} else if sum := transportChecksumFromPacket(packet, ipProtocolNumberIcmp6); sum != 0 {
			t.Fatalf("v%d: the icmpv6 checksum does not re-sum to zero (0x%04x)", version, sum)
		}
	}
}

func TestDialFailureRstBytesChecksum(t *testing.T) {
	const sequenceNumber = 0x12345678
	for _, version := range []int{4, 6} {
		path := dialFailureTestPath(IpProtocolTcp, version)
		packet, ok := ipOosRstSequence(path, sequenceNumber)
		if !ok {
			t.Fatalf("v%d: no rst was built", version)
		}
		transport := packet[packetTransportOffset(packet):]
		if sum := transportChecksumFromPacket(packet, ipProtocolNumberTcp); sum != 0 {
			t.Fatalf("v%d: the tcp checksum does not re-sum to zero (0x%04x)", version, sum)
		}
		if version == 4 {
			assertRegionChecksum(t, "ipv4 header", packet[:packetTransportOffset(packet)])
		}
		if transport[13]&0x04 == 0 {
			t.Fatalf("v%d: the reset flag is not set", version)
		}
		if sequenceNumber != binary.BigEndian.Uint32(transport[4:8]) {
			t.Fatalf("v%d: the reset carries sequence 0x%08x, want 0x%08x", version, binary.BigEndian.Uint32(transport[4:8]), sequenceNumber)
		}
	}
}

// the control: a single corrupted byte must fail the re-sum, so the checks
// above are reading the real wire field and not something inert
func TestDialFailureBytesChecksumDetectsCorruption(t *testing.T) {
	for _, version := range []int{4, 6} {
		path := dialFailureTestPath(IpProtocolTcp, version)
		packet, _ := ipOosUnreachable(path)
		corrupted := make([]byte, len(packet))
		copy(corrupted, packet)
		corrupted[len(corrupted)-1] ^= 0x01
		body := corrupted[packetTransportOffset(corrupted):]
		var sum uint16
		if version == 4 {
			sum = checksumFinish(checksumAdd(0, body))
		} else {
			sum = transportChecksumFromPacket(corrupted, ipProtocolNumberIcmp6)
		}
		if sum == 0 {
			t.Fatalf("v%d: a corrupted packet still re-summed to zero", version)
		}
	}
}
