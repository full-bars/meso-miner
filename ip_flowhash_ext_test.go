package connect

import (
	"encoding/binary"
	"testing"
)

func flowHashUdp6(srcPort uint16, exts []testIpv6Ext) []byte {
	udp := make([]byte, UdpHeaderSize+4)
	binary.BigEndian.PutUint16(udp[0:2], srcPort)
	binary.BigEndian.PutUint16(udp[2:4], 53)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	return testIpv6Packet("2001:db8::9", "2001:db8::53", exts, byte(ipProtocolNumberUdp), udp)
}

// A flow must keep its shard when a stack adds an extension header: the
// unfragmented chain hashes the ports at the transport header.
func TestFlowHashIpv6ExtensionChainHashesPorts(t *testing.T) {
	plain := flowHash(flowHashUdp6(42001, nil))
	chained := flowHash(flowHashUdp6(42001, []testIpv6Ext{{kind: ipv6NextHeaderHopByHop}}))
	if plain != chained {
		t.Fatalf("the same flow hashes to %d plain and %d behind a hop-by-hop header", plain, chained)
	}
	other := flowHash(flowHashUdp6(42002, []testIpv6Ext{{kind: ipv6NextHeaderHopByHop}}))
	if other == chained {
		t.Fatal("two source ports behind a hop-by-hop header hash alike")
	}
}
