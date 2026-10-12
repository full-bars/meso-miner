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

func newFastFailStrategy(ctx context.Context, requestTimeout time.Duration) *ClientStrategy {
	settings := DefaultClientStrategySettings()
	settings.RequestTimeout = requestTimeout
	settings.ReconnectTimeout = 20 * time.Millisecond
	settings.MinNextConnectDelay = 0
	settings.MaxNextConnectDelay = 0
	return NewClientStrategy(ctx, settings)
}

// A dial that fails must say why. Before, every failure collapsed into a bare
// "Timeout." after the full request timeout, whatever actually went wrong.
func TestWsDialContext_ReportsTheRealCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "database is on fire", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strategy := newFastFailStrategy(ctx, 600*time.Millisecond)

	_, _, err := strategy.WsDialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), http.Header{})
	if err == nil {
		t.Fatal("expected a failed dial")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Timeout.") {
		t.Fatalf("the Timeout. prefix is matched by the auth rate limiter and the cause classifier, got %q", msg)
	}
	if !strings.Contains(msg, "bad handshake") {
		t.Fatalf("the real cause is missing from %q", msg)
	}
	if !strings.Contains(msg, "dial(s) failed") {
		t.Fatalf("how many dials failed is missing from %q", msg)
	}
}

// When no dialer fails at all and the clock simply runs out, say that: it is a
// different problem from a refusal.
func TestWsDialContext_SaysWhenNothingAnswered(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strategy := newFastFailStrategy(ctx, 300*time.Millisecond)

	_, _, err := strategy.WsDialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), http.Header{})
	if err == nil {
		t.Fatal("expected a failed dial")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Timeout.") || !strings.Contains(msg, "no dialer connected") {
		t.Fatalf("an unanswered dial must say so, got %q", msg)
	}
	if strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "context canceled") {
		t.Fatalf("errors caused by the timeout itself are noise and must not be reported as the cause: %q", msg)
	}
}

// Local teardown is not a backend timeout: the caller and the governor tell
// them apart by errors.Is.
func TestWsDialContext_ParentCancelIsCanceledNotTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	strategy := newFastFailStrategy(ctx, 5*time.Second)
	cancel()

	_, _, err := strategy.WsDialContext(ctx, "ws://127.0.0.1:1", http.Header{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled dial must satisfy errors.Is(context.Canceled), got %v", err)
	}
}

func TestClassifyConnectError_UsesTheRealCause(t *testing.T) {
	silent := &attemptStats{}
	wrap := func(last error) error {
		return &dialTimeoutError{failures: 3, rounds: 2, waited: 15 * time.Second, last: last}
	}
	cases := []struct {
		name string
		err  error
		want connectClass
	}{
		{"upgrade rejected hides under a timeout", wrap(errors.New("websocket: bad handshake")), connectUpgradeFail},
		{"tls failure hides under a timeout", wrap(errors.New("tls: handshake failure")), connectTlsFail},
		{"socks failure hides under a timeout", wrap(errors.New("socks connect tcp 1.2.3.4:1081->api:443: connection refused")), connectSocksFail},
		{"a nothing-answered timeout stays a timeout", &dialTimeoutError{waited: 15 * time.Second}, connectTimeoutNoReply},
	}
	for _, c := range cases {
		if got := classifyConnectError(c.err, silent); got != c.want {
			t.Errorf("%s: classified %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDialTimeoutError_KeepsTheLegacySubstringsAndUnwraps(t *testing.T) {
	cause := errors.New("proxy unreachable: 1.2.3.4:1081")
	err := error(&dialTimeoutError{failures: 1, rounds: 1, waited: time.Second, last: cause})
	if !strings.Contains(err.Error(), "Timeout") {
		t.Fatal("string matchers look for Timeout")
	}
	if !strings.Contains(err.Error(), "proxy unreachable") {
		t.Fatal("classifyAuthFailureCause looks for the proxy cause text")
	}
	if !errors.Is(err, cause) {
		t.Fatal("the cause must be reachable with errors.Is")
	}
}
