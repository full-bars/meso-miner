package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The native identity receives messages larger than 4 KiB from the platform
// (encrypted peer sessions carry TLS records of up to 16 KiB). A read limit at
// the framer minimum ended its session with "websocket: read limit exceeded"
// seconds after connecting, over and over. A large message
// must be delivered and must not end the session.
func TestRunH1_LargePlatformMessageDoesNotEndTheSession(t *testing.T) {
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
		go func() {
			for {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
			}
		}()
		for i := 0; i < 5; i++ {
			if ws.WriteMessage(websocket.BinaryMessage, make([]byte, 16<<10)) != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	settings := DefaultPlatformTransportSettings()
	settings.EnableH1Plus = false
	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	deadline := time.Now().Add(10 * time.Second)
	for H1MaxMessageSeen() < 16<<10 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := H1MaxMessageSeen(); got < 16<<10 {
		t.Fatalf("the largest message read was %d bytes, want the 16 KiB platform message", got)
	}
	time.Sleep(500 * time.Millisecond)
	if n := accepts.Load(); n != 1 {
		t.Fatalf("the session ended and re-dialed after a 16 KiB message: %d accepts", n)
	}
}
