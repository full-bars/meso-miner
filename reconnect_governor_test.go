package connect

import (
	"context"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestGovernor() (*connectGovernor, *time.Time) {
	now := time.Unix(1_000_000, 0)
	g := newConnectGovernor(func() time.Time { return now })
	// tests judge the verdict, not the startup grace: start the governor long ago
	g.started = now.Add(-time.Hour)
	return g, &now
}

func record(g *connectGovernor, class connectClass, n int) {
	for i := 0; i < n; i++ {
		g.recordAttempt(class, nil)
	}
}

func TestClassifyConnectError(t *testing.T) {
	reached := &attemptStats{}
	reached.rx.Store(platformReachedBytes + 1)
	silent := &attemptStats{}

	cases := []struct {
		name string
		err  error
		st   *attemptStats
		want connectClass
	}{
		{"nil error is a connect", nil, nil, connectOK},
		{"local teardown is ignored", context.Canceled, nil, connectCanceled},
		{"giving up after hearing the platform", errors.New("Timeout."), reached, connectTimeoutPlatform},
		{"giving up without hearing anything", errors.New("Timeout."), silent, connectTimeoutNoReply},
		{"giving up with no stats at all", errors.New("Timeout."), nil, connectTimeoutNoReply},
		{"deadline exceeded after hearing the platform", context.DeadlineExceeded, reached, connectTimeoutPlatform},
		{"socks failure", errors.New("socks connect tcp 1.2.3.4:1081->api: connection refused"), silent, connectSocksFail},
		{"dial failure", errors.New("dial tcp 1.2.3.4:1081: connect: no route to host"), silent, connectSocksFail},
		{"tls failure", errors.New("tls: handshake failure"), reached, connectTlsFail},
		{"certificate failure", errors.New("x509: certificate signed by unknown authority"), reached, connectTlsFail},
		{"upgrade rejected", errors.New("websocket: bad handshake"), reached, connectUpgradeFail},
		{"something unforeseen", errors.New("kaboom"), silent, connectOtherFail},
	}
	for _, c := range cases {
		if got := classifyConnectError(c.err, c.st); got != c.want {
			t.Errorf("%s: classified %v, want %v", c.name, got, c.want)
		}
	}
}

func TestGovernor_CountsAndExpires(t *testing.T) {
	g, now := newTestGovernor()
	record(g, connectOK, 3)
	record(g, connectTimeoutPlatform, 7)
	if s := g.snapshot(); s.Attempts != 10 || s.OK != 3 {
		t.Fatalf("got attempts=%d ok=%d, want 10 and 3", s.Attempts, s.OK)
	}
	*now = now.Add(govWindow + time.Second)
	if s := g.snapshot(); s.Attempts != 0 {
		t.Fatalf("attempts older than the window must expire, got %d", s.Attempts)
	}
}

func TestGovernor_HealthyBackendIsNotDegraded(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 95)
	record(g, connectTimeoutPlatform, 5)
	if g.snapshot().Degraded {
		t.Fatal("95% of attempts connecting is a healthy backend")
	}
}

func TestGovernor_AllFailuresAreDegraded(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutPlatform, 100)
	if !g.snapshot().Degraded {
		t.Fatal("100 straight platform timeouts must read as degraded")
	}
}

// The incident: the backend accepts the websocket and the session dies seconds
// later. Every connect "succeeds", so a plain success ratio never trips.
func TestGovernor_ConnectsThatDieYoungCountAsFailures(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 100)
	for i := 0; i < 100; i++ {
		g.recordEarlyDeath()
	}
	// the backend also visibly failed some attempts, as it did in the incident
	record(g, connectUpgradeFail, govMinHardFailures)
	s := g.snapshot()
	if !s.Degraded {
		t.Fatalf("100 connects that all died young, beside visible platform failures, must read as degraded: %+v", s)
	}
	if s.Early != 100 {
		t.Fatalf("early deaths = %d, want 100", s.Early)
	}
}

// Junk proxies fail forever whatever the backend does. They must not push a
// healthy node over the line.
func TestGovernor_DeadSocksProxiesDoNotTrip(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectSocksFail, 400)
	record(g, connectOK, 600)
	if g.snapshot().Degraded {
		t.Fatal("40% dead socks proxies is a proxy-list problem, not a backend outage")
	}
}

