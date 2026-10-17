package connect

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// dialFailureAnswer runs a tcp sequence whose upstream dial fails with dialErr
// and returns the packets it handed to the receive callback in place of a
// syn-ack.
func dialFailureAnswer(t *testing.T, dialErr error) [][]byte {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan []byte, 4)
	receive := func(_ TransferPath, _ protocol.ProvideMode, _ *IpPath, packet []byte) {
		received <- append([]byte(nil), packet...)
	}

	settings := DefaultTcpBufferSettings()
	settings.ConnectTimeout = 60 * time.Second
	settings.DialContextSettings = &DialContextSettings{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, dialErr
		},
	}
	tcp := NewTcpSequence(
		ctx, receive, TransferPath{}, protocol.ProvideMode_Network, 4,
		net.ParseIP("10.0.0.9").To4(), TCPPort(42001),
		net.ParseIP("203.0.113.7").To4(), TCPPort(8080),
		settings,
	)
	done := make(chan struct{})
	go func() { tcp.Run(); close(done) }()
	tcp.sendItems <- &TcpSendItem{tcp: &parsedTcp{syn: true}, ipPacket: MessagePoolGet(2048)}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sequence did not finish after the failed dial")
	}
	close(received)
	var out [][]byte
	for p := range received {
		out = append(out, p)
	}
	return out
}

func TestTcpDialFailureAnswersTheSource(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	packets := dialFailureAnswer(t, refused)
	if len(packets) != 1 {
		t.Fatalf("a refused dial produced %d packets, want one RST", len(packets))
	}
	if packets[0][9] != byte(ipProtocolNumberTcp) {
		t.Fatalf("a refused dial was answered with protocol %d, want tcp", packets[0][9])
	}
	if flags := packets[0][20+13]; flags&0x04 == 0 {
		t.Fatalf("a refused dial was answered without RST (flags %#x)", flags)
	}

	unreachable := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}
	packets = dialFailureAnswer(t, unreachable)
	if len(packets) != 1 || packets[0][9] != byte(ipProtocolNumberIcmp4) {
		t.Fatalf("an unreachable dial produced %d packets, want one icmp unreachable", len(packets))
	}

	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
	if packets = dialFailureAnswer(t, timeout); len(packets) != 0 {
		t.Fatalf("a timed out dial produced %d packets, want none", len(packets))
	}
	if packets = dialFailureAnswer(t, errors.New("stub dial failure")); len(packets) != 1 {
		t.Fatalf("an immediate unclassified failure produced %d packets, want the capacity-class unreachable", len(packets))
	}
}
