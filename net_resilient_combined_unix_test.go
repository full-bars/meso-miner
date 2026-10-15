//go:build unix

package connect

// The socket-option checks: the combined file is untagged so the pure
// userspace tests run on windows too; these need syscall and stay unix-only.

import (
	"bytes"
	"net"
	"syscall"
	"testing"
	"time"
)

func tcpNoDelay(t *testing.T, conn *net.TCPConn) bool {
	t.Helper()
	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatalf("syscall conn: %v", err)
	}
	var value int
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		value, sockErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	}); err != nil {
		t.Fatalf("control: %v", err)
	}
	if sockErr != nil {
		t.Fatalf("getsockopt TCP_NODELAY: %v", sockErr)
	}
	return value != 0
}

// TestResilientCombinedPlainFragmentSetsNoDelay checks the plain-fragment TCP
// branch: with segment on, NODELAY is set so Nagle cannot coalesce the two
// halves; with segment off the socket option is left alone (so the test also
// proves the option change comes from segment, not from Go's default).
func TestResilientCombinedPlainFragmentSetsNoDelay(t *testing.T) {
	for _, segment := range []bool{true, false} {
		record := buildClientHelloRecord(t)
		client, server := newTcpPair(t)
		if err := client.SetNoDelay(false); err != nil {
			t.Fatalf("clear nodelay: %v", err)
		}

		rconn := newResilientTlsConn(client, true, false, segment)
		if _, err := rconn.Write(record); err != nil {
			t.Fatalf("segment=%v write: %v", segment, err)
		}
		if got := tcpNoDelay(t, client); got != segment {
			t.Fatalf("segment=%v: TCP_NODELAY = %v, want %v", segment, got, segment)
		}

		server.SetReadDeadline(time.Now().Add(5 * time.Second))
		got := readTlsRecords(t, server, len(record)-5)
		if !bytes.Equal(got, record[5:]) {
			t.Fatalf("segment=%v: peer received different payload than the hello", segment)
		}
	}
}
