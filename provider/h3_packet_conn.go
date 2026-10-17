package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/urnetwork/connect"
)

// newH3PacketConnFactory returns the PlatformTransportSettings.H3PacketConnFactory
// for an identity, or nil when the engine's default socket is right.
//
// A proxied identity (proxySettings != nil) always gets a factory: without one
// the engine opens a socket on the host and the identity's QUIC would leave from
// the host's address instead of its proxy's. The factory relays through the
// proxy and, if the proxy cannot, returns an error rather than a host socket, so
// the platform transport falls back to a TCP mode that already goes through the
// proxy.
//
// A direct identity keeps the engine's default socket and its physical-egress
// binding.
func newH3PacketConnFactory(proxySettings *connect.ProxySettings) func(context.Context) (net.PacketConn, error) {
	if proxySettings == nil {
		return nil
	}
	return func(ctx context.Context) (net.PacketConn, error) {
		// the grade only saw this proxy's TCP path; before QUIC goes through
		// its UDP relay make sure the relay works and exits from the address
		// TCP does
		switch result := globalProxyUDPCheck.Check(ctx, proxySettings, time.Now()); result.Status {
		case proxyUDPNoRelay, proxyUDPExitMismatch:
			return nil, fmt.Errorf("h3 not used through proxy %s: udp check %s", proxySettings.Address, result.Status)
		}
		relayed, err := dialSocks5UDP(ctx, proxySettings.Network, proxySettings.Address, proxySettings.Auth)
		if err != nil {
			return nil, fmt.Errorf("h3 through proxy %s: %w", proxySettings.Address, err)
		}
		return relayed, nil
	}
}
