package connect

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

func newMetricsTestSequence(t *testing.T, destination TransferPath, configure func(*SendBufferSettings)) (*SendSequence, *SendBuffer, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	settings := DefaultClientSettings()
	settings.EncryptionSettings.Mode = EncryptionModeOff
	settings.ControlPingTimeout = 0
	if configure != nil {
		configure(settings.SendBufferSettings)
	}
	client := NewClient(ctx, NewId(), NewNoContractClientOob(), settings)
	t.Cleanup(client.Close)
	sendBuffer := NewSendBuffer(ctx, client, settings.SendBufferSettings)
	seq := NewSendSequence(ctx, client, sendBuffer, destination, MultiHopId{}, false, true, sequenceTlsRoleClient, false, settings.SendBufferSettings)
	return seq, sendBuffer, cancel
}

// Frames that reach a carrier are counted by destination class, and only client
// destinations feed the ack yield.
func TestTransferMoreStats_CountsSendsByDestinationClass(t *testing.T) {
	client, sendBufferClient, cancelClient := newMetricsTestSequence(t, DestinationId(NewId()), nil)
	defer cancelClient()
	control, sendBufferControl, cancelControl := newMetricsTestSequence(t, DestinationId(ControlId), nil)
	defer cancelControl()

	before := TransferMoreStatsNow()
	client.noteYieldSend(300)
	control.noteYieldSend(70)
	after := TransferMoreStatsNow()
	if after.ClientSentFrames-before.ClientSentFrames != 1 || after.ClientSentBytes-before.ClientSentBytes != 300 {
		t.Fatalf("client sends: %+v -> %+v", before, after)
	}
	if after.ControlSentFrames-before.ControlSentFrames != 1 || after.ControlSentBytes-before.ControlSentBytes != 70 {
		t.Fatalf("control sends: %+v -> %+v", before, after)
	}
	if len(sendBufferClient.DestinationYields(time.Now())) != 1 || len(sendBufferControl.DestinationYields(time.Now())) != 0 {
		t.Fatal("only client destinations may feed the ack yield")
	}
}

func TestTransferMoreStats_AckSentCounter(t *testing.T) {
	before := TransferMoreStatsNow()
	recordTransferAckSent(48)
	after := TransferMoreStatsNow()
	if after.AckFrames-before.AckFrames != 1 || after.AckBytes-before.AckBytes != 48 {
		t.Fatalf("ack sent: %+v -> %+v", before, after)
	}
}

func exitCount(reason string) int64 { return TransferMoreStatsNow().SequenceExits[reason] }

// Each way a send sequence can end is counted under its own reason.
func TestTransferMoreStats_SequenceExitsAreCountedByReason(t *testing.T) {
	// cancelled
	{
		started := make(chan struct{})
		var seqReason atomic.Int32
		seqReason.Store(-1)
		seq, _, cancel := newMetricsTestSequence(t, DestinationId(NewId()), func(s *SendBufferSettings) {
			s.IdleTimeout = time.Hour
			// deterministically wait for the loop's first iteration, so the
			// cancel cannot land in the start-up path and record the exit
			// under another reason (a sleep here was flaky under load)
			s.SendSequenceLoopStartedForTest = sync.OnceFunc(func() { close(started) })
		})
		// the exit recorder is per-sequence: the process-wide counter also
		// takes records from client-internal sequences that share this ctx
		// and race the cancel, so this asserts the sequence under test
		seq.sendBufferSettings.SendSequenceExitRecorderForTest = func(got *SendSequence, reason int) {
			if got == seq {
				seqReason.Store(int32(reason))
			}
		}
		t.Cleanup(func() { seq.sendBufferSettings.SendSequenceExitRecorderForTest = nil })
		before := exitCount("cancelled")
		done := make(chan struct{})
		go HandleError(func() { seq.Run(); close(done) })
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("the sequence loop never started")
		}
		cancel()
		<-done
		if got := seqReason.Load(); got != int32(sendExitCancelled) {
			t.Fatalf("the cancelled sequence's own exit reason = %d, want %d (exits now: %+v)", got, sendExitCancelled, TransferMoreStatsNow().SequenceExits)
		}
		if exitCount("cancelled")-before < 1 {
			t.Fatal("a cancelled sequence must count as cancelled")
		}
	}
	// idle
	{
		seq, _, cancel := newMetricsTestSequence(t, DestinationId(NewId()), func(s *SendBufferSettings) { s.IdleTimeout = 20 * time.Millisecond })
		defer cancel()
		before := exitCount("idle")
		done := make(chan struct{})
		go HandleError(func() { seq.Run(); close(done) })
		<-done
		if exitCount("idle")-before != 1 {
			t.Fatal("an idle sequence must count as idle")
		}
	}
	// ack timeout: an unretained item past its deadline
	{
		seq, _, cancel := newMetricsTestSequence(t, DestinationId(NewId()), func(s *SendBufferSettings) {
			s.AckTimeout = 20 * time.Millisecond
			s.IdleTimeout = time.Hour
		})
		defer cancel()
		item := &sendItem{
			transferItem:       transferItem{messageId: NewId(), sequenceNumber: 1, messageByteCount: 10},
			sendTime:           time.Now().Add(-time.Second),
			resendTime:         time.Now().Add(-time.Second),
			sendCount:          1,
			head:               true,
			transferFrameBytes: make([]byte, 10),
			ackCallback:        func(error) {},
		}
		seq.sendItems = append(seq.sendItems, item)
		seq.resendQueue.Add(item)
		before := exitCount("ack_timeout")
		done := make(chan struct{})
		go HandleError(func() { seq.Run(); close(done) })
		<-done
		if exitCount("ack_timeout")-before != 1 {
			t.Fatal("an ack timeout must count as ack_timeout")
		}
	}
}