func TestGovernor_CanceledAttemptsAreIgnored(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectCanceled, 500)
	s := g.snapshot()
	if s.Attempts != 0 || s.Degraded {
		t.Fatalf("local teardown must not count as an attempt: %+v", s)
	}
}

func TestGovernor_TooFewAttemptsGiveNoVerdict(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutPlatform, govMinAttempts-1)
	if g.snapshot().Degraded {
		t.Fatal("a handful of failures is noise, not an outage")
	}
}

func TestGovernor_WouldHoldIsEverythingOverTheBudget(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutPlatform, 4000)
	s := g.snapshot()
	if want := int64(4000) - govShadowBudgetPerSecond*int64(govWindow/time.Second); s.WouldHold != want {
		t.Fatalf("would hold %d, want %d", s.WouldHold, want)
	}
	healthy, _ := newTestGovernor()
	record(healthy, connectOK, 4000)
	if healthy.snapshot().WouldHold != 0 {
		t.Fatal("a healthy backend must never report held attempts")
	}
}

func TestGovernor_AttributesBytesAndDialsToFailedAttempts(t *testing.T) {
	g, _ := newTestGovernor()
	failed := &attemptStats{}
	failed.dials.Store(4)
	failed.rx.Store(30_000)
	failed.tx.Store(10_000)
	ok := &attemptStats{}
	ok.dials.Store(1)
	ok.rx.Store(5_000)
	ok.tx.Store(2_000)

	g.recordAttempt(connectTimeoutPlatform, failed)
	g.recordAttempt(connectOK, ok)

	s := g.snapshot()
	if s.Dials != 5 || s.Bytes != 47_000 {
		t.Fatalf("dials=%d bytes=%d, want 5 and 47000", s.Dials, s.Bytes)
	}
	if s.FailedBytes != 40_000 {
		t.Fatalf("failed bytes = %d, want 40000", s.FailedBytes)
	}
}

func TestAttemptStats_ThroughContextAndConn(t *testing.T) {
	st := &attemptStats{}
	ctx := withAttemptStats(context.Background(), st)
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if attemptStatsFrom(child) != st {
		t.Fatal("stats must survive the derived contexts the dial path creates")
	}
	if attemptStatsFrom(context.Background()) != nil {
		t.Fatal("no stats on a plain context")
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	tc := &trackedConn{Conn: a, attempt: st}
	go func() {
		buf := make([]byte, 100)
		n, _ := b.Read(buf)
		b.Write(buf[:n/2])
	}()
	if _, err := tc.Write(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.Read(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if st.tx.Load() != 100 || st.rx.Load() != 50 {
		t.Fatalf("tx=%d rx=%d, want 100 and 50", st.tx.Load(), st.rx.Load())
	}
}

func TestGovernorReport_LogsTransitionsAndHeartbeat(t *testing.T) {
	g, _ := newTestGovernor()
	r := &govReporter{}

	record(g, connectOK, 100)
	if line, ok := r.next(g.snapshot()); ok {
		t.Fatalf("a healthy first tick should stay quiet, got %q", line)
	}

	record(g, connectTimeoutPlatform, 900)
	line, ok := r.next(g.snapshot())
	if !ok || !strings.Contains(line, "DEGRADED") {
		t.Fatalf("entering degraded must log, got %q ok=%v", line, ok)
	}
	if line, ok := r.next(g.snapshot()); !ok || !strings.Contains(line, "DEGRADED") {
		t.Fatalf("a degraded tick keeps logging, got %q ok=%v", line, ok)
	}

	healthy, _ := newTestGovernor()
	record(healthy, connectOK, 100)
	line, ok = r.next(healthy.snapshot())
	if !ok || !strings.Contains(line, "recovered") {
		t.Fatalf("leaving degraded must log, got %q ok=%v", line, ok)
	}
	for i := 0; i < govHeartbeatTicks-1; i++ {
		if _, ok := r.next(healthy.snapshot()); ok {
			t.Fatalf("healthy ticks stay quiet until the heartbeat (tick %d)", i)
		}
	}
	if line, ok := r.next(healthy.snapshot()); !ok || !strings.Contains(line, "ok") {
		t.Fatalf("healthy heartbeat missing, got %q ok=%v", line, ok)
	}
	_ = fmt.Sprint
}

// A proxy that accepts the connection and never answers fails the same way a
// dead proxy always has. It says nothing about the backend, so a poor proxy
// list on a healthy backend must not read as an outage.
func TestGovernor_SilentTimeoutsAreAmbiguous(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutNoReply, 900)
	record(g, connectOK, 100)
	if g.snapshot().Degraded {
		t.Fatal("silent timeouts are proxy trouble, not evidence against the platform")
	}
	only, _ := newTestGovernor()
	record(only, connectTimeoutNoReply, 900)
	if only.snapshot().Degraded {
		t.Fatal("nothing but silent timeouts is no evidence at all")
	}
}

func TestGovernor_TlsAndOtherFailuresAreAmbiguous(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTlsFail, 400)
	record(g, connectOtherFail, 400)
	record(g, connectOK, 100)
	if g.snapshot().Degraded {
		t.Fatal("tls and unclassified failures can be a proxy tampering with the connection")
	}
}

// The platform answered and said no: that is the backend talking.
func TestGovernor_RejectedUpgradesCount(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectUpgradeFail, 100)
	if !g.snapshot().Degraded {
		t.Fatal("a flood of rejected upgrades is the platform failing")
	}
}

