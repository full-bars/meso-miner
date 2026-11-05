package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTransportH1CanceledDialDoesNotFeedGovernor(t *testing.T) {
	resetGovernorWindow(t)
	started := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := connectGov.snapshot().Attempts
	settings := DefaultPlatformTransportSettings()
	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		transport.Close()
		t.Fatal("H1 dial never reached server")
	}
	cancel()
	select {
	case <-transport.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("transport did not exit after cancellation")
	}
	if got := connectGov.snapshot().Attempts - before; got != 0 {
		t.Fatalf("locally canceled H1 dial recorded %d governor attempts", got)
	}
}