// The retained-bytes gauge follows the sequences' own accounting.
func TestTransferMoreStats_RetainedBytesGaugeFollowsRelease(t *testing.T) {
	seq, _, cancel := newMetricsTestSequence(t, DestinationId(NewId()), nil)
	defer cancel()
	base := TransferMoreStatsNow().RetainedBytes
	item := &sendItem{
		transferItem:          transferItem{messageId: NewId(), sequenceNumber: 1, messageByteCount: 120},
		sendCount:             1,
		head:                  true,
		transferFrameBytes:    make([]byte, 120),
		ackCallback:           func(error) {},
		retainAfterAckTimeout: true,
	}
	seq.sendItems = append(seq.sendItems, item)
	seq.resendQueue.Add(item)
	seq.retainedByteCount = item.MessageByteCount()
	transferMoreCounters.retainedBytes.Add(int64(item.MessageByteCount())) // what admission does
	if got := TransferMoreStatsNow().RetainedBytes - base; got != 120 {
		t.Fatalf("gauge after admission = %d, want 120", got)
	}
	seq.dropItem(item, context.Canceled)
	if got := TransferMoreStatsNow().RetainedBytes - base; got != 0 {
		t.Fatalf("gauge after the item was dropped = %d, want 0", got)
	}
}

func TestSendBuffer_HeldBytesSumsTheResendQueueOfOneDestination(t *testing.T) {
	destination := DestinationId(NewId())
	seq, sendBuffer, cancel := newMetricsTestSequence(t, destination, nil)
	defer cancel()
	for i := 1; i <= 3; i++ {
		item := &sendItem{
			transferItem:       transferItem{messageId: NewId(), sequenceNumber: uint64(i), messageByteCount: 100},
			sendTime:           time.Now(),
			resendTime:         time.Now().Add(time.Hour),
			sendCount:          1,
			transferFrameBytes: make([]byte, 100),
			ackCallback:        func(error) {},
		}
		seq.sendItems = append(seq.sendItems, item)
		seq.resendQueue.Add(item)
	}
	sendBuffer.mutex.Lock()
	sendBuffer.sendSequences[sendSequenceId{Destination: destination}] = seq
	sendBuffer.mutex.Unlock()
	if got := sendBuffer.HeldBytes(destination.DestinationId); got != 300 {
		t.Fatalf("held bytes = %d, want 300", got)
	}
	if got := sendBuffer.HeldBytes(NewId()); got != 0 {
		t.Fatalf("held bytes for another destination = %d", got)
	}
}

