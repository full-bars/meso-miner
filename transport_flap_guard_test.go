package connect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func identityJitter(d time.Duration) time.Duration { return d }

func newTestFlapGuard(stableAfter time.Duration, base time.Duration) *flapGuard {
	g := newFlapGuard(stableAfter, base)
	g.jitter = identityJitter
	return g
}

// The governor keeps its counts as a 60-second sliding window, so a
// before/after diff of connectGov.snapshot() measures "what this test added
// minus what earlier tests recorded up to a minute ago and then aged out". In
// a wider -run set that subtraction shows up and can even go negative, which
// is what made TestRunH1_FeedsTheGovernor flaky. A test that diffs the window
// clears it first, so the diff is exactly what the test recorded. The ring is
// written by packet loops under the same mutex, so the clear is race-clean.
func resetGovernorWindow(t *testing.T) {
	t.Helper()
	connectGov.mu.Lock()
	defer connectGov.mu.Unlock()
	for i := range connectGov.ring {
		connectGov.ring[i] = govBucket{}
	}
}

func TestFlapGuard_StableSessionNeedsNoExtraWait(t *testing.T) {
	g := newTestFlapGuard(60*time.Second, 5*time.Second)
	if got := g.sessionEnded(2 * time.Hour); got != 0 {
		t.Fatalf("a session that lived 2h must reconnect without extra wait, got %s", got)
	}
}

func TestFlapGuard_YoungSessionsEscalate(t *testing.T) {
	g := newTestFlapGuard(60*time.Second, 5*time.Second)
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, w := range want {
		if got := g.sessionEnded(3 * time.Second); got != w {
			t.Fatalf("young session %d: wait %s, want %s", i, got, w)
		}
	}
}

func TestFlapGuard_StableSessionClearsStreak(t *testing.T) {
	g := newTestFlapGuard(60*time.Second, 5*time.Second)
	g.sessionEnded(time.Second)
	g.sessionEnded(time.Second)
	if got := g.sessionEnded(61 * time.Second); got != 0 {
		t.Fatalf("stable session must not wait, got %s", got)
	}
	if got := g.sessionEnded(time.Second); got != 5*time.Second {
		t.Fatalf("after a stable session the streak starts over at the base, got %s", got)
	}
}

func TestFlapGuard_SessionJustUnderThresholdCounts(t *testing.T) {
	g := newTestFlapGuard(60*time.Second, 5*time.Second)
	if got := g.sessionEnded(60*time.Second - time.Nanosecond); got == 0 {
		t.Fatal("a session shorter than the stable threshold is a flap")
	}
	if got := g.sessionEnded(60 * time.Second); got != 0 {
		t.Fatalf("a session exactly at the threshold is stable, got %s", got)
	}
}

func TestFlapGuard_UnsetBaseFallsBackToOneSecond(t *testing.T) {
	g := newTestFlapGuard(60*time.Second, 0)
	if got := g.sessionEnded(time.Second); got != time.Second {
		t.Fatalf("unset base must not make a tight loop, got %s", got)
	}
}

func TestFlapGuard_NegativeStableAfterDisables(t *testing.T) {
	g := newTestFlapGuard(-1, 5*time.Second)
	for i := 0; i < 5; i++ {
		if got := g.sessionEnded(time.Millisecond); got != 0 {
			t.Fatalf("disabled guard must never wait, got %s", got)
		}
	}
}

func TestFlapGuard_JitterStaysWithinBand(t *testing.T) {
	g := newFlapGuard(60*time.Second, 10*time.Second)
	for i := 0; i < 200; i++ {
		g2 := newFlapGuard(60*time.Second, 10*time.Second)
		got := g2.sessionEnded(time.Second)
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("first wait %s outside the +-20%% band around 10s", got)
		}
	}
	_ = g
}

