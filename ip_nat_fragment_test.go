package connect

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// fragmentedUdpThroughNat sends one udp datagram, fragmented at mtu, into a
// LocalUserNat and returns what a real udp listener behind it received.
func fragmentedUdpThroughNat(t *testing.T, network string, payload []byte, mtu int, order []int) []byte {
	t.Helper()
	listener, err := net.ListenPacket(network, net.JoinHostPort(map[string]string{"udp4": "127.0.0.1", "udp6": "::1"}[network], "0"))
	if err != nil {
		t.Skipf("no %s loopback: %v", network, err)
	}
	defer listener.Close()
	dest := listener.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := DefaultLocalUserNatSettings()
	settings.UdpBufferSettings.DialContextSettings = &DialContextSettings{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	nat := NewLocalUserNat(ctx, "test", nil, settings)

	version := 4
	src := net.ParseIP("10.0.0.9").To4()
	dst := dest.IP.To4()
	if network == "udp6" {
		version = 6
		src = net.ParseIP("fd00::9")
		dst = dest.IP
	}
	path := &IpPath{
		Version: version, Protocol: IpProtocolUdp,
		SourceIp: src, SourcePort: 42001, DestinationIp: dst, DestinationPort: dest.Port,
	}
	whole := ipOosUdpPacket(path, payload)
	var fragments [][]byte
	if version == 4 {
		fragments, err = fragmentIpv4Packet(whole, mtu)
	} else {
		fragments, err = fragmentIpv6Packet(whole, mtu)
	}
	if err != nil || len(fragments) < 2 {
		t.Fatalf("fragmenting: %d fragments, err=%v", len(fragments), err)
	}
	for _, i := range order {
		if i >= len(fragments) {
			continue
		}
		if !nat.SendPacket(TransferPath{}, protocol.ProvideMode_Network, fragments[i], time.Second) {
			t.Fatal("the nat refused a fragment")
		}
	}

	buffer := make([]byte, 65535)
	listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buffer)
	if err != nil {
		return nil
	}
	return buffer[:n]
}

func TestNatReassemblesFragmentedUdp(t *testing.T) {
	payload := make([]byte, 3000)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	for _, network := range []string{"udp4", "udp6"} {
		for name, order := range map[string][]int{
			"in order":   {0, 1, 2, 3},
			"reversed":   {3, 2, 1, 0},
			"first last": {1, 2, 3, 0},
		} {
			got := fragmentedUdpThroughNat(t, network, payload, 1100, order)
			if string(got) != string(payload) {
				t.Fatalf("%s %s: the destination received %d bytes, want the %d byte datagram", network, name, len(got), len(payload))
			}
		}
	}
}

func TestNatDropsAnIncompleteFragmentedDatagram(t *testing.T) {
	payload := make([]byte, 3000)
	for _, network := range []string{"udp4", "udp6"} {
		// the last of three fragments never arrives
		if got := fragmentedUdpThroughNat(t, network, payload, 1100, []int{0, 1}); got != nil {
			t.Fatalf("%s: an incomplete datagram was delivered (%d bytes)", network, len(got))
		}
	}
}

// every fragment of one datagram must pin to the same shard
func TestFlowHashPinsAllFragmentsOfADatagram(t *testing.T) {
	payload := make([]byte, 3000)
	for _, version := range []int{4, 6} {
		path := &IpPath{Version: version, Protocol: IpProtocolUdp, SourcePort: 42001, DestinationPort: 443}
		if version == 4 {
			path.SourceIp, path.DestinationIp = net.ParseIP("10.0.0.9").To4(), net.ParseIP("203.0.113.7").To4()
		} else {
			path.SourceIp, path.DestinationIp = net.ParseIP("fd00::9"), net.ParseIP("2001:db8::7")
		}
		whole := ipOosUdpPacket(path, payload)
		var fragments [][]byte
		var err error
		if version == 4 {
			fragments, err = fragmentIpv4Packet(whole, 1100)
		} else {
			fragments, err = fragmentIpv6Packet(whole, 1100)
		}
		if err != nil || len(fragments) < 3 {
			t.Fatalf("v%d fragmenting: %d err=%v", version, len(fragments), err)
		}
		want := flowHash(fragments[0])
		for i, fragment := range fragments {
			if got := flowHash(fragment); got != want {
				t.Fatalf("v%d: fragment %d hashes to %d, the first to %d", version, i, got, want)
			}
		}
	}
}

// An oversized udp reply is one datagram fragmented at the ip layer in both
// families, never several independent datagrams, which would corrupt the
// application's framing.
func TestStreamStateOversizedUdpReplyIsOneFragmentedDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		stream := newTestStreamState(version)
		if version == 6 {
			stream.sourceIp, stream.destinationIp = net.ParseIP("fd00::1"), net.ParseIP("2001:db8::2")
		}
		payload := make([]byte, 3000)
		for i := range payload {
			payload[i] = byte(i * 3)
		}
		packets, err := stream.DataPackets(payload, len(payload), 1100)
		if err != nil || len(packets) < 3 {
			t.Fatalf("v%d: %d packets, err=%v", version, len(packets), err)
		}
		var whole []byte
		if version == 4 {
			r := newIpv4FragmentReassembler()
			defer r.close()
			for _, p := range packets {
				whole = r.process(TransferPath{}, TransferKey{}, protocol.ProvideMode_Network, p)
			}
		} else {
			r := newIpv6FragmentReassembler()
			defer r.close()
			for _, p := range packets {
				whole = r.process(TransferPath{}, TransferKey{}, protocol.ProvideMode_Network, p)
			}
		}
		if whole == nil {
			t.Fatalf("v%d: the fragments did not reassemble into one datagram", version)
		}
		ipPath, got, err := ParseIpPathWithPayload(whole)
		if err != nil || string(got) != string(payload) || ipPath.Protocol != IpProtocolUdp {
			t.Fatalf("v%d: reassembled datagram is wrong (err=%v, %d bytes)", version, err, len(got))
		}
	}
}