// A poor proxy list mixed with a backend that is working: the connects that do
// happen are good, everything else is ambiguous, so the verdict is healthy.
func TestGovernor_PoorProxyListOnAHealthyBackendIsHealthy(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectSocksFail, 500)
	record(g, connectTimeoutNoReply, 300)
	record(g, connectTlsFail, 50)
	record(g, connectOK, 150)
	record(g, connectTimeoutPlatform, 10)
	if g.snapshot().Degraded {
		t.Fatalf("a mostly dead proxy list must not look like an outage: %+v", g.snapshot())
	}
}

// The label must say what the governor is actually doing. In enforce mode the
// line used to claim "observing only", which sent an operator looking for a
// shadow-mode setting that was not on.
func TestGovernorReport_NamesTheModeHonestly(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutPlatform, 100)

	enforcing := &govReporter{mode: func() governMode { return governEnforce }, rate: func() float64 { return 8 }, waiting: func() int { return 321 }}
	line, ok := enforcing.next(g.snapshot())
	if !ok || !strings.Contains(line, "enforcing") || strings.Contains(line, "observing only") {
		t.Fatalf("enforce mode must say it is enforcing, got %q", line)
	}
	for _, want := range []string{"mode=enforce", "rate=8.0/s", "waiting=321"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}

	shadow := &govReporter{mode: func() governMode { return governShadow }}
	line, _ = shadow.next(g.snapshot())
	if !strings.Contains(line, "observing only") || !strings.Contains(line, "mode=shadow") {
		t.Fatalf("shadow mode must say it only observes, got %q", line)
	}
}

func TestGovernorStatusNow_ReportsModeRateAndWaiting(t *testing.T) {
	defer setGovernModeForTest(governEnforce)()
	connectBudget.setRate(25)
	defer connectBudget.setRate(budgetCeilingRate)

	s := GovernorStatusNow()
	if s.Mode != "enforce" {
		t.Fatalf("mode = %q, want enforce", s.Mode)
	}
	if s.BudgetRate != 25 {
		t.Fatalf("budget rate = %v, want 25", s.BudgetRate)
	}
	if s.Verdict != "ok" && s.Verdict != "degraded" {
		t.Fatalf("verdict = %q", s.Verdict)
	}
	restore := setGovernModeForTest(governOff)
	defer restore()
	if got := GovernorStatusNow().Mode; got != "off" {
		t.Fatalf("mode = %q, want off", got)
	}
}

// Sessions that die young with NO visible platform failure beside them are as
// likely to be proxies resetting connections (rotating residential IPs, a flaky
// list) as a struggling backend. The flap guard already slows such proxies one by
// one; the fleet-wide verdict, which throttles every reconnect, needs the
// platform itself to have visibly failed some attempts.
func TestGovernor_FlappingProxiesAloneAreNotABackendOutage(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 200)
	for i := 0; i < 200; i++ {
		g.recordEarlyDeath()
	}
	if g.snapshot().Degraded {
		t.Fatal("proxy-side flapping with no platform failure must not throttle the fleet")
	}
}

func TestGovernor_HardFailuresAreTimeoutsAfterTheAnswerAndRejectedUpgrades(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectTimeoutPlatform, 2)
	record(g, connectUpgradeFail, 1)
	record(g, connectTimeoutNoReply, 50)
	record(g, connectSocksFail, 50)
	if got := g.snapshot().HardFailures; got != 3 {
		t.Fatalf("hard failures = %d, want 3 (silent timeouts and socks failures are not hard)", got)
	}
}

