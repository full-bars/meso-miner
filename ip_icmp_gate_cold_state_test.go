package connect

import (
	"os"
	"testing"
)

// The gate's atomic state starts at its zero value in a fresh process, and
// that state must read URNETWORK_ICMP. This test deliberately does not
// pre-seed any sentinel: it stores the process-default zero value, sets the
// env kill switch, and requires the parse to reject icmp.
func TestIcmpEnvKillSwitchFromColdState(t *testing.T) {
	resetIcmpGate(t)
	// the zero value is the boot state (icmpEnvUnread); restoring it leaves
	// the other tests free to re-read the env
	icmpEnvState.Store(0)
	defer icmpEnvState.Store(0)

	t.Setenv("URNETWORK_ICMP", "off")
	request := testIcmp4EchoPacket("10.0.0.9", "203.0.113.7", icmp4TypeEchoRequest, 0x22, 1, []byte("x"))
	defer MessagePoolReturn(request)
	if _, _, err := ParseIpPathWithPayload(request); err == nil {
		t.Fatal("URNETWORK_ICMP=off must reject icmp from the boot state")
	}

	// and the other value of the kill switch must accept
	icmpEnvState.Store(0)
	t.Setenv("URNETWORK_ICMP", "on")
	request2 := testIcmp4EchoPacket("10.0.0.9", "203.0.113.7", icmp4TypeEchoRequest, 0x22, 1, []byte("x"))
	defer MessagePoolReturn(request2)
	if _, _, err := ParseIpPathWithPayload(request2); err != nil {
		t.Fatalf("URNETWORK_ICMP=on must accept icmp: %v", err)
	}

	// leave no env behind for the rest of the process
	os.Unsetenv("URNETWORK_ICMP")
}