// A released destination that acknowledges again is a release that was wrong,
// and how long it took to come back is recorded, per mode.
func TestDestinationYield_ReturnAfterReleaseIsRecordedPerMode(t *testing.T) {
	_, enforceBefore := ReturnAfterReleaseHistogram(false)
	_, shadowBefore := ReturnAfterReleaseHistogram(true)
	base := time.Unix(3_000_000, 0)

	enforced := &destinationYield{destinationId: NewId()}
	enforced.markReleased(base.UnixNano(), false)
	enforced.noteAck(10, base.Add(45*time.Second).UnixNano()) // <= 60s
	enforced.noteAck(10, base.Add(50*time.Second).UnixNano()) // already returned: not counted again

	shadow := &destinationYield{destinationId: NewId()}
	shadow.markReleased(base.UnixNano(), true)
	shadow.noteLive(base.Add(4 * time.Minute).UnixNano()) // <= 5m

	bounds, enforceAfter := ReturnAfterReleaseHistogram(false)
	_, shadowAfter := ReturnAfterReleaseHistogram(true)
	last := len(enforceAfter) - 1
	if len(enforceAfter) != len(bounds)+1 {
		t.Fatalf("histogram shape: %d bounds, %d buckets", len(bounds), len(enforceAfter))
	}
	if enforceAfter[last]-enforceBefore[last] != 1 || enforceAfter[1]-enforceBefore[1] != 1 || enforceAfter[0]-enforceBefore[0] != 0 {
		t.Fatalf("enforce: %v -> %v", enforceBefore, enforceAfter)
	}
	if shadowAfter[last]-shadowBefore[last] != 1 || shadowAfter[3]-shadowBefore[3] != 1 || shadowAfter[2]-shadowBefore[2] != 0 {
		t.Fatalf("shadow: %v -> %v", shadowBefore, shadowAfter)
	}
}

// The retained-byte budget is charged and released with exactly the same amount,
// through real admission, a head rewrite that changes the wire frame size, a
// cumulative ack and a drop. It used to be charged with the payload size and
// released with the (larger) frame size, drifting negative and loosening the cap.
func TestRetainedBudget_ChargeAndReleaseBalanceThroughRealAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := DefaultClientSettings()
	settings.EncryptionSettings.Mode = EncryptionModeOff
	settings.ControlPingTimeout = 0
	client := NewClient(ctx, NewId(), NewNoContractClientOob(), settings)
	defer client.Close()
	sink := make(chan []byte, 1<<12)
	transport := NewSendGatewayTransport()
	client.RouteManager().UpdateTransport(transport, []Route{sink})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sink:
			}
		}
	}()
	sendBuffer := NewSendBuffer(ctx, client, settings.SendBufferSettings)
	seq := NewSendSequence(ctx, client, sendBuffer, DestinationId(NewId()), MultiHopId{}, false, true, sequenceTlsRoleClient, false, settings.SendBufferSettings)

	gaugeBase := TransferMoreStatsNow().RetainedBytes
	const count = 6
	for i := 0; i < count; i++ {
		frame := RequireToFrameWithDefaultProtocolVersion(&protocol.SimpleMessage{Content: "retained return payload"})
		seq.send(frame, func(error) {}, true, false, true)
	}
	if len(seq.sendItems) != count {
		t.Fatalf("admitted %d items, want %d", len(seq.sendItems), count)
	}
	charged := ByteCount(0)
	for _, item := range seq.sendItems {
		if !item.retainAfterAckTimeout || item.retainedCharge == 0 {
			t.Fatalf("an admitted retained item carries no charge: %+v", item)
		}
		charged += item.retainedCharge
	}
	if seq.retainedByteCount != charged || TransferMoreStatsNow().RetainedBytes-gaugeBase != int64(charged) {
		t.Fatalf("budget %d / gauge %d, want exactly what items were charged %d",
			seq.retainedByteCount, TransferMoreStatsNow().RetainedBytes-gaugeBase, charged)
	}

	// a head rewrite changes the wire frame size; the release must not follow it
	head := seq.sendItems[0]
	before := len(head.transferFrameBytes)
	if rewritten, err := seq.setHead(head); err == nil {
		head.transferFrameBytes = rewritten
	}
	if len(head.transferFrameBytes) == before {
		t.Log("head rewrite did not change the frame size; the balance check still stands")
	}

	// cumulative ack of the first half, then drop one of the rest, then ack the rest
	seq.receiveAck(seq.sendItems[2].messageId, false, nil)
	if len(seq.sendItems) != count-3 {
		t.Fatalf("cumulative ack left %d items", len(seq.sendItems))
	}
	seq.dropItem(seq.sendItems[0], context.Canceled)
	seq.receiveAck(seq.sendItems[len(seq.sendItems)-1].messageId, false, nil)

	if seq.retainedByteCount != 0 {
		t.Fatalf("retained budget after releasing everything = %d, want exactly 0", seq.retainedByteCount)
	}
	if got := TransferMoreStatsNow().RetainedBytes - gaugeBase; got != 0 {
		t.Fatalf("retained gauge after releasing everything = %d, want 0", got)
	}
}
