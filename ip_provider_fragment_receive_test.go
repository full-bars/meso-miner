package connect

import (
	"net"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// T32 end-to-end: fragments arriving from a tunnel client through the
// provider receive path are reassembled at ingress and the NAT receives the
// whole datagram. Before the ingress gate the parser rejected every fragment
// and the datagram was dropped without a trace.

func providerFragmentedUdp(t *testing.T, network string, payload []byte, mtu int, order []int) []byte {
	t.Helper()
	listener, err := net.ListenPacket(network, net.JoinHostPort(map[string]string{"udp4": "127.0.0.1", "udp6": "::1"}[network], "0"))
	if err != nil {
		t.Skipf("no %s loopback: %v", network, err)
	}
	defer listener.Close()
	dest := listener.LocalAddr().(*net.UDPAddr)

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

	provider := newV6ExtReceiveProvider(t)
	for _, i := range order {
		if i >= len(fragments) {
			continue
		}
		pooled := MessagePoolCopy(fragments[i])
		frame, err := ToFrame(&protocol.IpPacketToProvider{
			IpPacket: &protocol.IpPacket{PacketBytes: pooled},
		}, DefaultProtocolVersion)
		if err != nil {
			MessagePoolReturn(pooled)
			t.Fatal(err)
		}
		defer MessagePoolReturn(frame.MessageBytes)
		provider.ClientReceive(TransferPath{}, []*protocol.Frame{frame}, Peer{ProvideMode: protocol.ProvideMode_Public})
	}

	buffer := make([]byte, 65535)
	listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buffer)
	if err != nil {
		return nil
	}
	return buffer[:n]
}

func providerFragmentPayload(size int) []byte {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i*7 + 3)
	}
	return payload
}

func TestProviderReassemblesFragmentedUdp(t *testing.T) {
	payload := providerFragmentPayload(3000)
	for _, tc := range []struct {
		name    string
		network string
		mtu     int
		order   []int
	}{
		{"v4 in order", "udp4", 1100, []int{0, 1, 2, 3}},
		{"v4 reversed", "udp4", 1100, []int{3, 2, 1, 0}},
		{"v6 in order", "udp6", 1280, []int{0, 1, 2, 3}},
		{"v6 reversed", "udp6", 1280, []int{3, 2, 1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := providerFragmentedUdp(t, tc.network, payload, tc.mtu, tc.order)
			if string(got) != string(payload) {
				t.Fatalf("the destination received %d bytes, want the whole %d byte datagram", len(got), len(payload))
			}
		})
	}
}

func TestProviderIncompleteFragmentsAreNotForwarded(t *testing.T) {
	payload := providerFragmentPayload(3000)
	// the final fragment of the three never arrives
	if got := providerFragmentedUdp(t, "udp4", payload, 1100, []int{0, 1}); got != nil {
		t.Fatalf("a partial datagram of %d bytes reached the destination", len(got))
	}
	if got := providerFragmentedUdp(t, "udp6", payload, 1280, []int{0, 1}); got != nil {
		t.Fatalf("a partial datagram of %d bytes reached the destination", len(got))
	}
}
