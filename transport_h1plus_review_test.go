package connect

import (
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type closeCountingH1Conn struct {
	closed atomic.Int32
}

func (c *closeCountingH1Conn) Close() error                      { c.closed.Add(1); return nil }
func (c *closeCountingH1Conn) WriteMessage(int, []byte) error    { return nil }
func (c *closeCountingH1Conn) ReadMessage() (int, []byte, error) { return 0, nil, io.EOF }
func (c *closeCountingH1Conn) NextReader() (int, io.Reader, error) {
	return 0, nil, io.EOF
}
func (c *closeCountingH1Conn) SetReadLimit(int64)               {}
func (c *closeCountingH1Conn) SetReadDeadline(time.Time) error  { return nil }
func (c *closeCountingH1Conn) SetWriteDeadline(time.Time) error { return nil }
func (c *closeCountingH1Conn) UnderlyingConn() net.Conn         { return nil }

// A losing parallel H1 dial holds an authenticated platform session; closing its
// result must close the connection (websocket or H1+), not only a wsConn.
func TestEvalResultCloseClosesTheH1Conn(t *testing.T) {
	conn := &closeCountingH1Conn{}
	(&evalResult{h1Conn: conn}).Close()
	if conn.closed.Load() != 1 {
		t.Fatalf("the H1 connection was closed %d times, want 1", conn.closed.Load())
	}
}

// A busy or failing server is platform trouble, not a capability miss: no fresh
// websocket dial on top of it. A real capability miss still falls back.
func TestH1PlusUpgradeFallbackOnlyForCapabilityMisses(t *testing.T) {
	for status, wantFallback := range map[int]bool{
		400: true, 404: true, 426: true, 501: true, 200: true,
		429: false, 500: false, 502: false, 503: false, 504: false,
		401: false, 403: false,
	} {
		err := &HTTPUpgradeError{StatusCode: status, Reason: "rejected", Terminal: status == 401 || status == 403}
		if got := HTTPUpgradeAllowsFallback(err); got != wantFallback {
			t.Errorf("status %d: fallback = %t, want %t", status, got, wantFallback)
		}
	}
}

// The platform refusing the credential on the upgrade is a refusal, which the
// connect governor files apart from a backend failure.
func TestH1DialErrorAuthRejected(t *testing.T) {
	for status, want := range map[int]bool{401: true, 403: true, 404: false, 503: false} {
		err := &h1DialError{dial: errors.New("Timeout."), causes: &HTTPUpgradeError{StatusCode: status, Reason: "rejected"}}
		if got := explicitRejection(err); got != want {
			t.Errorf("status %d: explicit rejection = %t, want %t", status, got, want)
		}
		if got := classifyConnectError(err, nil); (got == connectRefused) != want {
			t.Errorf("status %d: classified %s, refused expected %t", status, got, want)
		}
	}
}

func TestMinimumMessageLenLimitCoversThePeerHandshake(t *testing.T) {
	// per-peer TLS handshake flights are 4.9 KB in the lab and 9,124 bytes in a
	// production capture
	if got := DefaultClientSettings().MinimumMessageLenLimit(); got < 9124 {
		t.Fatalf("MinimumMessageLenLimit = %d, below the 9,124-byte production handshake", got)
	}
	if got := DefaultPlatformTransportSettings().H1MaxMessageByteCount; got < int64(DefaultClientSettings().MinimumMessageLenLimit()) {
		t.Fatalf("the H1 read limit %d is below MinimumMessageLenLimit", got)
	}
}
