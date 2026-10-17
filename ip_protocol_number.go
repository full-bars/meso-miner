// ip_protocol_number.go — the iana protocol-number type, as upstream declares it.

package connect

// iana ip protocol numbers, as carried in the ipv4 protocol and ipv6 next
// header fields. tcp, udp, and icmp echo are handled; other protocols are
// dropped. the icmp numbers also label the flow-teardown unreachable
// messages built by `ipOosUnreachable`, which are injected toward the
// source.
//
// This is upstream's own declaration. The fork's earlier duplicate (IPProtocol
// with IP_PROTOCOL_* constants) is gone: one type carries the wire value, and
// IpProtocol (the int enum on IpPath.Protocol) is the separate flow enum, as
// upstream has it.
type ipProtocolNumber uint8

const (
	ipProtocolNumberIcmp4 ipProtocolNumber = 1
	ipProtocolNumberTcp   ipProtocolNumber = 6
	ipProtocolNumberUdp   ipProtocolNumber = 17
	ipProtocolNumberIcmp6 ipProtocolNumber = 58
)
