package connect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func sessionTotals() map[string]int64 {
	stats := SessionEndStatsNow()
	totals := map[string]int64{}
	for _, byReason := range stats.Cumulative {
		for reason, cumulative := range byReason {
			totals[reason] += cumulative[len(cumulative)-1]
		}
	}
	return totals
}

// A session that ends is counted under why it ended and how long it lived.
func TestSessionEnd_CountsByReasonAndLifetime(t *testing.T) {
	before := sessionTotals()
	beforeStats := SessionEndStatsNow()

	alive := context.Background()
	recordSessionEnd(alive, 20*time.Second, false, "url") // remote, <=30s
	recordSessionEnd(alive, 20*time.Minute, false, "url") // remote, <=30m
	recordSessionEnd(alive, 5*time.Second, true, "file")  // client limit, <=10s
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	recordSessionEnd(cancelled, 2*time.Hour, false, "file") // local, <=6h, even if it was also young

	after := sessionTotals()
	if after["remote"]-before["remote"] != 2 || after["client_limit"]-before["client_limit"] != 1 || after["local"]-before["local"] != 1 {
		t.Fatalf("totals %v -> %v", before, after)
	}
	afterStats := SessionEndStatsNow()
	bound := func(seconds float64) int {
		for i, b := range afterStats.BoundsSeconds {
			if b == seconds {
				return i
			}
		}
		t.Fatalf("no bucket for %gs", seconds)
		return -1
	}
	remote30s, remote30m := bound(30), bound(1800)
	if afterStats.Cumulative["url"]["remote"][remote30s]-beforeStats.Cumulative["url"]["remote"][remote30s] != 1 {
		t.Fatal("the 20s session must be in the <=30s bucket")
	}
	if afterStats.Cumulative["url"]["remote"][remote30m]-beforeStats.Cumulative["url"]["remote"][remote30m] != 2 {
		t.Fatal("both remote sessions must be in the <=30m bucket (cumulative)")
	}
}

// Only the main transport is counted, so an auxiliary H3 beside H1 does not
// double count one node's churn.
func TestSessionEnd_AuxiliaryTransportIsNotCounted(t *testing.T) {
	before := sessionTotals()
	flaps := newFlapGuard(time.Minute, time.Second)
	sessionEndWait(context.Background(), flaps, 5*time.Second, false, "aux", NewNoopLogger(), false, "url")
	if after := sessionTotals(); after["remote"] != before["remote"] {
		t.Fatalf("an auxiliary session was counted: %v -> %v", before, after)
	}
	sessionEndWait(context.Background(), flaps, 5*time.Second, false, "main", NewNoopLogger(), true, "url")
	if after := sessionTotals(); after["remote"]-before["remote"] != 1 {
		t.Fatalf("the main transport's session was not counted: %v -> %v", before, after)
	}
}

// Many remote closes inside a few seconds are one burst, logged once; the same
// number spread over minutes is not.
func TestRemoteCloseBursts_DetectsSimultaneousClosesNotAScatter(t *testing.T) {
	var lines []string
	detector := &remoteCloseBurstDetector{emit: func(line string) { lines = append(lines, line) }}
	base := time.Unix(5_000_000, 0)

	for i := 0; i < remoteCloseBurstMin*3; i++ { // spread over ~4 minutes: never a burst
		detector.note(base.Add(time.Duration(i) * 1600 * time.Millisecond))
	}
	if bursts, _ := func() (int64, int64) { return detector.bursts, detector.maxBurst }(); bursts != 0 || len(lines) != 0 {
		t.Fatalf("a scatter was reported as a burst: bursts=%d lines=%v", bursts, lines)
	}

	burstStart := base.Add(time.Hour)
	for i := 0; i < 80; i++ { // 80 closes inside 4 seconds
		detector.note(burstStart.Add(time.Duration(i) * 50 * time.Millisecond))
	}
	if detector.bursts != 1 || detector.maxBurst != 80 {
		t.Fatalf("burst accounting: bursts=%d largest=%d", detector.bursts, detector.maxBurst)
	}
	if len(lines) != 1 || !containsAll(lines[0], "mass disconnect") {
		t.Fatalf("a burst must be logged once: %v", lines)
	}
	// a second burst a minute later is a second burst and logs again
	second := burstStart.Add(2 * time.Minute)
	for i := 0; i < remoteCloseBurstMin; i++ {
		detector.note(second.Add(time.Duration(i) * 10 * time.Millisecond))
	}
	if detector.bursts != 2 || len(lines) != 2 {
		t.Fatalf("second burst: bursts=%d lines=%d", detector.bursts, len(lines))
	}
}

func TestTransportCloseCause_ClassifiesTheErrorsALoopSees(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&websocket.CloseError{Code: websocket.CloseNormalClosure}, "ws_normal"},
		{&websocket.CloseError{Code: websocket.CloseGoingAway}, "ws_going_away"},
		{&websocket.CloseError{Code: websocket.CloseAbnormalClosure}, "ws_abnormal"},
		{&websocket.CloseError{Code: 4003}, "ws_app"},
		{os.ErrDeadlineExceeded, "timeout"},
		{fmt.Errorf("read tcp: %w", syscall.ECONNRESET), "reset"},
		{io.EOF, "eof"},
		{io.ErrUnexpectedEOF, "eof"},
		{net.ErrClosed, "closed_local"},
		{errors.New("something else"), "other"},
	}
	for _, c := range cases {
		if got := transportCloseCauseNames[classifyTransportCloseCause(c.err)]; got != c.want {
			t.Errorf("%v classified as %q, want %q", c.err, got, c.want)
		}
	}
	before := map[string]int64{}
	for _, cause := range TransportCloseCausesNow() {
		before[cause.Source+cause.Mode+cause.Side+cause.Cause] = cause.Count
	}
	recordTransportCloseCause("url", "h1", "read", io.EOF)
	recordTransportCloseCause("file", "h3", "write", os.ErrDeadlineExceeded)
	recordTransportCloseCause("url", "h1", "read", nil) // no error: not counted
	for _, cause := range TransportCloseCausesNow() {
		delta := cause.Count - before[cause.Source+cause.Mode+cause.Side+cause.Cause]
		want := int64(0)
		if (cause.Source == "url" && cause.Mode == "h1" && cause.Side == "read" && cause.Cause == "eof") ||
			(cause.Source == "file" && cause.Mode == "h3" && cause.Side == "write" && cause.Cause == "timeout") {
			want = 1
		}
		if delta != want {
			t.Errorf("%s/%s/%s moved by %d, want %d", cause.Mode, cause.Side, cause.Cause, delta, want)
		}
	}
}
