//go:build linux

package connect

import (
	"net"
	"testing"
)

// the linux-native counterpart of ip_dial_errno_windows_test.go: instead of
// asserting the classifier against constructed winsock codes, this drives a
// real denied connect on the loopback and checks the real kernel errno maps
// to the refused answer. the port is bound and then closed, so nothing can
// be listening when the dial runs.

func nativeRefusedDialError(t *testing.T, network string, address string) error {
	t.Helper()
	connection, err := net.Dial(network, address)
	if err == nil {
		connection.Close()
		t.Fatal("dialing a just-closed loopback port succeeded")
	}
	return err
}

func TestClassifyDialFailureLinuxNativeRefused(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no ipv4 loopback listener: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	path := dialFailureTestPath(IpProtocolTcp, 4)
	action, packet := classifyDialFailure(path, nativeRefusedDialError(t, "tcp4", address), 0)
	if action != dialFailureRst || packet != nil {
		t.Fatalf("a native refused dial classified as %v (packet %v), want the rst", action, packet != nil)
	}
}

func TestClassifyDialFailureLinuxNativeRefusedV6(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no ipv6 loopback listener: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	path := dialFailureTestPath(IpProtocolTcp, 6)
	action, packet := classifyDialFailure(path, nativeRefusedDialError(t, "tcp6", address), 0)
	if action != dialFailureRst || packet != nil {
		t.Fatalf("a native refused dial classified as %v (packet %v), want the rst", action, packet != nil)
	}
}
