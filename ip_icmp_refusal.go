package connect

import (
	"errors"
	"net"
)

// icmpEgressUnsupported is the fail-soft sentinel for platforms with no echo
// backend at all (js/wasm). It classifies as a refusal so such a platform
// does not retry a fresh flow per packet.
var icmpEgressUnsupported = errors.New("No icmp egress support on this platform.")

// icmpEgressRefusalError reports whether an egress open failure means the
// platform itself refuses unprivileged icmp for that family (the only errors
// that may latch the family off), or whether it is a per-flow condition
// (EAFNOSUPPORT on udp6, EMFILE and friends) that must drop just the one
// flow. See ICMP.md.
func icmpEgressRefusalError(err error) bool {
	return errors.Is(err, icmpEgressUnsupported) || icmpEgressRefusalErrno(err)
}

// icmpEgressUnusableError reports write or read errors that mean the socket
// cannot be used again, so the flow must close. Every other send error (a
// message error like EMSGSIZE, a timeout, a transient route error) drops
// that one packet and keeps the flow.
func icmpEgressUnusableError(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return icmpEgressUnusableErrno(err)
}