// A SOCKS handshake that timed out is the proxy's failure, however many bytes the
// attempt's earlier dials moved. These are the real lines from a 1,100-proxy host
// whose backend was healthy: the "last:" cause of every flagged timeout.
func TestClassify_SocksHandshakeTimeoutsAreProxyFailuresNotPlatformOnes(t *testing.T) {
	stats := &attemptStats{}
	stats.rx.Store(6000) // earlier dials in the same attempt moved plenty
	for _, msg := range []string{
		"socks connect tcp 45.74.31.41:10062->65.49.70.70:443: read tcp 66.187.7.83:4806->45.74.31.41:10062: i/o timeout",
		"socks connect tcp 156.225.20.83:1080->65.49.70.70:443: read tcp 66.187.7.83:4806->156.225.20.83:1080: i/o timeout",
		"read tcp 66.187.7.83:4806->156.225.20.83:1080: i/o timeout (proxy 156.225.20.83:1080 unreachable)",
	} {
		err := &dialTimeoutError{failures: 2, rounds: 1, last: errors.New(msg)}
		if got := classifyConnectError(err, stats); got != connectSocksFail {
			t.Errorf("%q classified %v, want socks", msg, got)
		}
	}
	// a timeout waiting on the platform itself, through an established tunnel, still counts
	err := &dialTimeoutError{failures: 1, rounds: 1, last: errors.New("read tcp 66.187.7.83:4806->45.74.31.41:10062: i/o timeout")}
	if got := classifyConnectError(err, stats); got != connectTimeoutPlatform {
		t.Errorf("a post-handshake timeout classified %v, want timeout_platform", got)
	}
}

// A host whose proxy list is chronically poor (about 30% of platform attempts
// good) is not in an outage at 17%. The verdict compares each window with what
// this host normally sees, so a poor list on a healthy backend does not read as a
// failing backend, while a real collapse still does.
func windowWith(g *connectGovernor, now *time.Time, good, bad int) {
	record(g, connectOK, good)
	record(g, connectTimeoutPlatform, bad)
	g.learnBaseline()
	*now = now.Add(61 * time.Second) // age the window out
}

func TestGovernor_ChronicallyPoorHostIsNotDegradedByItsUsualRatio(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 9, 21) // 30% good, every minute
	}
	record(g, connectOK, 5)
	record(g, connectTimeoutPlatform, 25) // 17%: below the plain 20% line
	if g.snapshot().Degraded {
		t.Fatalf("17%% good on a host that normally sees 30%% is noise: %+v", g.snapshot())
	}
}

func TestGovernor_ChronicallyPoorHostStillSeesARealCollapse(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 9, 21)
	}
	record(g, connectOK, 1)
	record(g, connectTimeoutPlatform, 29) // 3% good
	if !g.snapshot().Degraded {
		t.Fatalf("a collapse from 30%% to 3%% must read as degraded: %+v", g.snapshot())
	}
}

func TestGovernor_HealthyHostKeepsThePlainThreshold(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 28, 2) // 93% good
	}
	record(g, connectOK, 4)
	record(g, connectTimeoutPlatform, 26) // 13%
	if !g.snapshot().Degraded {
		t.Fatalf("a healthy host dropping to 13%% good must read as degraded: %+v", g.snapshot())
	}
}

func TestGovernor_NoBaselineMeansThePlainThreshold(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 5)
	record(g, connectTimeoutPlatform, 25)
	if !g.snapshot().Degraded {
		t.Fatal("with nothing learned yet the plain 20% line applies (a start in the middle of an outage)")
	}
}

// A slow decline must not teach the baseline that an outage is normal.
func TestGovernor_BaselineDoesNotFollowACollapseDown(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 28, 2) // 93%
	}
	for i := 0; i < 30; i++ {
		windowWith(g, now, 1, 29) // 3%, a long outage
	}
	record(g, connectOK, 1)
	record(g, connectTimeoutPlatform, 29)
	if !g.snapshot().Degraded {
		t.Fatal("thirty minutes of collapse must not become the new normal")
	}
}

