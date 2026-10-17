package connect

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// T30 end-to-end: an IpPacketToProvider carrying a v6 udp packet behind a
// hop-by-hop extension header must reach the nat and be forwarded, exactly
// like a plain v6 packet. The harness mirrors ip_smtp_seam_test.go: a real
// provider over a real local user nat, driven through ClientReceive.

func newV6ExtReceiveProvider(t *testing.T) *RemoteUserNatProvider {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clientSettings := DefaultClientSettings()
	clientSettings.SendBufferSettings.SequenceBufferSize = 0
	clientSettings.SendBufferSettings.AckBufferSize = 0
	clientSettings.ReceiveBufferSettings.SequenceBufferSize = 0
	clientSettings.ForwardBufferSettings.SequenceBufferSize = 0
	client := NewClient(ctx, NewId(), NewNoContractClientOob(), clientSettings)
	localUserNat := NewLocalUserNatWithDefaults(ctx, "v6ext-provider-test", nil)
	settings := DefaultRemoteUserNatProviderSettings()
	settings.WriteTimeout = time.Second
	settings.EgressSecurityPolicyGenerator = func(context.Context, *SecurityPolicyStatsCollector) SecurityPolicy {
		return DisableSecurityPolicy()
	}
	provider := NewRemoteUserNatProvider(client, localUserNat, nil, settings)
	t.Cleanup(func() {
		provider.Close()
		localUserNat.Close()
		client.Cancel()
		cancel()
	})
	return provider
}

func TestProviderReceiveIpv6ExtensionHeadersReachTheNat(t *testing.T) {
	listener, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		t.Skipf("no ipv6 loopback: %v", err)
	}
	defer listener.Close()
	destination := listener.LocalAddr().(*net.UDPAddr)

	payload := []byte("through-the-chain")
	udp := make([]byte, UdpHeaderSize+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], 42001)
	binary.BigEndian.PutUint16(udp[2:4], uint16(destination.Port))
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	copy(udp[UdpHeaderSize:], payload)
	checksum := transportChecksum(ipProtocolNumberUdp, net.ParseIP("fd00::9").To16(), destination.IP.To16(), udp)
	if checksum == 0 {
		checksum = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:8], checksum)
	packet := testIpv6Packet("fd00::9", destination.IP.String(), []testIpv6Ext{{kind: ipv6NextHeaderHopByHop}}, byte(ipProtocolNumberUdp), udp)

	provider := newV6ExtReceiveProvider(t)
	pooled := MessagePoolCopy(packet)
	frame, err := ToFrame(&protocol.IpPacketToProvider{
		IpPacket: &protocol.IpPacket{PacketBytes: pooled},
	}, DefaultProtocolVersion)
	if err != nil {
		MessagePoolReturn(pooled)
		t.Fatal(err)
	}
	defer MessagePoolReturn(frame.MessageBytes)
	// like the smtp seam harness: ClientReceive hands the packet to the nat
	// shard asynchronously, so the buffer is not returned here
	provider.ClientReceive(TransferPath{}, []*protocol.Frame{frame}, Peer{ProvideMode: protocol.ProvideMode_Public})

	buffer := make([]byte, 2048)
	listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buffer)
	if err != nil {
		t.Fatalf("the chained packet never reached the nat destination: %v", err)
	}
	if string(buffer[:n]) != string(payload) {
		t.Fatalf("the destination received %q, want %q", buffer[:n], payload)
	}
}
