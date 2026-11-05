package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTransportH1CanceledDialDoesNotFeedGovernor(t *testing.T) {
	resetGovernorWindow(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		once.Do(func() { close(started) })
		<-release
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := connectGov.snapshot().Attempts
	settings := DefaultPlatformTransportSettings()
	settings.V2H1Auth = false // the legacy auth echo is part of the dial
	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("H1 dial never reached the auth echo")
	}
	// The auth read returns a socket error, not context.Canceled. Cancel first,
	// then close the server side so the dial completes after local teardown.
	cancel()
	close(release)
	select {
	case <-transport.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("transport did not exit after cancellation")
	}
	if got := connectGov.snapshot().Attempts - before; got != 0 {
		t.Fatalf("locally canceled H1 dial recorded %d governor attempts", got)
	}
}
