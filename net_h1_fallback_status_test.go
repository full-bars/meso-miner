package connect

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A failed H1+ upgrade followed by the websocket fallback's own refusal is joined
// into one error. The credential check must still see the platform's 401 through
// the join.
func TestExplicitRejectionSeesThroughAJoinedError(t *testing.T) {
	upgradeFailure := &HTTPUpgradeError{Reason: "rejected", StatusCode: 426}
	refused := &wsStatusError{status: http.StatusUnauthorized, err: errors.New("websocket: bad handshake")}
	overloaded := &wsStatusError{status: http.StatusServiceUnavailable, err: errors.New("websocket: bad handshake")}

	if !explicitRejection(errors.Join(upgradeFailure, refused)) {
		t.Fatal("a 401 inside a joined error must read as a rejection")
	}
	if explicitRejection(errors.Join(upgradeFailure, overloaded)) {
		t.Fatal("a 503 is overload, not a rejection")
	}
	if !explicitRejection(refused) {
		t.Fatal("the plain status error is still a rejection")
	}
	if explicitRejection(nil) || explicitRejection(errors.New("401 Unauthorized")) {
		t.Fatal("text is not evidence and nil is not a rejection")
	}
}

// The H1+ dial's websocket fallback keeps the HTTP status of its refusal, so a
// refused token reads as one and an overloaded backend does not (WsDialContext
// has always done this).
func TestH1FallbackKeepsTheWebsocketStatus(t *testing.T) {
	for _, tc := range []struct {
		status   int
		rejected bool
	}{{http.StatusUnauthorized, true}, {http.StatusServiceUnavailable, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				w.WriteHeader(tc.status)
				return
			}
			// the framed upgrade: answer as a server that does not know it
			w.WriteHeader(http.StatusNotImplemented)
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resetFramedUpgradeCache(t)
		_, err := dialH1MessagesWithinDeadline(
			ctx,
			"ws"+strings.TrimPrefix(server.URL, "http")+"/",
			http.Header{},
			&websocket.Dialer{HandshakeTimeout: 5 * time.Second},
			H1FramerProtocol,
			65535,
			true,
			nil,
			"route-test",
		)
		cancel()
		server.Close()
		if err == nil {
			t.Fatalf("status %d: the dial must fail", tc.status)
		}
		if got := explicitRejection(err); got != tc.rejected {
			t.Fatalf("status %d: explicitRejection=%v, want %v (err=%v)", tc.status, got, tc.rejected, err)
		}
	}
}
