package connect

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// the v33 icmp reachability: the ParseIpPath echo view (v4 proto 1 and v6
// proto 58, echo only), the kill switch and platform self-disable, and the
// end-to-end provider receive path. Deterministic; the socket-level test
// skips where the kernel refuses unprivileged icmp sockets.

func testIcmp4EchoPacket(sourceIp string, destinationIp string, icmpType byte, id int, seq int, payload []byte) []byte {
	icmp := make([]byte, IcmpHeaderSize+len(payload))
	icmp[0] = icmpType
	icmp[1] = 0
	binary.BigEndian.PutUint16(icmp[4:6], uint16(id))
	binary.BigEndian.PutUint16(icmp[6:8], uint16(seq))
	copy(icmp[IcmpHeaderSize:], payload)
	binary.BigEndian.PutUint16(icmp[2:4], checksumFinish(checksumAdd(0, icmp)))
	packet, transport := ipTransportPacket(&IpPath{
		Version:       4,
		SourceIp:      net.ParseIP(sourceIp).To4(),
		DestinationIp: net.ParseIP(destinationIp).To4(),
	}, ipProtocolNumberIcmp4, len(icmp))
	copy(transport, icmp)
	return packet
}

func TestParseIpPathIcmpEchoView(t *testing.T) {
	resetIcmpGate(t)
	payload := []byte("ping")

	request := testIcmp4EchoPacket("10.0.0.9", "203.0.113.7", icmp4TypeEchoRequest, 0x4711, 1, payload)
	ipPath, got, err := ParseIpPathWithPayload(request)
	if err != nil {
		t.Fatalf("v4 echo request: %v", err)
	}
	if ipPath.Version != 4 || ipPath.Protocol != IpProtocolIcmp || ipPath.SourcePort != 0x4711 ||
		ipPath.DestinationPort != 0 || !strings.EqualFold(ipPath.SourceIp.String(), "10.0.0.9") ||
		string(got) != string(payload) {
		t.Fatalf("v4 request path = %+v payload=%q", ipPath, got)
	}
	MessagePoolReturn(request)

	reply := testIcmp4EchoPacket("203.0.113.7", "10.0.0.9", icmp4TypeEchoReply, 0x4711, 1, payload)
	ipPath, _, err = ParseIpPathWithPayload(reply)
	if err != nil {
		t.Fatalf("v4 echo reply: %v", err)
	}
	if ipPath.Protocol != IpProtocolIcmp || ipPath.SourcePort != 0 || ipPath.DestinationPort != 0x4711 {
		t.Fatalf("v4 reply path = %+v", ipPath)
	}
	MessagePoolReturn(reply)

	echo6 := testingIcmp6EchoPacket(testIpv6Src, testIpv6Dst, 0x77, 1, payload)
	ipPath, got, err = ParseIpPathWithPayload(echo6)
	if err != nil {
		t.Fatalf("v6 echo request: %v", err)
	}
	if ipPath.Version != 6 || ipPath.Protocol != IpProtocolIcmp || ipPath.SourcePort != 0x77 || string(got) != string(payload) {
		t.Fatalf("v6 request path = %+v payload=%q", ipPath, got)
	}

	// only echo parses: an unreachable (and everything else) must keep
	// failing so the dial-failure intercept receives it
	unreachable, ok := ipOosUnreachable(&IpPath{
		Version:         4,
		Protocol:        IpProtocolTcp,
		SequenceNumber:  1,
		SourceIp:        net.ParseIP("10.0.0.9").To4(),
		SourcePort:      42001,
		DestinationIp:   net.ParseIP("203.0.113.7").To4(),
		DestinationPort: 443,
	})
	if !ok {
		t.Fatal("the unreachable builder failed")
	}
	if _, _, err := ParseIpPathWithPayload(unreachable); err == nil {
		t.Fatal("an icmp unreachable must not parse as an echo")
	}
	MessagePoolReturn(unreachable)
}

func TestIcmpEchoKillSwitchAndSelfDisable(t *testing.T) {
	resetIcmpGate(t)
	request := testIcmp4EchoPacket("10.0.0.9", "203.0.113.7", icmp4TypeEchoRequest, 0x22, 1, []byte("x"))
	defer MessagePoolReturn(request)
	requestCopy := MessagePoolCopy(request)
	defer MessagePoolReturn(requestCopy)

	// enabled by default
	if _, _, err := ParseIpPathWithPayload(request); err != nil {
		t.Fatalf("icmp must parse while enabled: %v", err)
	}

	// the env kill switch restores the historical drop
	icmpEnvState.Store(icmpEnvUnread)
	os.Setenv("URNETWORK_ICMP", "off")
	defer os.Unsetenv("URNETWORK_ICMP")
	if _, _, err := ParseIpPathWithPayload(request); err == nil {
		t.Fatal("URNETWORK_ICMP=off must reject icmp")
	}
	icmpEnvState.Store(icmpEnvUnread)

	// and the platform refusal latches the same drop, for that family only
	icmpEchoFamilyRefused.Store(icmpFamilyBit(4))
	defer icmpEchoFamilyRefused.Store(0)
	if _, _, err := ParseIpPathWithPayload(requestCopy); err == nil {
		t.Fatal("the platform self-disable must reject icmp")
	}
}

