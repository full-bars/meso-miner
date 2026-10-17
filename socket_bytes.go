package connect

import "sync/atomic"

// Where the bytes on the proxy sockets go.
//
// A provider's total traffic is every byte on its proxy sockets, and almost none
// of it is client payload. A node with no clients can still move megabits. These
// counters split that total by what the connection was doing, so the unexplained
// part can be named instead of guessed:
//
//   handshake: the proxy dial, TLS and websocket upgrade of a platform connect
//              attempt that had not finished yet (failed attempts included)
//   session:   the same connection after its attempt finished: transfer frames,
//              acks, pings and TLS record overhead
//   other:     connections made outside a connect attempt (health probes, speed
//              tests, URL fetches, anything else dialled through a proxy)

type socketPhase int

const (
	socketPhaseHandshake socketPhase = iota
	socketPhaseSession
	socketPhaseOther
	socketPhaseCount
)

var socketPhaseNames = [socketPhaseCount]string{"handshake", "session", "other"}

// [phase][0=rx, 1=tx]
var socketByteCounters [socketPhaseCount][2]atomic.Int64

func (self *trackedConn) socketPhase() socketPhase {
	if self.attempt == nil {
		return socketPhaseOther
	}
	if self.attempt.done.Load() {
		return socketPhaseSession
	}
	return socketPhaseHandshake
}

func recordSocketBytes(phase socketPhase, tx bool, byteCount int) {
	direction := 0
	if tx {
		direction = 1
	}
	socketByteCounters[phase][direction].Add(int64(byteCount))
}

// SocketBytesStats is the proxy socket byte count by phase and direction.
type SocketBytesStats struct {
	// Rx and Tx are keyed by SocketPhaseNames.
	Rx map[string]int64
	Tx map[string]int64
}

func SocketBytesNow() SocketBytesStats {
	stats := SocketBytesStats{Rx: map[string]int64{}, Tx: map[string]int64{}}
	for phase, name := range socketPhaseNames {
		stats.Rx[name] = socketByteCounters[phase][0].Load()
		stats.Tx[name] = socketByteCounters[phase][1].Load()
	}
	return stats
}

func SocketPhaseNames() []string { return append([]string(nil), socketPhaseNames[:]...) }

// What finished connect attempts cost, by outcome: [class][0=rx, 1=tx, 2=count].
var connectAttemptCounters [connectClassCount][3]atomic.Int64

func recordConnectAttemptBytes(class connectClass, rx int64, tx int64) {
	if class >= connectClassCount {
		return
	}
	connectAttemptCounters[class][0].Add(rx)
	connectAttemptCounters[class][1].Add(tx)
	connectAttemptCounters[class][2].Add(1)
}

// ConnectAttemptOutcome is the cost of the attempts that ended one way.
type ConnectAttemptOutcome struct {
	Outcome  string
	Attempts int64
	RxBytes  int64
	TxBytes  int64
}

func ConnectAttemptOutcomesNow() []ConnectAttemptOutcome {
	outcomes := make([]ConnectAttemptOutcome, 0, int(connectClassCount))
	for class := connectClass(0); class < connectClassCount; class++ {
		outcomes = append(outcomes, ConnectAttemptOutcome{
			Outcome:  class.String(),
			RxBytes:  connectAttemptCounters[class][0].Load(),
			TxBytes:  connectAttemptCounters[class][1].Load(),
			Attempts: connectAttemptCounters[class][2].Load(),
		})
	}
	return outcomes
}

// Keepalive frames on the platform transports. They are empty websocket
// messages: tiny each, but a node holds a transport per proxy.
var transportPingFrames struct {
	tx atomic.Int64
	rx atomic.Int64
}

// TransportPingFramesNow returns keepalive frames written and received.
func TransportPingFramesNow() (tx int64, rx int64) {
	return transportPingFrames.tx.Load(), transportPingFrames.rx.Load()
}
