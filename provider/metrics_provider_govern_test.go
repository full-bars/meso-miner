package main

import (
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

func TestGovernHealthLine_CarriesTheOperatorFacts(t *testing.T) {
	line := governHealthLine(connect.GovernorStatus{
		Mode: "enforce", Verdict: "degraded", BudgetRate: 8, Waiting: 3100,
		Attempts: 138, Connected: 45, DiedYoung: 42, WouldHold: 18, FlapBackoffs: 77,
	})
	for _, want := range []string{
		"[health][govern]", "mode=enforce", "verdict=degraded", "rate=8.0/s",
		"waiting=3100", "attempts=138", "connected=45", "died_young=42", "flap_backoffs=77",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

func TestProviderMetrics_ExposeTheGovernor(t *testing.T) {
	body := providerExtraMetrics()
	for _, want := range []string{
		"urnet_govern_degraded", "urnet_govern_budget_rate", "urnet_govern_waiting",
		"urnet_govern_attempts", "urnet_govern_died_young", "urnet_govern_flap_backoffs_total",
		"urnet_govern_mode",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics are missing %s", want)
		}
	}
}
