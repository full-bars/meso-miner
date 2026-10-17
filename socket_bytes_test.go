package connect

import (
	"net"
	"testing"
	"time"
)

func socketTotal(phase string, tx bool) int64 {
	stats := SocketBytesNow()
	if tx {
		return stats.Tx[phase]
	}
	return stats.Rx[phase]
}

// The same connection is counted as handshake while its connect attempt runs and
// as session once the attempt has finished. A connection made outside any
// attempt is other.
func TestSocketBytes_SplitByWhatTheConnectionIsDoing(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := server.Read(buf); err != nil {
				return
			}
		}
	}()

	attempt := &attemptStats{}
	conn := &trackedConn{Conn: client, attempt: attempt}

	handshakeBefore := socketTotal("handshake", true)
	sessionBefore := socketTotal("session", true)
	if _, err := conn.Write(make([]byte, 700)); err != nil {
		t.Fatal(err)
	}
	if socketTotal("handshake", true)-handshakeBefore != 700 || socketTotal("session", true) != sessionBefore {
		t.Fatal("bytes before the attempt finished must be handshake bytes")
	}

	attempt.done.Store(true)
	if _, err := conn.Write(make([]byte, 300)); err != nil {
		t.Fatal(err)
	}
	if socketTotal("session", true)-sessionBefore != 300 || socketTotal("handshake", true)-handshakeBefore != 700 {
		t.Fatal("bytes after the attempt finished must be session bytes")
	}

	otherBefore := socketTotal("other", true)
	standalone := &trackedConn{Conn: client}
	if _, err := standalone.Write(make([]byte, 50)); err != nil {
		t.Fatal(err)
	}
	if socketTotal("other", true)-otherBefore != 50 {
		t.Fatal("a connection outside any attempt must be other")
	}
}

func TestSocketBytes_FinishedAttemptsAreCountedByOutcome(t *testing.T) {
	find := func(name string) ConnectAttemptOutcome {
		for _, outcome := range ConnectAttemptOutcomesNow() {
			if outcome.Outcome == name {
				return outcome
			}
		}
		t.Fatalf("no outcome %q", name)
		return ConnectAttemptOutcome{}
	}
	before := find("socks")
	governor := newConnectGovernor(time.Now)
	stats := &attemptStats{}
	stats.rx.Store(1000)
	stats.tx.Store(2000)
	governor.recordAttempt(connectSocksFail, stats)
	after := find("socks")
	if after.Attempts-before.Attempts != 1 || after.RxBytes-before.RxBytes != 1000 || after.TxBytes-before.TxBytes != 2000 {
		t.Fatalf("outcome counters: %+v -> %+v", before, after)
	}
	if !stats.done.Load() {
		t.Fatal("recording an attempt must mark its connection as past the handshake")
	}
}
