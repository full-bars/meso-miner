package connect

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

func newTrackerTestSequence(t *testing.T, source TransferPath, identifier uint16) *IcmpSequence {
	t.Helper()
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	return NewIcmpSequence(
		context.Background(), nil, source, protocol.ProvideMode(0), 4,
		net.ParseIP("10.0.0.9"), identifier, net.ParseIP("203.0.113.7"), settings,
	)
}

func icmpSequenceCanceled(sequence *IcmpSequence) bool {
	select {
	case <-sequence.ctx.Done():
		return true
	default:
		return false
	}
}

// the shared tracker holds the nat-wide caps: the global cap evicts the least
// recently active flow of the whole nat, not of one buffer, and the counts
// follow every registration and release.
func TestIcmpFlowTrackerGlobalCapIsNatsWide(t *testing.T) {
	resetIcmpGate(t)
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	settings.UserLimit = 0
	settings.GlobalLimit = 2
	tracker := newIcmpFlowTracker()

	sourceA := TransferPath{SourceId: NewId()}
	sourceB := TransferPath{SourceId: NewId()}
	sourceC := TransferPath{SourceId: NewId()}
	s1 := newTrackerTestSequence(t, sourceA, 1)
	s2 := newTrackerTestSequence(t, sourceB, 2)
	s3 := newTrackerTestSequence(t, sourceC, 3)

	tracker.register(s1, settings)
	tracker.register(s2, settings)
	if tracker.total != 2 || tracker.perSource[sourceA] != 1 || tracker.perSource[sourceB] != 1 {
		t.Fatalf("counts after two registrations: total %d perSource %v", tracker.total, tracker.perSource)
	}
	if icmpSequenceCanceled(s1) || icmpSequenceCanceled(s2) {
		t.Fatal("no eviction while the cap admits")
	}

	// the third flow crosses the global cap: the oldest flow of the nat is
	// evicted, across buffers
	tracker.register(s3, settings)
	if !icmpSequenceCanceled(s1) {
		t.Fatal("the global cap must evict the oldest flow of the nat")
	}
	if icmpSequenceCanceled(s2) || icmpSequenceCanceled(s3) {
		t.Fatal("only the oldest flow may be evicted")
	}
	if tracker.total != 2 {
		t.Fatalf("total must stay at the cap, got %d", tracker.total)
	}
	if _, ok := tracker.perSource[sourceA]; ok {
		t.Fatal("the evicted flow's source count must be released")
	}

	// release frees room: a canceled flow's slot admits the next one
	s2.Cancel()
	if tracker.total != 1 {
		t.Fatalf("release must decrement the total, got %d", tracker.total)
	}
	if _, ok := tracker.order[s2]; ok {
		t.Fatal("release must drop the flow from the order")
	}
	// Cancel is idempotent with the tracker: a second cancel changes nothing
	s2.Cancel()
	if tracker.total != 1 {
		t.Fatalf("release must be idempotent, got %d", tracker.total)
	}
}

// the per-source cap counts a source's flows across the whole nat, and never
// another source's
func TestIcmpFlowTrackerUserCapIsPerSource(t *testing.T) {
	resetIcmpGate(t)
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	settings.UserLimit = 1
	settings.GlobalLimit = 0
	tracker := newIcmpFlowTracker()

	sourceA := TransferPath{SourceId: NewId()}
	sourceB := TransferPath{SourceId: NewId()}
	sA1 := newTrackerTestSequence(t, sourceA, 1)
	sB1 := newTrackerTestSequence(t, sourceB, 2)
	tracker.register(sA1, settings)
	tracker.register(sB1, settings)
	if icmpSequenceCanceled(sA1) || icmpSequenceCanceled(sB1) {
		t.Fatal("a different source's flow must not evict")
	}

	sA2 := newTrackerTestSequence(t, sourceA, 3)
	tracker.register(sA2, settings)
	if !icmpSequenceCanceled(sA1) {
		t.Fatal("the per-source cap must evict that source's oldest flow")
	}
	if icmpSequenceCanceled(sB1) {
		t.Fatal("the per-source cap must not touch another source")
	}
	if tracker.perSource[sourceA] != 1 || tracker.total != 2 {
		t.Fatalf("counts after the per-source eviction: total %d perSource %v", tracker.total, tracker.perSource)
	}
}

// one source's cancel reaches its icmp flows (the nat registers this with
// its udp and tcp buffers) and returns how many it cancelled
func TestIcmpBufferCancelSourceCancelsItsFlows(t *testing.T) {
	resetIcmpGate(t)
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buffer := NewIcmp4Buffer(ctx, nil, settings)

	source := TransferPath{SourceId: NewId()}
	other := TransferPath{SourceId: NewId()}
	itsFlow := newTrackerTestSequence(t, source, 1)
	otherFlow := newTrackerTestSequence(t, other, 2)
	buffer.mutex.Lock()
	buffer.sourceSequences[source] = map[BufferId4]*IcmpSequence{BufferId4{}: itsFlow}
	buffer.sourceSequences[other] = map[BufferId4]*IcmpSequence{BufferId4{}: otherFlow}
	buffer.mutex.Unlock()

	if n := buffer.CancelSource(source.SourceId); n != 1 {
		t.Fatalf("CancelSource must cancel the source's flows, got %d", n)
	}
	if !icmpSequenceCanceled(itsFlow) {
		t.Fatal("the source's flow must be canceled")
	}
	if icmpSequenceCanceled(otherFlow) {
		t.Fatal("another source's flow must not be canceled")
	}
}

// two buffers sharing one tracker enforce the nat-wide cap across both, so
// the configured ceiling is not multiplied by the shard and family count
func TestIcmpSharedBuffersEnforceTheNatsWideCap(t *testing.T) {
	resetIcmpGate(t)
	if !pingGroupRangePermits(t) {
		t.Skip("unprivileged icmp sockets are not permitted here")
	}
	settings := DefaultIcmpBufferSettingsWithBufferSize(1)
	settings.Log = NewNoopLogger()
	settings.UserLimit = 0
	settings.GlobalLimit = 1
	tracker := newIcmpFlowTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bufferA := newIcmp4BufferShared(ctx, nil, settings, tracker)
	bufferB := newIcmp4BufferShared(ctx, nil, settings, tracker)

	send := func(buffer *Icmp4Buffer, identifier uint16) bool {
		packet := MessagePoolGet(2048)
		success, _ := buffer.send(
			TransferPath{},
			protocol.ProvideMode(0),
			&parsedIcmp{
				sourceIp:       net.ParseIP("10.0.0.9"),
				destinationIp:  net.ParseIP("203.0.113.7"),
				echoRequest:    true,
				identifier:     identifier,
				sequenceNumber: 1,
				ttl:            64,
				payload:        []byte("ping"),
			},
			0,
			packet,
		)
		return success
	}

	if !send(bufferA, 0x11) {
		t.Skip("could not open an echo flow here")
	}
	if !send(bufferB, 0x22) {
		t.Fatal("the second buffer's send must reach its buffer (eviction admits it)")
	}
	// the evicted first flow removes itself from its own buffer asynchronously
	deadline := time.Now().Add(5 * time.Second)
	for {
		bufferA.mutex.Lock()
		remaining := len(bufferA.sequences)
		bufferA.mutex.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the nat-wide cap must evict the first buffer's flow; %d left", remaining)
		}
		time.Sleep(20 * time.Millisecond)
	}
	bufferB.mutex.Lock()
	kept := len(bufferB.sequences)
	bufferB.mutex.Unlock()
	if kept != 1 {
		t.Fatalf("the second buffer's flow must stay, got %d", kept)
	}
}
