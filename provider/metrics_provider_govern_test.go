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
		// the transfer retry budget and the speed-test echo
		"urnet_transfer_send_frames_total", "urnet_transfer_resend_deferred_total",
		"urnet_transfer_resend_would_defer_total", "urnet_transfer_budget_tokens",
		"urnet_transfer_budget_earned_bytes_total", "urnet_speedtest_total",
		"urnet_speedtest_echo_bytes_total",
		// dead-client release
		"urnet_dead_client_released_total", "urnet_dead_client_would_release_total",
		"urnet_dead_client_reason_total", "urnet_dead_client_silent",
		"urnet_dead_client_fleet_collapsed", "urnet_dead_client_fleet",
		"urnet_dead_client_recovered_silence_seconds",
		"urnet_dead_client_return_after_release_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics are missing %s", want)
		}
	}
}