func TestFlapGuard_CountsEngagedBackoffs(t *testing.T) {
	before := TransportFlapBackoffs()
	g := newTestFlapGuard(60*time.Second, time.Second)
	g.sessionEnded(time.Hour)
	g.sessionEnded(time.Second)
	g.sessionEnded(time.Second)
	if got := TransportFlapBackoffs() - before; got != 2 {
		t.Fatalf("expected 2 engaged backoffs counted, got %d", got)
	}
}

// flapServer upgrades every websocket and holds it for hold before closing, so
// a client sees a connect that succeeds and then dies.
func flapServer(hold time.Duration, accepts *atomic.Int64, stamps *flapStamps) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepts.Add(1)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		stamps.add(time.Now())
		if 0 < hold {
			time.Sleep(hold)
		}
		ws.Close()
	}))
}

type flapStamps struct {
	mu sync.Mutex
	at []time.Time
}

func (s *flapStamps) add(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at = append(s.at, t)
}

func (s *flapStamps) snapshot() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.at...)
}

func newFlapTestTransport(t *testing.T, ctx context.Context, serverUrl string, settings *PlatformTransportSettings) *PlatformTransport {
	t.Helper()
	// the default strategy spaces every connect 100-1000ms apart, which would
	// mask the loop's own behavior; these tests are about the loop, not that pacing
	strategySettings := DefaultClientStrategySettings()
	strategySettings.MinNextConnectDelay = 0
	strategySettings.MaxNextConnectDelay = 0
	// a failed dial otherwise holds its attempt open for the full 15s request
	// timeout; a local server answers in microseconds
	strategySettings.RequestTimeout = 500 * time.Millisecond
	strategy := NewClientStrategy(ctx, strategySettings)
	routeManager := NewRouteManager(ctx, "test-flap")
	auth := &ClientAuth{
		ByJwt:         "jwt",
		InstanceId:    NewId(),
		AppVersion:    "1.0.0",
		ProvideIntent: true,
	}
	return NewPlatformTransportWithTargetMode(ctx, strategy, routeManager, "ws"+strings.TrimPrefix(serverUrl, "http"), auth, TransportModeH1, settings)
}

// The incident shape: the backend accepts the websocket and the session dies
// seconds later. Before the guard each death reconnected as fast as the
// ReconnectTimeout jitter allowed, forever.
func TestRunH1_FlappingSessionBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(0, &accepts, &stamps)
	defer server.Close()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.FlapStableAfter = 10 * time.Second

	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	time.Sleep(2 * time.Second)

	// waits of ~20, 40, 80, 160, 320, 640, 1280 ms: at most 8 connects in 2s.
	// an unguarded loop reconnects every few ms and lands in the hundreds.
	if n := accepts.Load(); n > 12 {
		t.Fatalf("flapping sessions were not backed off: %d connects in 2s", n)
	}
	if n := accepts.Load(); n < 3 {
		t.Fatalf("guard over-throttled, only %d connects in 2s", n)
	}
}

// A healthy session ends rarely and long after it began. The guard must not add
// any wait there: this is the path every hot restart and platform deploy takes.
func TestRunH1_StableSessionReconnectsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(300*time.Millisecond, &accepts, &stamps)
	defer server.Close()

	logger := &lockedCaptureLogger{}
	settings := DefaultPlatformTransportSettings()
	settings.Log = logger
	settings.ReconnectTimeout = 20 * time.Millisecond
	// every session below lives 300ms, which is stable by this threshold
	settings.FlapStableAfter = 100 * time.Millisecond

	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	deadline := time.After(10 * time.Second)
	for len(stamps.snapshot()) < 4 {
		select {
		case <-deadline:
			t.Fatalf("only %d connects, stable sessions are being delayed", len(stamps.snapshot()))
		case <-time.After(10 * time.Millisecond):
		}
	}
	at := stamps.snapshot()
	for i := 1; i < 4; i++ {
		// 300ms hold plus the reconnect fast path, which draws an independent
		// jitter in [0, 250ms) (NextReconnectTime), plus scheduling slack
		gap := at[i].Sub(at[i-1])
		if gap > 300*time.Millisecond+reconnectFastPathMaxDelay+100*time.Millisecond {
			t.Fatalf("gap %d between connects was %s, stable sessions must reconnect promptly", i, gap)
		}
	}
	// the jitter masks a guard delay of tens of milliseconds, so the guard itself
	// is checked directly: a stable session must never log a held reconnect
	for _, line := range logger.snapshot() {
		if strings.Contains(line, "holding reconnect") {
			t.Fatalf("the flap guard held a stable session: %s", line)
		}
	}
}

