package connect

// icmp echo acceptance gate. icmp echo is ON by default (the v33 decision),
// with an operator kill switch and a platform self-disable:
//
//   - `URNETWORK_ICMP=off` restores the historical drop (read once and
//     cached; a restart applies a change).
//   - a platform whose kernel refuses unprivileged icmp sockets
//     (`net.ipv4.ping_group_range` excluding the service gid) self-disables
//     the first time a flow cannot open its egress socket, with ONE logged
//     line. After that icmp simply reverts to unsupported - exactly as if
//     the feature were never enabled - and no other traffic is affected.
//
// The gate is consulted by the ParseIpPathWithPayload icmp tail, so a
// disabled icmp is rejected at the parse, never reaches the NAT, and cannot
// open an egress socket at all.

import (
	"os"
	"strings"
	"sync/atomic"
)

// icmpEchoFamilyRefused holds, per address family, whether the platform
// refused unprivileged icmp sockets for that family: bit 0 is ipv4, bit 1 is
// ipv6. A refusal in one family never disables the other (see ICMP.md).
var icmpEchoFamilyRefused atomic.Int32

func icmpFamilyBit(ipVersion int) int32 {
	if ipVersion == 4 {
		return 1
	}
	return 2
}

// icmpEnvState caches the URNETWORK_ICMP read. The zero value must mean
// "unread" so that a fresh process reads the env on the first gate consult:
// the value zero is what a booted process actually has.
const (
	icmpEnvUnread = 0
	icmpEnvOn     = 1
	icmpEnvOff    = 2
)

var icmpEnvState atomic.Int32

func icmpEnvDisabled() bool {
	state := icmpEnvState.Load()
	if state == icmpEnvUnread {
		state = icmpEnvOn
		if strings.ToLower(strings.TrimSpace(os.Getenv("URNETWORK_ICMP"))) == "off" {
			state = icmpEnvOff
		}
		icmpEnvState.Store(state)
	}
	return state == icmpEnvOff
}

// icmpEchoParsingEnabled reports whether an icmp echo packet of this address
// family is accepted by the parse surfaces. nil cost when enabled (the
// default).
func icmpEchoParsingEnabled(ipVersion int) bool {
	if icmpEchoFamilyRefused.Load()&icmpFamilyBit(ipVersion) != 0 {
		return false
	}
	return !icmpEnvDisabled()
}

// icmpEgressRefused latches the refusal for the AFFECTED FAMILY ONLY, and
// only for errors that mean the platform refuses unprivileged icmp sockets
// (EACCES/EPERM/EPROTONOSUPPORT, see icmpEgressRefusalError). Any other open
// error is a per-flow condition: the one flow drops and nothing latches. The
// single logged line names the real error and the family.
func icmpEgressRefused(log Logger, ipVersion int, err error) {
	if !icmpEgressRefusalError(err) {
		return
	}
	bit := icmpFamilyBit(ipVersion)
	if previous := icmpEchoFamilyRefused.Or(bit); previous&bit == 0 {
		log.Errorf("[icmp] unprivileged icmp sockets are not available for ip version %d (on linux this is net.ipv4.ping_group_range against the service gid); icmp echo is disabled for that address family: %s\n", ipVersion, err)
	}
}
