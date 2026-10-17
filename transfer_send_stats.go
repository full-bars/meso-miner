package connect

import "sync/atomic"

// Where the bytes go. The transfer layer writes each frame once when it is first
// sent and again every time its resend timer fires before an ack arrives. The
// two look identical on the wire, so a provider whose acks are slow can send many
// times more than it ever delivers while the billable counters (which count each
// client packet once) stay flat. These counters split the two.

// ResendDepth buckets count resends by how many times the item had already been
// sent: second send, third or fourth, fifth to eighth, ninth and later.
const transferResendDepthBuckets = 4

var transferSendCounters struct {
	firstFrames  atomic.Int64
	firstBytes   atomic.Int64
	resendFrames atomic.Int64
	resendBytes  atomic.Int64
	resendDepth  [transferResendDepthBuckets]atomic.Int64

	deferredFrames atomic.Int64
	deferredBytes  atomic.Int64

	wouldDeferFrames atomic.Int64
	wouldDeferBytes  atomic.Int64
	earnedBytes      atomic.Int64
}

// TransferSendStats is a snapshot of the process-wide transfer send counters.
type TransferSendStats struct {
	FirstFrames  int64
	FirstBytes   int64
	ResendFrames int64
	ResendBytes  int64
	ResendDepth  [transferResendDepthBuckets]int64

	// ResendDeferred counts resends the retry budget refused to send yet.
	ResendDeferredFrames int64
	ResendDeferredBytes  int64

	// ResendWouldDefer counts resends shadow mode sent anyway that enforce would
	// have refused. BudgetEarnedBytes is acknowledged bytes credited to the budget.
	ResendWouldDeferFrames int64
	ResendWouldDeferBytes  int64
	BudgetEarnedBytes      int64
}

func TransferSendStatsNow() TransferSendStats {
	var s TransferSendStats
	s.FirstFrames = transferSendCounters.firstFrames.Load()
	s.FirstBytes = transferSendCounters.firstBytes.Load()
	s.ResendFrames = transferSendCounters.resendFrames.Load()
	s.ResendBytes = transferSendCounters.resendBytes.Load()
	for i := range s.ResendDepth {
		s.ResendDepth[i] = transferSendCounters.resendDepth[i].Load()
	}
	s.ResendDeferredFrames = transferSendCounters.deferredFrames.Load()
	s.ResendDeferredBytes = transferSendCounters.deferredBytes.Load()
	s.ResendWouldDeferFrames = transferSendCounters.wouldDeferFrames.Load()
	s.ResendWouldDeferBytes = transferSendCounters.wouldDeferBytes.Load()
	s.BudgetEarnedBytes = transferSendCounters.earnedBytes.Load()
	return s
}

func recordTransferFirstSend(byteCount int) {
	transferSendCounters.firstFrames.Add(1)
	transferSendCounters.firstBytes.Add(int64(byteCount))
}

func recordTransferResendWouldDefer(byteCount int) {
	transferSendCounters.wouldDeferFrames.Add(1)
	transferSendCounters.wouldDeferBytes.Add(int64(byteCount))
}

// recordTransferAckedBytes credits acknowledged bytes to the resend budget.
func recordTransferAckedBytes(byteCount int) {
	transferSendCounters.earnedBytes.Add(int64(byteCount))
	transferResendBudget.earn(byteCount)
}

func recordTransferResendDeferred(byteCount int) {
	transferSendCounters.deferredFrames.Add(1)
	transferSendCounters.deferredBytes.Add(int64(byteCount))
}

// recordTransferResend records a resend. sendNumber is which send this is for the
// item: 2 for the first resend.
func recordTransferResend(byteCount int, sendNumber int) {
	transferSendCounters.resendFrames.Add(1)
	transferSendCounters.resendBytes.Add(int64(byteCount))
	bucket := 3
	switch {
	case sendNumber <= 2:
		bucket = 0
	case sendNumber <= 4:
		bucket = 1
	case sendNumber <= 8:
		bucket = 2
	}
	transferSendCounters.resendDepth[bucket].Add(1)
}

// Counters that say where transmitted bytes go and what the sequences are doing,
// beyond the first-send / resend split above.

// sequence exit reasons, in the order of sendSequenceExitNames
const (
	sendExitCancelled = iota
	sendExitAckTimeout
	sendExitIdle
	sendExitNoContract
	sendExitHeadError
	sendExitClosed
	sendExitReasonCount
)

var sendSequenceExitNames = [sendExitReasonCount]string{
	"cancelled", "ack_timeout", "idle", "no_contract", "head_error", "closed",
}

var transferMoreCounters struct {
	ackFrames atomic.Int64
	ackBytes  atomic.Int64

	clientSentFrames  atomic.Int64
	clientSentBytes   atomic.Int64
	controlSentFrames atomic.Int64
	controlSentBytes  atomic.Int64

	sequenceExits [sendExitReasonCount]atomic.Int64

	retainedBytes atomic.Int64

	releasedHeldBytes atomic.Int64
}

// recordTransferAckSent counts an acknowledgement frame this node wrote to a peer.
func recordTransferAckSent(byteCount int) {
	transferMoreCounters.ackFrames.Add(1)
	transferMoreCounters.ackBytes.Add(int64(byteCount))
}

// recordTransferDestinationSend counts a frame that reached a carrier, first send
// or resend, by whether its destination is the control plane or a client.
func recordTransferDestinationSend(control bool, byteCount int) {
	if control {
		transferMoreCounters.controlSentFrames.Add(1)
		transferMoreCounters.controlSentBytes.Add(int64(byteCount))
		return
	}
	transferMoreCounters.clientSentFrames.Add(1)
	transferMoreCounters.clientSentBytes.Add(int64(byteCount))
}

func recordSendSequenceExit(reason int) {
	transferMoreCounters.sequenceExits[reason].Add(1)
}

// TransferMoreStats is a snapshot of the counters above.
type TransferMoreStats struct {
	AckFrames         int64
	AckBytes          int64
	ClientSentFrames  int64
	ClientSentBytes   int64
	ControlSentFrames int64
	ControlSentBytes  int64
	// SequenceExits is keyed by SendSequenceExitReasons.
	SequenceExits map[string]int64
	// RetainedBytes is held right now in retained returns across all sequences.
	RetainedBytes int64
	// ReleasedHeldBytes is the undelivered bytes held for clients at the moment
	// they were released (or, in shadow, would have been).
	ReleasedHeldBytes int64
}

func TransferMoreStatsNow() TransferMoreStats {
	s := TransferMoreStats{
		AckFrames:         transferMoreCounters.ackFrames.Load(),
		AckBytes:          transferMoreCounters.ackBytes.Load(),
		ClientSentFrames:  transferMoreCounters.clientSentFrames.Load(),
		ClientSentBytes:   transferMoreCounters.clientSentBytes.Load(),
		ControlSentFrames: transferMoreCounters.controlSentFrames.Load(),
		ControlSentBytes:  transferMoreCounters.controlSentBytes.Load(),
		SequenceExits:     map[string]int64{},
		RetainedBytes:     transferMoreCounters.retainedBytes.Load(),
		ReleasedHeldBytes: transferMoreCounters.releasedHeldBytes.Load(),
	}
	for i, name := range sendSequenceExitNames {
		s.SequenceExits[name] = transferMoreCounters.sequenceExits[i].Load()
	}
	return s
}

// SendSequenceExitReasons lists the exit reason names in a stable order.
func SendSequenceExitReasons() []string {
	return append([]string(nil), sendSequenceExitNames[:]...)
}
