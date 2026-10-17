package connect

import "net"

func udpTestPath(version int) *IpPath {
	if version == 4 {
		return &IpPath{
			Version:         4,
			Protocol:        IpProtocolUdp,
			SourceIp:        net.ParseIP("10.11.12.13"),
			SourcePort:      54321,
			DestinationIp:   net.ParseIP("93.184.216.34"),
			DestinationPort: 443,
		}
	}
	return &IpPath{
		Version:         6,
		Protocol:        IpProtocolUdp,
		SourceIp:        net.ParseIP("fd00::1"),
		SourcePort:      54321,
		DestinationIp:   net.ParseIP("2606:2800:220:1::1"),
		DestinationPort: 443,
	}
}