// A host that starts in the middle of an accept-then-die-young episode must not
// learn that 5% good is normal.
func TestGovernor_BaselineIsNotSeededFromAnOutageWindow(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 5; i++ {
		record(g, connectOK, 100)
		for j := 0; j < 95; j++ {
			g.recordEarlyDeath() // sessions accepted then dropped: 5% good, no hard failures
		}
		g.learnBaseline()
		*now = now.Add(61 * time.Second)
	}
	record(g, connectOK, 2)
	record(g, connectTimeoutPlatform, 40)
	if !g.snapshot().Degraded {
		t.Fatalf("a window learned during an outage must not hide a later collapse: %+v", g.snapshot())
	}
}

// The threshold never drops below a floor, whatever the baseline.
func TestGovernor_ThresholdHasAFloor(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 6, 14) // exactly 30%
	}
	g.mu.Lock()
	g.baseline = 0.1 // as if learned very low
	g.mu.Unlock()
	record(g, connectOK, 2)
	record(g, connectTimeoutPlatform, 38) // 5%
	if s := g.snapshot(); s.Threshold < govMinThreshold || !s.Degraded {
		t.Fatalf("threshold %v below the floor %v or 5%% good not degraded: %+v", s.Threshold, govMinThreshold, s)
	}
}

// A backend that degrades over a quarter of an hour must not drag the baseline
// down with it: a window at half the baseline is not degraded, and the baseline
// follows downward only very slowly.
func TestGovernor_SlowDeclineDoesNotErodeTheBaseline(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 28, 2) // 93%
	}
	for i := 0; i < 15; i++ {
		windowWith(g, now, 14, 16) // 47%: half the baseline, not yet degraded
	}
	g.mu.Lock()
	b := g.baseline
	g.mu.Unlock()
	if b < 0.75 {
		t.Fatalf("baseline eroded to %.2f over a fifteen minute decline", b)
	}
}

// An overloaded platform that drops SYNs makes a LATER round's SOCKS CONNECT
// hang. The earlier round that reached the platform and timed out after the TLS
// handshake is the real evidence, and the last error must not erase it.
func TestClassify_AnEarlierPlatformFailureSurvivesALaterSocksTimeout(t *testing.T) {
	stats := &attemptStats{}
	stats.rx.Store(6000)
	rec := &evalErrorRecorder{}
	rec.note(errors.New("read tcp 66.187.7.83:4806->45.74.31.41:10062: i/o timeout")) // through the tunnel
	rec.note(errors.New("socks connect tcp 45.74.31.41:10062->65.49.70.70:443: read tcp 66.187.7.83:4806->45.74.31.41:10062: i/o timeout"))
	err := rec.timeoutError(15 * time.Second)
	if got := classifyConnectError(err, stats); got != connectTimeoutPlatform {
		t.Fatalf("classified %v, want timeout_platform: a later socks timeout hid an earlier platform failure", got)
	}
	// with only proxy-level failures it stays a proxy failure
	rec2 := &evalErrorRecorder{}
	rec2.note(errors.New("socks connect tcp a->b: i/o timeout"))
	rec2.note(errors.New("socks connect tcp c->d: i/o timeout"))
	if got := classifyConnectError(rec2.timeoutError(time.Second), stats); got != connectSocksFail {
		t.Fatalf("only proxy-level failures classified %v, want socks", got)
	}
}

// One burst window (the whole fleet reconnecting at once after a platform-side
// drop, 600 clean connects in a minute) says nothing about what this host's
// proxies usually do. It must not teach the baseline that 90% good is normal, or
// the next ordinary window of a poor list reads as degraded. Seen on a live host:
// 683 attempts at 23:19, then a plain 14%-good window flagged DEGRADED at 23:25.
func TestGovernor_AFleetWideReconnectBurstDoesNotRaiseTheBaseline(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 10; i++ {
		windowWith(g, now, 9, 21) // 30% good, the host's usual
	}
	windowWith(g, now, 600, 10) // everything reconnects cleanly at once
	for i := 0; i < 2; i++ {
		windowWith(g, now, 9, 21)
	}
	record(g, connectOK, 6)
	record(g, connectTimeoutPlatform, 38) // 14% good: this host's bad minute
	if g.snapshot().Degraded {
		t.Fatalf("an ordinary bad minute read as degraded after a reconnect burst: %+v", g.snapshot())
	}
}

