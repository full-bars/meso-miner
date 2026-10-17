package connect

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// RFC 8200 wire values, kept literal so this reproduction compiles against
// the pre-fix tree as well.
const (
	testIpv6FragmentHeaderNextHeader = 44
	testIpv6FragmentHeaderSize       = 8
)

// Oversized IPv6 UDP replies used to be split into several independent
// datagrams, each with its own UDP header, which corrupts application
// framing (QUIC rejects them and large EDNS answers cannot be parsed once
// the payload exceeds mtu - IPv6 header - UDP header, 1392 bytes at the
// default 1440 mtu). An oversized datagram must instead be fragmented at
// the IP layer: one UDP header, one identification, RFC 8200 fragment
// headers, reassembled by the source stack.
func TestStreamStateDataPacketsFragmentsOversizedIpv6Datagram(t *testing.T) {
	stream := newTestStreamState(6)
	payload := make([]byte, 1393)
	for i := range payload {
		payload[i] = byte(i)
	}

	// the full datagram is the unfragmented frame the sender meant to send
	full := stream.udpPacket(payload)
	ipPayload := full[Ipv6HeaderSize:]

	packets, err := stream.DataPackets(payload, len(payload), DefaultMtu)
	if err != nil {
		t.Fatalf("DataPackets: %s", err)
	}
	if len(packets) != 2 {
		t.Fatalf("a %d byte payload over a %d mtu must arrive as 2 IP fragments (1392 + 9 bytes of ip payload), got %d packets", len(payload), DefaultMtu, len(packets))
	}

	var identification uint32
	for i, packet := range packets {
		if len(packet) > DefaultMtu {
			t.Fatalf("fragment %d is %d bytes, over the mtu", i, len(packet))
		}
		if packet[0]>>4 != 6 {
			t.Fatalf("fragment %d is not IPv6", i)
		}
		if packet[6] != testIpv6FragmentHeaderNextHeader {
			t.Fatalf("fragment %d carries next header %d, want the fragment header (%d); an oversized UDP datagram must not be split into several independent datagrams", i, packet[6], testIpv6FragmentHeaderNextHeader)
		}
		fragmentHeader := packet[Ipv6HeaderSize : Ipv6HeaderSize+testIpv6FragmentHeaderSize]
		if fragmentHeader[0] != byte(ipProtocolNumberUdp) {
			t.Fatalf("fragment %d resumes header %d, want udp", i, fragmentHeader[0])
		}
		id := binary.BigEndian.Uint32(fragmentHeader[4:8])
		if id == 0 {
			t.Fatalf("fragment %d has a zero identification", i)
		}
		if i == 0 {
			identification = id
		} else if id != identification {
			t.Fatalf("fragment %d identification %d differs from the first fragment's %d", i, id, identification)
		}
	}

	if flags := binary.BigEndian.Uint16(packets[0][Ipv6HeaderSize+2 : Ipv6HeaderSize+4]); flags != 0x1 {
		t.Fatalf("first fragment offset/flags = %#04x, want offset 0 with the more-fragments flag", flags)
	}
	if flags := binary.BigEndian.Uint16(packets[1][Ipv6HeaderSize+2 : Ipv6HeaderSize+4]); flags != 1392 {
		t.Fatalf("second fragment offset/flags = %#04x, want the byte offset 1392 with no more fragments", flags)
	}

	// the fragmentable parts together are the original ip payload, so the
	// source stack reassembles exactly the datagram the sender produced
	joined := append([]byte(nil), packets[0][Ipv6HeaderSize+testIpv6FragmentHeaderSize:]...)
	joined = append(joined, packets[1][Ipv6HeaderSize+testIpv6FragmentHeaderSize:]...)
	if !bytes.Equal(joined, ipPayload) {
		t.Fatalf("reassembled ip payload differs from the datagram (len %d vs %d)", len(joined), len(ipPayload))
	}

	// a payload that still fits one datagram keeps its single frame
	small := make([]byte, 1392)
	packets, err = stream.DataPackets(small, len(small), DefaultMtu)
	if err != nil {
		t.Fatalf("DataPackets (boundary): %s", err)
	}
	if len(packets) != 1 {
		t.Fatalf("a %d byte payload fits one datagram, got %d packets", len(small), len(packets))
	}
	if packets[0][6] != byte(ipProtocolNumberUdp) {
		t.Fatalf("the unfragmented boundary packet carries next header %d, want udp", packets[0][6])
	}

	// far over the mtu still fragments cleanly into more than two pieces
	big := make([]byte, 4000)
	packets, err = stream.DataPackets(big, len(big), DefaultMtu)
	if err != nil {
		t.Fatalf("DataPackets (big): %s", err)
	}
	if len(packets) < 3 {
		t.Fatalf("a %d byte payload must fragment into at least 3 pieces, got %d", len(big), len(packets))
	}
}

// The identification must be nonzero and unpredictable: a counter would let an
// attacker who can trigger fragmented replies guess the next one.
func TestIpv6FragmentIdentificationIsUnpredictableAndNonzero(t *testing.T) {
	const count = 256
	ids := make([]uint32, count)
	sequential := 0
	for i := range ids {
		ids[i] = nextIpv6FragmentIdentification()
		if ids[i] == 0 {
			t.Fatal("zero identification")
		}
		if 0 < i && ids[i] == ids[i-1]+1 {
			sequential++
		}
	}
	if sequential == count-1 {
		t.Fatalf("every identification followed the previous by one: the counter is predictable")
	}
	seen := map[uint32]bool{}
	dup := 0
	for _, id := range ids {
		if seen[id] {
			dup++
		}
		seen[id] = true
	}
	if 1 < dup {
		t.Fatalf("%d repeated identifications in %d draws", dup, count)
	}
}

// A payload above the datagram limit is refused even when the mtu is large enough
// that it would otherwise take the single-packet branch.
func TestStreamStateDataPacketsRefusesAnUnrepresentableIpv6Datagram(t *testing.T) {
	stream := newTestStreamState(6)
	payload := make([]byte, 65528)
	if _, err := stream.DataPackets(payload, len(payload), 70000); err == nil {
		t.Fatal("a 65528 byte udp payload over a 70000 mtu was packetized instead of refused")
	}
	if _, err := stream.DataPackets(payload, len(payload), DefaultMtu); err == nil {
		t.Fatal("a 65528 byte udp payload was fragmented instead of refused")
	}
	// the limit itself is representable
	ok := make([]byte, 65527)
	if _, err := stream.DataPackets(ok, len(ok), DefaultMtu); err != nil {
		t.Fatalf("a 65527 byte payload is the largest representable one but got %v", err)
	}
}
