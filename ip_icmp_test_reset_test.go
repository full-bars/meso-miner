package connect

import (
	"os"
	"testing"
)

// resetIcmpGate returns the process-wide ICMP gate to a clean boot state for
// one test and restores it afterwards. The gate holds two globals: the cached
// URNETWORK_ICMP read and the per-family refusal latch. A latch set by an
// earlier test that really tried to open an unprivileged ICMP socket (a CI
// runner refuses them, a workstation allows them) must never decide the
// outcome of a later test, so every test that parses ICMP or drives an ICMP
// buffer starts here.
func resetIcmpGate(t testing.TB) {
	t.Helper()
	previousEnv, hadEnv := os.LookupEnv("URNETWORK_ICMP")
	os.Unsetenv("URNETWORK_ICMP")
	icmpEnvState.Store(icmpEnvUnread)
	icmpEchoFamilyRefused.Store(0)
	t.Cleanup(func() {
		if hadEnv {
			os.Setenv("URNETWORK_ICMP", previousEnv)
		} else {
			os.Unsetenv("URNETWORK_ICMP")
		}
		icmpEnvState.Store(icmpEnvUnread)
		icmpEchoFamilyRefused.Store(0)
	})
}