// A server-side client-limit close or an explicit 401/403 is the platform
// enforcing a rule, not the backend failing. Counting it as a hard failure lets a
// token or limit change throttle a whole host whose backend is perfectly healthy.
func TestClassify_RefusalsAndClientLimitAreNotPlatformEvidence(t *testing.T) {
	stats := &attemptStats{}
	stats.rx.Store(6000)
	refused := []error{
		&wsStatusError{status: 401, err: errors.New("websocket: bad handshake")},
		&wsStatusError{status: 403, err: errors.New("websocket: bad handshake")},
		&websocket.CloseError{Code: ClientLimitCloseCode, Text: "client limit"},
		&dialTimeoutError{failures: 1, rounds: 1, rejected: true, last: errors.New("i/o timeout")},
	}
	for _, err := range refused {
		if got := classifyConnectError(err, stats); got != connectRefused {
			t.Errorf("%v classified %v, want refused", err, got)
		}
	}
	// overload is still the platform failing
	for _, status := range []int{429, 500, 502, 503, 499} {
		err := &wsStatusError{status: status, err: errors.New("websocket: bad handshake")}
		if got := classifyConnectError(err, stats); got != connectUpgradeFail {
			t.Errorf("http %d classified %v, want upgrade", status, got)
		}
	}
	g, _ := newTestGovernor()
	record(g, connectRefused, 200)
	if s := g.snapshot(); s.Degraded || s.PlatformAttempts != 0 {
		t.Fatalf("refusals must carry no weight in the verdict: %+v", s)
	}
}

// The incident's own shape: the platform accepts the websocket and then drops it,
// so every connect "succeeds" and the session dies within seconds. No attempt
// fails, so only the early deaths can say so.
func TestGovernor_AcceptThenDropIsDegraded(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 100)
	for i := 0; i < 95; i++ {
		g.recordProvenEarlyDeath() // proxies that had held a stable session before
	}
	s := g.snapshot()
	if !s.Degraded {
		t.Fatalf("100 connects of which 95 died young must read as degraded: %+v", s)
	}
	if s.HardFailures < 95 {
		t.Fatalf("early deaths are hard failures: %+v", s)
	}
}

// A host whose proxies always drop young (a poor residential list) learns that as
// its normal, and is not degraded by it.
func TestGovernor_ChronicEarlyDeathsAreLearnedNotFlagged(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 10; i++ {
		record(g, connectOK, 20)
		for j := 0; j < 14; j++ {
			g.recordProvenEarlyDeath() // 30% of connects live
		}
		g.learnBaseline()
		*now = now.Add(61 * time.Second)
	}
	record(g, connectOK, 20)
	for j := 0; j < 15; j++ {
		g.recordProvenEarlyDeath()
	}
	if g.snapshot().Degraded {
		t.Fatalf("this host's usual early deaths read as an outage: %+v", g.snapshot())
	}
}

// Early deaths of proxies that never lived long are ambiguous (a poor proxy dies
// young whatever the backend does), so they alone cannot make a verdict.
func TestGovernor_UnprovenEarlyDeathsAreNotHardEvidence(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 100)
	for i := 0; i < 95; i++ {
		g.recordEarlyDeath()
	}
	if g.snapshot().Degraded {
		t.Fatalf("unproven early deaths alone must not read as an outage: %+v", g.snapshot())
	}
}

func TestFlapGuard_ProvenOnceASessionHasLivedLongEnough(t *testing.T) {
	f := newFlapGuard(time.Minute, time.Second)
	f.jitter = func(d time.Duration) time.Duration { return d }
	if f.everStable {
		t.Fatal("a new guard has seen no stable session")
	}
	f.sessionEnded(5 * time.Second)
	if f.everStable {
		t.Fatal("a young session does not prove a proxy")
	}
	f.sessionEnded(2 * time.Minute)
	if !f.everStable {
		t.Fatal("a stable session proves the proxy")
	}
	f.sessionEnded(3 * time.Second)
	if !f.everStable {
		t.Fatal("once proven, a later young session does not unprove it")
	}
}

