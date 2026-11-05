package connect

import (
	"context"
	"net"
	"testing"
)

func TestControlProxyDialDoesNotDemoteOrRetry(t *testing.T) {
	restore := swapControlFamilyProbe(func(int) bool { return true })
	defer restore()
	controlFamilyClear()
	defer controlFamilyClear()
	settings := DefaultConnectSettings()
	settings.ProxySettings = &ProxySettings{Index: 0, Network: "tcp", Address: "127.0.0.1:1080"}
	attempts := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		attempts++
		return &stubConn{remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 443}}, nil
	}
	handshake := func(context.Context, net.Conn) (net.Conn, error) { return nil, &timeoutError{} }
	if _, err := dialControlTlsWithFamilyFallback(context.Background(), settings, "tcp", "api.example:443", dial, handshake); err == nil {
		t.Fatal("expected handshake failure")
	}
	if attempts != 1 {
		t.Fatalf("dialed %d times through proxy", attempts)
	}
	if controlFamilyDemotedFamily() != 0 {
		t.Fatal("proxied handshake demoted a family")
	}
}

func TestControlProxyDialFailureDoesNotRedial(t *testing.T) {
	restore := swapControlFamilyProbe(func(int) bool { return true })
	defer restore()
	controlFamilyClear()
	defer controlFamilyClear()
	if !recordControlFamilyStrike(6, &timeoutError{}) {
		t.Fatal("could not seed demotion")
	}
	settings := DefaultConnectSettings()
	settings.ProxySettings = &ProxySettings{Index: 0, Network: "tcp", Address: "127.0.0.1:1080"}
	attempts := 0
	dial := func(context.Context, string, string) (net.Conn, error) { attempts++; return nil, &timeoutError{} }
	handshake := func(_ context.Context, conn net.Conn) (net.Conn, error) { return conn, nil }
	if _, err := dialControlTlsWithFamilyFallback(context.Background(), settings, "tcp", "api.example:443", dial, handshake); err == nil {
		t.Fatal("expected proxy failure")
	}
	if attempts != 1 {
		t.Fatalf("dialed %d times after proxy failure", attempts)
	}
	if controlFamilyDemotedFamily() != 6 {
		t.Fatal("proxy failure cleared unrelated demotion")
	}
}
