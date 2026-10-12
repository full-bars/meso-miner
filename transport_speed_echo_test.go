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

// The platform floods a fresh connection with a speed test and the provider echoes
// every byte back. It must echo ALL of it: a test that never comes back is retried
// by the server (a provider that dropped echoes got ten times the tests and ten
// times the inbound bytes), and the score feeds provider ranking.
func TestRunH1_SpeedTestIsEchoedInFullAndCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const frame = 64 << 10
	const flood = 4 << 20
	var echoedBack atomic.Int64
	done := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		go func() {
			for {
				_, msg, err := ws.ReadMessage()
				if err != nil {
					return
				}
				if frame <= len(msg) {
					echoedBack.Add(int64(len(msg)))
				}
			}
		}()
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte{TransportControlSpeedStart, 0, 0, 0, 0})
		buf := make([]byte, frame)
		for sent := 0; sent < flood; sent += frame {
			if ws.WriteMessage(websocket.BinaryMessage, buf) != nil {
				break
			}
		}
		time.Sleep(1500 * time.Millisecond)
		close(done)
	}))
	defer server.Close()

	before := SpeedEchoStatsNow()
	transport := newFlapTestTransport(t, ctx, server.URL, DefaultPlatformTransportSettings())
	defer transport.Close()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the flood never finished")
	}
	if got := echoedBack.Load(); got != flood {
		t.Fatalf("echoed %d of %d bytes", got, flood)
	}
	after := SpeedEchoStatsNow()
	if after.Tests-before.Tests != 1 {
		t.Fatalf("tests counted = %d, want 1", after.Tests-before.Tests)
	}
	if after.EchoedBytes-before.EchoedBytes != flood {
		t.Fatalf("echoed bytes counted = %d, want %d", after.EchoedBytes-before.EchoedBytes, flood)
	}
}