// lockedCaptureLogger records Infof lines for a test whose transport logs from
// several goroutines.
type lockedCaptureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *lockedCaptureLogger) Info(_ ...any) {}
func (l *lockedCaptureLogger) Infof(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *lockedCaptureLogger) Warningf(format string, args ...any) {}
func (l *lockedCaptureLogger) Errorf(format string, args ...any)   {}
func (l *lockedCaptureLogger) V(_ int32) Verbose                   { return nopVerbose{} }
func (l *lockedCaptureLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// The loop feeds the process-wide governor: every attempt is classified, and a
// connect whose session dies young is recorded as an early death.
func TestRunH1_FeedsTheGovernor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(0, &accepts, &stamps)
	defer server.Close()

	resetGovernorWindow(t)
	before := connectGov.snapshot()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.FlapStableAfter = 10 * time.Second

	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	time.Sleep(1500 * time.Millisecond)

	after := connectGov.snapshot()
	connects := accepts.Load()
	if connects < 3 {
		t.Fatalf("only %d connects, test setup is not exercising the loop", connects)
	}
	if got := after.OK - before.OK; got < connects-1 {
		t.Fatalf("governor saw %d connected attempts for %d server accepts", got, connects)
	}
	if got := after.Early - before.Early; got < connects-2 {
		t.Fatalf("governor saw %d early deaths for %d young sessions", got, connects)
	}
}

// An attempt that fails is classified, not silently dropped.
func TestRunH1_FailedAttemptsReachTheGovernor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(0, &accepts, &stamps)
	url := server.URL
	server.Close() // nothing listens any more: every dial is refused

	resetGovernorWindow(t)
	before := connectGov.snapshot()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond

	transport := newFlapTestTransport(t, ctx, url, settings)
	defer transport.Close()

	deadline := time.After(10 * time.Second)
	for {
		after := connectGov.snapshot()
		if 2 <= after.Attempts-before.Attempts {
			if after.OK != before.OK {
				t.Fatal("refused dials must not count as connected")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("governor saw %d attempts from a refusing server", after.Attempts-before.Attempts)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// A session cut short by our own teardown (a reload, the reaper, shutdown) says
// nothing about the backend and must not count as one that died young.
func TestRunH1_LocalTeardownIsNotAnEarlyDeath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(time.Hour, &accepts, &stamps) // holds the session open
	defer server.Close()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.FlapStableAfter = time.Hour // any session counts as young

	resetGovernorWindow(t)
	before := connectGov.snapshot()
	transport := newFlapTestTransport(t, ctx, server.URL, settings)

	deadline := time.After(10 * time.Second)
	for accepts.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the transport never connected")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(100 * time.Millisecond)
	transport.Close() // local teardown of a live session
	time.Sleep(500 * time.Millisecond)

	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("a locally cancelled session was counted as dying young (%d)", got)
	}
}

// A session the platform closes for the client limit is the platform enforcing a
// rule, not the backend dropping connections; it must not count as dying young.
func TestRunH1_ClientLimitCloseIsNotAnEarlyDeath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepts.Add(1)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		time.Sleep(100 * time.Millisecond)
		_ = ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(ClientLimitCloseCode, "client limit"), time.Now().Add(time.Second))
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.FlapStableAfter = time.Hour // every session is young

	resetGovernorWindow(t)
	before := connectGov.snapshot()
	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	deadline := time.After(10 * time.Second)
	for accepts.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the transport never connected")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(700 * time.Millisecond)
	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("a client-limit close was counted as dying young (%d)", got)
	}
}

