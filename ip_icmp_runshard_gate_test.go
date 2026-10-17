package connect

import (
	"context"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// the run shard must consult the icmp gate too, not only the parse: a request
// that reaches a shard after the family latched off (a platform refusal, or
// the operator switch flipping while packets were queued) must be dropped,
// never sent through an egress socket. The request is injected straight into
// the shard queue, which is exactly the path a queued packet takes.
func TestIcmpRunShardConsultsTheGate(t *testing.T) {
	resetIcmpGate(t)
	if !pingGroupRangePermits(t) {
		t.Skip("the kernel refuses unprivileged icmp sockets here (ping_group_range)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	localUserNat := NewLocalUserNatWithDefaults(ctx, "icmp-gate-runshard-test", nil)
	defer func() {
		localUserNat.Close()
		cancel()
	}()

	natReplies := make(chan []byte, 4)
	localUserNat.AddReceivePacketCallback(func(source TransferPath, provideMode protocol.ProvideMode, ipPath *IpPath, packet []byte) {
		select {
		case natReplies <- append([]byte(nil), packet...):
		default:
		}
	})

	inject := func(request []byte) {
		t.Helper()
		pooled := MessagePoolCopy(request)
		localUserNat.sendShards[0] <- &SendPacket{
			source:      TransferPath{},
			provideMode: protocol.ProvideMode_Public,
			packet:      pooled,
		}
	}
	waitReply := func() []byte {
		select {
		case reply := <-natReplies:
			return reply
		case <-time.After(1500 * time.Millisecond):
			return nil
		}
	}

	// positive control: with the gate on the same injection is answered
	control := testIcmp4EchoPacket("10.0.0.9", "127.0.0.1", icmp4TypeEchoRequest, 0x5150, 6, []byte("gated"))
	inject(control)
	if reply := waitReply(); reply == nil {
		t.Skip("the loopback echo was not answered even with the gate on; environment cannot run this test")
	}

	// latch the gate off exactly as a platform refusal would
	icmpEchoFamilyRefused.Store(icmpFamilyBit(4))
	icmpEnvState.Store(icmpEnvUnread)
	defer func() {
		icmpEchoFamilyRefused.Store(0)
		icmpEnvState.Store(icmpEnvUnread)
	}()

	gated := testIcmp4EchoPacket("10.0.0.9", "127.0.0.1", icmp4TypeEchoRequest, 0x5151, 7, []byte("gated"))
	inject(gated)
	if reply := waitReply(); reply != nil {
		t.Fatalf("the gate is off; the shard must drop the request, got a %d byte reply", len(reply))
	}
}