// ping_groupRangePermits reports whether this process may open unprivileged
// icmp sockets, with the same rule the production self-disable relies on.
func pingGroupRangePermits(t *testing.T) bool {
	t.Helper()
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ping_group_range")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return false
	}
	low, errLow := strconv.Atoi(fields[0])
	high, errHigh := strconv.Atoi(fields[1])
	if errLow != nil || errHigh != nil {
		return false
	}
	gid := os.Getgid()
	return low <= gid && gid <= high
}

// the acceptance gate: an echo request in at the provider receive path is
// answered and the reply comes back out as an IpPacketFromProvider. The
// kernel answers loopback pings; where it refuses the sockets the test skips.
func TestProviderReceiveIcmpEchoAnswersThroughTheNat(t *testing.T) {
	resetIcmpGate(t)
	if !pingGroupRangePermits(t) {
		t.Skip("the kernel refuses unprivileged icmp sockets here (ping_group_range)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	clientSettings := DefaultClientSettings()
	clientSettings.SendBufferSettings.SequenceBufferSize = 0
	clientSettings.SendBufferSettings.AckBufferSize = 0
	clientSettings.ReceiveBufferSettings.SequenceBufferSize = 0
	clientSettings.ForwardBufferSettings.SequenceBufferSize = 0
	client := NewClient(ctx, NewId(), NewNoContractClientOob(), clientSettings)
	localUserNat := NewLocalUserNatWithDefaults(ctx, "icmp-provider-test", nil)
	settings := DefaultRemoteUserNatProviderSettings()
	settings.WriteTimeout = time.Second
	settings.EgressSecurityPolicyGenerator = func(context.Context, *SecurityPolicyStatsCollector) SecurityPolicy {
		return DisableSecurityPolicy()
	}
	provider := NewRemoteUserNatProvider(client, localUserNat, nil, settings)
	defer func() {
		provider.Close()
		localUserNat.Close()
		client.Cancel()
		cancel()
	}()

	natReplies := make(chan []byte, 4)
	localUserNat.AddReceivePacketCallback(func(source TransferPath, provideMode protocol.ProvideMode, ipPath *IpPath, packet []byte) {
		select {
		case natReplies <- append([]byte(nil), packet...):
		default:
		}
	})

	// an echo request for loopback: the kernel answers it locally
	request := testIcmp4EchoPacket("10.0.0.9", "127.0.0.1", icmp4TypeEchoRequest, 0x5150, 7, []byte("through"))
	pooled := MessagePoolCopy(request)
	frame, err := ToFrame(&protocol.IpPacketToProvider{
		IpPacket: &protocol.IpPacket{PacketBytes: pooled},
	}, DefaultProtocolVersion)
	if err != nil {
		MessagePoolReturn(pooled)
		t.Fatal(err)
	}
	defer MessagePoolReturn(frame.MessageBytes)
	provider.ClientReceive(TransferPath{}, []*protocol.Frame{frame}, Peer{ProvideMode: protocol.ProvideMode_Public})

	// The observation point is the nat's receive boundary: the provider
	// frames what the nat yields toward the client from exactly here, but a
	// standalone harness cannot carry those frames without live transports
	// (the same boundary the other standalone provider tests assert at).
	select {
	case replyPacket := <-natReplies:
		ipPath, payload, err := ParseIpPathWithPayload(replyPacket)
		if err != nil {
			t.Fatalf("the reply does not parse: %v", err)
		}
		if ipPath.Protocol != IpProtocolIcmp || ipPath.DestinationPort != 0x5150 || string(payload) != "through" {
			t.Fatalf("the reply is not the echo reply for the request: %+v %q", ipPath, payload)
		}
		if len(replyPacket) < Ipv4HeaderSizeWithoutExtensions+IcmpHeaderSize || replyPacket[0]>>4 != 4 || replyPacket[20] != icmp4TypeEchoReply {
			t.Fatalf("the reply is not a v4 echo reply (%d bytes)", len(replyPacket))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no echo reply returned through the provider receive path")
	}
}
