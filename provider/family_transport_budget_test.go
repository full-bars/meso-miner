package main

import (
	"testing"

	"github.com/urnetwork/connect"
)

// The group's transport budget is sized for the number of transports the
// launch stack enables: three for dual (both pins and the standby), one for
// any fixed stack. A node running ipv4-only used to carry the three-transport
// admission anyway.
func TestFamilyGroupBudgetScalesWithTheLaunchStack(t *testing.T) {
	target := connect.MemoryBudget()
	if target <= 0 {
		target = familyBudgetReferenceByteCount
	}
	// compare against the same constructors, so internal scaling rounding
	// cannot drift the expectation
	one := connect.NewPlatformTransportBudgetForMemoryTarget(target).Stats().TotalByteCount
	threeTransports := connect.NewPlatformTransportBudgetForMemoryTarget(3 * target).Stats().TotalByteCount

	for name, c := range map[string]struct {
		mode connect.FamilyStackMode
		want connect.ByteCount
	}{
		"dual":   {connect.FamilyStackDual, threeTransports},
		"ipv4":   {connect.FamilyStackIpv4, one},
		"ipv6":   {connect.FamilyStackIpv6, one},
		"legacy": {connect.FamilyStackLegacy, one},
	} {
		if got := familyGroupBudgetForStack(c.mode).Stats().TotalByteCount; got != c.want {
			t.Fatalf("%s: budget total %d, want %d", name, got, c.want)
		}
	}
}
