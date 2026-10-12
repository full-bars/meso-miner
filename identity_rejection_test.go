package connect

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func dialStatus(t *testing.T, status int) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", status)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strategy := newFastFailStrategy(ctx, 400*time.Millisecond)
	_, _, err := strategy.WsDialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), http.Header{})
	if err == nil {
		t.Fatal("expected a failed dial")
	}
	return err
}

// Only the platform saying no, to this token, is evidence that an identity has
// been revoked. Overload, timeouts and gateway errors say nothing about it.
func TestExplicitRejection_OnlyUnauthorizedAndForbidden(t *testing.T) {
	cases := []struct {
		status   int
		rejected bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusServiceUnavailable, false},
		{http.StatusBadGateway, false},
		{http.StatusGatewayTimeout, false},
		{http.StatusTooManyRequests, false},
		{499, false}, // nginx "client closed request": a load balancer storm, not a rejection
		{http.StatusInternalServerError, false},
		{http.StatusNotFound, false},
	}
	for _, c := range cases {
		err := dialStatus(t, c.status)
		if got := explicitRejection(err); got != c.rejected {
			t.Errorf("http %d: explicitRejection = %v, want %v (err: %v)", c.status, got, c.rejected, err)
		}
		if !strings.Contains(err.Error(), "bad handshake") {
			t.Errorf("http %d: the original cause text must survive, got %q", c.status, err)
		}
	}
}

func TestExplicitRejection_PlainErrorsAreNeverRejections(t *testing.T) {
	for _, err := range []error{
		errors.New("Timeout."),
		errors.New("401 Unauthorized"), // text alone is not structural evidence
		errors.New("websocket: bad handshake"),
		context.DeadlineExceeded,
		nil,
	} {
		if explicitRejection(err) {
			t.Errorf("%v must not read as an explicit rejection", err)
		}
	}
}

func TestStatusErrorKeepsTheChain(t *testing.T) {
	inner := errors.New("websocket: bad handshake")
	err := error(&wsStatusError{status: 503, err: inner})
	if !errors.Is(err, inner) {
		t.Fatal("the original error must stay reachable")
	}
	if !strings.Contains(err.Error(), "http 503") {
		t.Fatalf("the status should be visible in %q", err)
	}
}

func TestRecordProxyAuthFailure_CountsRejectionsSeparately(t *testing.T) {
	prev := backendDegradedNow
	backendDegradedNow = func() bool { return false }
	defer func() { backendDegradedNow = prev }()
	ResetProxyHealthForTesting()
	RegisterProxy(11, "10.0.0.11:1080", "10.0.0.11:1080")

	for i := 0; i < 4; i++ {
		RecordProxyAuthFailure(11, &dialTimeoutError{failures: 1, rounds: 1, last: errors.New("x")})
		RecordProxyAuthFailure(11, errors.New("Timeout."))
	}
	if got := ProxyAuthRejectionCount(11); got != 0 {
		t.Fatalf("timeouts are not rejections, got %d", got)
	}

	RecordProxyAuthFailure(11, &dialTimeoutError{failures: 2, rounds: 1, last: &wsStatusError{status: 401, err: errors.New("websocket: bad handshake")}, rejected: true})
	if got := ProxyAuthRejectionCount(11); got != 1 {
		t.Fatalf("an explicit rejection must count, got %d", got)
	}
	if ProxyAuthRejectionCount(404) != 0 {
		t.Fatal("an unknown proxy has no rejections")
	}
}

// A rejection hides behind a later timeout in the same call: the dial error is
// the LAST failure, but one dialer was told no, and that must not be lost.
func TestDialTimeoutError_RemembersARejectionEvenIfALaterErrorWasATimeout(t *testing.T) {
	rec := &evalErrorRecorder{}
	rec.note(&wsStatusError{status: 401, err: errors.New("websocket: bad handshake")})
	rec.note(errors.New("read tcp: i/o timeout"))
	err := rec.timeoutError(time.Second)
	if !explicitRejection(err) {
		t.Fatalf("a 401 earlier in the call must survive a later timeout: %v", err)
	}
}

func TestBackendDegradedFrom_ReadsTheVerdictAndTheOutageSignal(t *testing.T) {
	gov, _ := newTestGovernor()
	h := newBudgetHarness()

	if backendDegradedFrom(gov, h.budget) {
		t.Fatal("a quiet backend is not degraded")
	}
	record(gov, connectTimeoutPlatform, 100)
	if !backendDegradedFrom(gov, h.budget) {
		t.Fatal("a failing verdict is degraded")
	}

	// the verdict clears but the budget was throttled a moment ago
	quiet, _ := newTestGovernor()
	h.attempts(connectTimeoutPlatform, 200)
	h.budget.control()
	if !backendDegradedFrom(quiet, h.budget) {
		t.Fatal("a throttled budget is an outage in progress")
	}
}

// A refusal that arrives while the backend is failing is not trusted.
func TestRecordProxyAuthFailure_IgnoresRefusalsDuringAnOutage(t *testing.T) {
	prev := backendDegradedNow
	degraded := true
	backendDegradedNow = func() bool { return degraded }
	defer func() { backendDegradedNow = prev }()
	ResetProxyHealthForTesting()
	RegisterProxy(12, "10.0.0.12:1080", "10.0.0.12:1080")

	rej := &dialTimeoutError{failures: 1, rounds: 1, last: &wsStatusError{status: 401, err: errors.New("websocket: bad handshake")}, rejected: true}
	for i := 0; i < 10; i++ {
		RecordProxyAuthFailure(12, rej)
	}
	if got := ProxyAuthRejectionCount(12); got != 0 {
		t.Fatalf("refusals during an outage must not count toward revocation, got %d", got)
	}
	degraded = false
	RecordProxyAuthFailure(12, rej)
	if got := ProxyAuthRejectionCount(12); got != 1 {
		t.Fatalf("a refusal from a healthy backend counts, got %d", got)
	}
}

// The renewal fast path renews a JWT when auth failures grow. A real refusal
// must still reach it, and a timeout must not trigger useless renewals mid-outage.
func TestRecordProxyAuthFailure_RefusalsReachTheRenewalFastPathTimeoutsDoNot(t *testing.T) {
	prev := backendDegradedNow
	backendDegradedNow = func() bool { return false }
	defer func() { backendDegradedNow = prev }()
	ResetProxyHealthForTesting()
	RegisterProxy(13, "10.0.0.13:1080", "10.0.0.13:1080")

	RecordProxyAuthFailure(13, &dialTimeoutError{failures: 2, rounds: 1, last: errors.New("i/o timeout")})
	if got := ProxyAuthFailureCount(13); got != 0 {
		t.Fatalf("a timeout is not an auth failure, got %d", got)
	}
	RecordProxyAuthFailure(13, &dialTimeoutError{failures: 1, rounds: 1, last: &wsStatusError{status: 401, err: errors.New("websocket: bad handshake")}, rejected: true})
	if got := ProxyAuthFailureCount(13); got != 1 {
		t.Fatalf("a refusal is an auth failure, got %d", got)
	}
}
