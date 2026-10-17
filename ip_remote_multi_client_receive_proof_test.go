package connect

import (
	"context"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// the receive-side connect proof: a control-only packet (a reset, a bare syn)
// must not count as a proven connect, and the race-overflow branch must
// commit the source client as the flow's owner.

func newReceiveProofTestMultiClient() *RemoteUserNatMultiClient {
	mc := newDialFailureTestMultiClient(true)
	mc.settings.MultiRaceClientPacketMaxCount = 1
	mc.settings.MultiRacePacketMaxCount = 1
	mc.ingressSecurityPolicy = DisableSecurityPolicy()
	mc.log = NewNoopLogger()
	mc.receivePacketCallback = func(source TransferPath, provideMode protocol.ProvideMode, ipPath *IpPath, packet []byte) {}
	return mc
}

func TestProvenConnectPacket(t *testing.T) {
	cases := []struct {
		name     string
		protocol IpProtocol
		syn      bool
		ack      bool
		rst      bool
		want     bool
	}{
		{"tcp syn-ack", IpProtocolTcp, true, true, false, true},
		{"tcp data", IpProtocolTcp, false, false, false, true},
		{"tcp ack", IpProtocolTcp, false, true, false, true},
		{"tcp reset", IpProtocolTcp, false, false, true, false},
		{"tcp bare syn", IpProtocolTcp, true, false, false, false},
		{"udp data", IpProtocolUdp, false, false, false, true},
	}
	for _, c := range cases {
		ipPath := &IpPath{
			Version:       4,
			Protocol:      c.protocol,
			SourceIp:      []byte{10, 0, 0, 9},
			DestinationIp: []byte{203, 0, 113, 7},
			Syn:           c.syn,
			Ack:           c.ack,
			Rst:           c.rst,
		}
		if got := provenConnectPacket(ipPath); got != c.want {
			t.Fatalf("%s: provenConnectPacket = %v, want %v", c.name, got, c.want)
		}
	}
}

// a provider's own reset must not prove a connect: it must not mark the flow
// established nor clear the exit's dial-strike window (an always-rsting
// provider would otherwise never read as dial-starved)
func TestMultiClientReceiveProviderRstDoesNotProveConnect(t *testing.T) {
	mc := newReceiveProofTestMultiClient()
	channel := &multiClientChannel{}
	fwd := dialFailureFlowPath(4)
	update := newMultiClientChannelUpdate(context.Background(), fwd)
	update.client = channel
	registerDialFailureTestFlow(mc, channel, update, fwd)

	rst := *fwd.Reverse()
	rst.Syn = false
	rst.Ack = false
	rst.Rst = true
	mc.clientReceivePacket(channel, TransferPath{}, protocol.ProvideMode_Network, &rst, []byte("rst"))

	channel.stateLock.Lock()
	successes := len(channel.connectSuccessTimes)
	channel.stateLock.Unlock()
	if successes != 0 {
		t.Fatal("the provider's own rst counted as a proven connect")
	}
	if update.receivedInbound.Load() {
		t.Fatal("the provider's own rst marked the flow established")
	}

	// and a real syn-ack on the same flow still proves the connect
	synAck := *fwd.Reverse()
	synAck.Ack = true
	mc.clientReceivePacket(channel, TransferPath{}, protocol.ProvideMode_Network, &synAck, []byte("synack"))
	channel.stateLock.Lock()
	successes = len(channel.connectSuccessTimes)
	channel.stateLock.Unlock()
	if successes != 1 {
		t.Fatalf("a syn-ack must prove the connect, got %d successes", successes)
	}
	if !update.receivedInbound.Load() {
		t.Fatal("a syn-ack must mark the flow established")
	}
}

// the race-overflow branch commits the source client as the flow's owner,
// not nil: an unowned established flow reads every later packet as a race
func TestMultiClientReceiveRaceOverflowCommitsSourceClient(t *testing.T) {
	mc := newReceiveProofTestMultiClient()
	// the overflow commit binds the channel through updateClientPath, which
	// reads IsDone on it; a real channel always has a ctx
	channel := &multiClientChannel{ctx: context.Background()}
	fwd := dialFailureFlowPath(4)
	update := newMultiClientChannelUpdate(context.Background(), fwd)
	update.race = newMultiClientChannelUpdateRace(context.Background())
	update.race.clientStates[channel] = &multiClientChannelRaceClientState{
		packets:     []*receivePacket{{Packet: []byte("x")}},
		sendTime:    time.Now(),
		receiveTime: time.Now(),
	}
	update.race.packetCount = 1
	registerDialFailureTestFlow(mc, channel, update, fwd)

	data := *fwd.Reverse()
	data.Syn = false
	data.Rst = false
	data.Ack = true
	mc.clientReceivePacket(channel, TransferPath{}, protocol.ProvideMode_Network, &data, []byte("data"))

	if update.client != channel {
		t.Fatal("the overflow race must commit the source client as the flow's owner")
	}
	// and the committed data packet proves the connect
	if !update.receivedInbound.Load() {
		t.Fatal("the committed data packet must mark the flow established")
	}
}
