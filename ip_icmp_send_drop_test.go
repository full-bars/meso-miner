package connect

import (
	"context"
	"net"
	"testing"

	"github.com/urnetwork/connect/protocol"
)

// A send that never enqueues must return the packet to the pool (the udp
// path's rule). A canceled nat context drops the send on both attempts, so
// the drop is deterministic: the packet must come back to its pool.
func TestIcmpSendDropReturnsThePoolBuffer(t *testing.T) {
	resetIcmpGate(t)
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buffer := NewIcmp4Buffer(ctx, func(source TransferPath, provideMode protocol.ProvideMode, ipPath *IpPath, packet []byte) {
		MessagePoolReturn(packet)
	}, settings)

	// the pool's smallest bucket, which is where every small packet lands
	packetSize := 2048
	takenBefore, returnedBefore := MessagePoolTotals(packetSize)
	packet := MessagePoolGet(packetSize)
	success, err := buffer.send(
		TransferPath{},
		protocol.ProvideMode(0),
		&parsedIcmp{
			sourceIp:       net.ParseIP("10.0.0.9"),
			destinationIp:  net.ParseIP("203.0.113.7"),
			echoRequest:    true,
			identifier:     0x22,
			sequenceNumber: 1,
			ttl:            64,
			payload:        []byte("ping"),
		},
		0,
		packet,
	)
	if success {
		t.Fatalf("a canceled context must drop the send")
	}
	if err == nil {
		t.Log("dropped without error (idle-close path)")
	}
	takenAfter, returnedAfter := MessagePoolTotals(packetSize)
	if takenAfter-takenBefore != 1 {
		t.Fatalf("expected exactly one take, got %d", takenAfter-takenBefore)
	}
	if returnedAfter-returnedBefore != 1 {
		t.Fatalf("a dropped icmp send must return the pool buffer: taken %d returned %d", takenAfter-takenBefore, returnedAfter-returnedBefore)
	}
}