func TestGovLine_CarriesTheBaselineAndThreshold(t *testing.T) {
	g, now := newTestGovernor()
	for i := 0; i < 8; i++ {
		windowWith(g, now, 9, 21)
	}
	record(g, connectOK, 5)
	line := govLine("ok", g.snapshot())
	for _, want := range []string{"baseline=30%", "threshold=12%", "refused=0"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

// A restart on a host with a poor proxy list must not forget what is normal for
// it, or its first minutes are judged by the blunt 20% line and read as an outage.
func TestGovernor_BaselineSurvivesARestart(t *testing.T) {
	path := t.TempDir() + "/govern_baseline"
	g1, now := newTestGovernor()
	g1.baselinePath = path
	for i := 0; i < 10; i++ {
		windowWith(g1, now, 9, 21) // 30% good
	}
	g1.saveBaseline() // as the minute tick does

	g2, _ := newTestGovernor()
	g2.restoreBaseline(path, time.Now())
	if b := g2.snapshot().Baseline; b < 0.25 || 0.35 < b {
		t.Fatalf("restored baseline = %.2f, want about 0.30", b)
	}
	record(g2, connectOK, 6)
	record(g2, connectTimeoutPlatform, 38) // 14%: this host's bad minute, right after restart
	if g2.snapshot().Degraded {
		t.Fatalf("a restart forgot what is normal for the host: %+v", g2.snapshot())
	}
}

func TestGovernor_RestoreIgnoresStaleOrNonsenseBaselines(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"junk":  "banana",
		"zero":  "0.0",
		"huge":  "7.5",
		"tiny":  "0.001",
		"empty": "",
	} {
		p := dir + "/" + name
		os.WriteFile(p, []byte(content), 0o600)
		g, _ := newTestGovernor()
		g.restoreBaseline(p, time.Now())
		if g.snapshot().Baseline != 0 {
			t.Errorf("%s: restored a baseline from %q", name, content)
		}
	}
	// stale
	p := dir + "/stale"
	os.WriteFile(p, []byte("0.4"), 0o600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(p, old, old)
	g, _ := newTestGovernor()
	g.restoreBaseline(p, time.Now())
	if g.snapshot().Baseline != 0 {
		t.Error("restored a month-old baseline")
	}
	// missing
	g.restoreBaseline(dir+"/absent", time.Now())
}

// A start (cold, hot swap or restart) is a stampede: thousands of proxies dial at
// once, and for a while most attempts fail on the host's own congestion. That is
// not the backend failing, and throttling it slows the start the owner requires
// to stay fast. For a short grace the verdict is not trusted. (A restart that
// finds a fresh outage marker is a different case: it starts throttled on purpose.)
func TestGovernor_StartupStampedeIsNotAnOutage(t *testing.T) {
	g, now := newTestGovernor()
	g.started = *now                       // a governor that has only just started
	record(g, connectTimeoutPlatform, 146) // the first minute of a 1,100-proxy start
	s := g.snapshot()
	if s.Degraded {
		t.Fatalf("the startup stampede read as an outage: %+v", s)
	}
	if !s.StartupGrace {
		t.Fatal("the snapshot should say it is inside the startup grace")
	}
	*now = now.Add(govStartupGrace + time.Second)
	record(g, connectTimeoutPlatform, 146)
	if !g.snapshot().Degraded {
		t.Fatal("after the grace the same failures must read as an outage")
	}
}

// One burst of clean connects at a start says nothing about what the host's
// proxies usually do. The baseline is seeded from several windows and takes the
// lowest, so a start cannot leave it far above the host's normal (which would
// also raise the bar the budget needs to reopen).
func TestGovernor_BaselineNeedsSeveralWindowsAndTakesTheLowest(t *testing.T) {
	g, now := newTestGovernor()
	windowWith(g, now, 600, 10) // a start burst: 98% good
	if b := g.snapshot().Baseline; b != 0 {
		t.Fatalf("one window seeded the baseline: %.2f", b)
	}
	windowWith(g, now, 9, 21)  // 30%
	windowWith(g, now, 10, 20) // 33%
	b := g.snapshot().Baseline
	if b < 0.28 || 0.32 < b {
		t.Fatalf("baseline = %.2f, want the lowest of the first windows (about 0.30), not the burst", b)
	}
}

func TestGovLine_SaysWhenItIsInsideTheStartupGrace(t *testing.T) {
	g, now := newTestGovernor()
	g.started = *now
	if line := govLine("ok", g.snapshot()); !strings.Contains(line, "startup_grace=yes") {
		t.Fatalf("no startup_grace in %q", line)
	}
	*now = now.Add(govStartupGrace + time.Second)
	if line := govLine("ok", g.snapshot()); strings.Contains(line, "startup_grace") {
		t.Fatalf("startup_grace after the grace: %q", line)
	}
}
