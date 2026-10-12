package main

import (
	"testing"
)

func TestHourlyPulse_SkippedWhileTheBackendIsDegraded(t *testing.T) {
	prevDegraded, prevTrigger := hourlyPulseBackendDegraded, hourlyPulseTrigger
	defer func() { hourlyPulseBackendDegraded, hourlyPulseTrigger = prevDegraded, prevTrigger }()

	cases := []struct {
		name        string
		degraded    bool
		wantTrigger bool
	}{
		{"healthy backend pulses as before", false, true},
		{"degraded backend skips the reset", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fired := 0
			hourlyPulseBackendDegraded = func() bool { return tc.degraded }
			hourlyPulseTrigger = func() { fired += 1 }
			if got := hourlyPulse(); got != tc.wantTrigger {
				t.Fatalf("hourlyPulse() = %v, want %v", got, tc.wantTrigger)
			}
			want := 0
			if tc.wantTrigger {
				want = 1
			}
			if fired != want {
				t.Fatalf("TriggerPulse called %d times, want %d", fired, want)
			}
		})
	}
}