// The group stands the standby down the instant a pin connects, so a young
// session can end because the owner disabled the transport. That is our own
// teardown, not evidence about the platform, and must not feed the governor.
func TestRunH1_DisabledTransportEndIsNotAnEarlyDeath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepts atomic.Int64
	var stamps flapStamps
	server := flapServer(time.Hour, &accepts, &stamps) // holds the session open
	defer server.Close()

	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.FlapStableAfter = time.Hour // any session counts as young

	resetGovernorWindow(t)
	before := connectGov.snapshot()
	transport := newFlapTestTransport(t, ctx, server.URL, settings)

	deadline := time.After(10 * time.Second)
	for accepts.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the transport never connected")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(100 * time.Millisecond)
	transport.SetEnabled(false) // the owner stands it down mid-session
	time.Sleep(500 * time.Millisecond)
	transport.Close()

	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("a disabled transport's young session end was counted as dying young (%d)", got)
	}
}

// The end-of-session rules are shared by the H1 and H3 loops through one helper,
// so that the two cannot drift apart.
func TestSessionEnd_YoungSessionIsHeldAndCountedOnce(t *testing.T) {
	flaps := newFlapGuard(time.Minute, time.Second)
	flaps.jitter = func(d time.Duration) time.Duration { return d }
	ctx := context.Background()
	resetGovernorWindow(t)
	before := connectGov.snapshot()
	wait := sessionEndWait(ctx, flaps, 3*time.Second, false, "c", loggerOrDefault(nil), true, "url")
	if wait != time.Second {
		t.Fatalf("first young session wait = %v, want the base", wait)
	}
	if got := connectGov.snapshot().Early - before.Early; got != 1 {
		t.Fatalf("early deaths counted = %d, want 1", got)
	}
}

func TestSessionEnd_StableSessionHasNoWaitAndNoEarlyDeath(t *testing.T) {
	flaps := newFlapGuard(time.Minute, time.Second)
	resetGovernorWindow(t)
	before := connectGov.snapshot()
	if wait := sessionEndWait(context.Background(), flaps, 5*time.Minute, false, "c", loggerOrDefault(nil), true, "url"); wait != 0 {
		t.Fatalf("stable session wait = %v, want 0", wait)
	}
	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("a stable session counted as an early death (%d)", got)
	}
}

func TestSessionEnd_LocalTeardownAndAuxiliaryAreNotEarlyDeaths(t *testing.T) {
	flaps := newFlapGuard(time.Minute, time.Second)
	flaps.jitter = func(d time.Duration) time.Duration { return d }
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	resetGovernorWindow(t)
	before := connectGov.snapshot()
	sessionEndWait(cancelled, flaps, time.Second, false, "c", loggerOrDefault(nil), true, "url")
	// an auxiliary H3 beside H1 is never a health signal, but is still held back
	if wait := sessionEndWait(context.Background(), flaps, time.Second, false, "c", loggerOrDefault(nil), false, "url"); wait == 0 {
		t.Fatal("an auxiliary transport still must not redial in a tight loop")
	}
	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("teardown or an auxiliary transport counted as an early death (%d)", got)
	}
}

func TestSessionEnd_ClientLimitCloseIsNotAnEarlyDeath(t *testing.T) {
	flaps := newFlapGuard(time.Minute, time.Second)
	flaps.jitter = func(d time.Duration) time.Duration { return d }
	resetGovernorWindow(t)
	before := connectGov.snapshot()
	sessionEndWait(context.Background(), flaps, time.Second, true, "c", loggerOrDefault(nil), true, "url")
	if got := connectGov.snapshot().Early - before.Early; got != 0 {
		t.Fatalf("a client-limit close counted as an early death (%d)", got)
	}
}
