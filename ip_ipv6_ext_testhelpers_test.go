// ip_ipv6_ext_testhelpers_test.go — packet builders for the ported ipv6 tests.
//
// These are upstream connect's own test helpers, copied verbatim from the files
// it keeps them in: testingUdp4Packet from ip_block_action_test.go, and
// testingIcmp6EchoPacket / testingIcmp6EchoTypePacket from ip_icmp_test.go.
// None of those files exists on this line, so the helpers are carried here
// instead and the ported test files use them unedited.
//
// test-only: nothing in the production build references this file.
package connect

import (
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func testingUdp4Packet(sourceIp string, destinationIp string, destinationPort int, payload []byte) []byte {
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		SrcIP:    net.ParseIP(sourceIp).To4(),
		DstIP:    net.ParseIP(destinationIp).To4(),
		Protocol: layers.IPProtocolUDP,
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(40000),
		DstPort: layers.UDPPort(destinationPort),
	}
	udp.SetNetworkLayerForChecksum(ip)
	buffer := gopacket.NewSerializeBuffer()
	err := gopacket.SerializeLayers(
		buffer,
		gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true},
		ip,
		udp,
		gopacket.Payload(payload),
	)
	if err != nil {
		panic(err)
	}
	packet := make([]byte, len(buffer.Bytes()))
	copy(packet, buffer.Bytes())
	return packet
}

func testingIcmp6EchoTypePacket(sourceIp string, destinationIp string, echoType uint8, id int, seq int, payload []byte) []byte {
	ip := &layers.IPv6{
		Version:    6,
		HopLimit:   64,
		SrcIP:      net.ParseIP(sourceIp).To16(),
		DstIP:      net.ParseIP(destinationIp).To16(),
		NextHeader: layers.IPProtocolICMPv6,
	}
	icmpLayer := &layers.ICMPv6{
		TypeCode: layers.CreateICMPv6TypeCode(echoType, 0),
	}
	icmpLayer.SetNetworkLayerForChecksum(ip)
	echo := &layers.ICMPv6Echo{
		Identifier: uint16(id),
		SeqNumber:  uint16(seq),
	}
	buffer := gopacket.NewSerializeBuffer()
	err := gopacket.SerializeLayers(
		buffer,
		gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true},
		ip,
		icmpLayer,
		echo,
		gopacket.Payload(payload),
	)
	if err != nil {
		panic(err)
	}
	packet := make([]byte, len(buffer.Bytes()))
	copy(packet, buffer.Bytes())
	return packet
}

func testingIcmp6EchoPacket(sourceIp string, destinationIp string, id int, seq int, payload []byte) []byte {
	return testingIcmp6EchoTypePacket(sourceIp, destinationIp, 128, id, seq, payload)
}
