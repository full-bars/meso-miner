package urnettools

import "testing"

func TestFreeBSDSetupHasNoExecutablePlan(t *testing.T) {
	cfg := &SystemPlatformConfigurer{PlatformName: "freebsd"}
	plan, err := cfg.PlanRequirement("", "provider")
	if err != nil || len(plan) != 0 {
		t.Fatalf("FreeBSD plan = %v, err = %v", plan, err)
	}
}
