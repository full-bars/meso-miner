package connect

import (
	"net"
	"testing"
)

// The client send path must forward every packet the SMTP guard does not
// govern. A guard verdict of "not applicable" once counted as a refusal, which
// dropped all non-SMTP client traffic and was invisible under -short because
// the end to end echo tests skip there.
func TestSmtpVerdictPermitsSend(t *testing.T) {
	for verdict, want := range map[smtpEgressVerdict]bool{
		smtpEgressNotApplicable: true,
		smtpEgressAllow:         true,
		smtpEgressReject:        false,
		smtpEgressRejectLatched: false,
	} {
		if got := verdict.permitsSend(); got != want {
			t.Fatalf("verdict %d permitsSend = %v, want %v", verdict, got, want)
		}
	}
}

func TestSmtpGuardDoesNotGovernOrdinaryTraffic(t *testing.T) {
	var guard smtpEgressGuard
	for _, ipPath := range []*IpPath{
		{Version: 4, Protocol: IpProtocolUdp, SourceIp: net.IPv4(72, 0, 0, 1), SourcePort: 40000, DestinationIp: net.IPv4(72, 1, 1, 1), DestinationPort: 443},
		{Version: 4, Protocol: IpProtocolTcp, SourceIp: net.IPv4(72, 0, 0, 1), SourcePort: 40000, DestinationIp: net.IPv4(72, 1, 1, 1), DestinationPort: 443},
	} {
		if verdict := guard.inspect(ipPath, nil); !verdict.permitsSend() {
			t.Fatalf("ordinary %v traffic to port 443 got verdict %d and would be dropped by the client send path", ipPath.Protocol, verdict)
		}
	}
}
