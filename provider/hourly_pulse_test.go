package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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

// A pulse skipped because the backend is degraded fires as soon as it recovers,
// not an hour later.
func TestHourlyPulseLoop_RetriesASkippedPulseUntilTheBackendRecovers(t *testing.T) {
	prevDegraded, prevTrigger := hourlyPulseBackendDegraded, hourlyPulseTrigger
	defer func() { hourlyPulseBackendDegraded, hourlyPulseTrigger = prevDegraded, prevTrigger }()
	var degraded atomic.Bool
	degraded.Store(true)
	var fired atomic.Int32
	hourlyPulseBackendDegraded = degraded.Load
	hourlyPulseTrigger = func() { fired.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hourlyPulseLoop(ctx, 10*time.Millisecond, 5*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatal("pulsed while the backend was degraded")
	}
	degraded.Store(false)
	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("a skipped pulse was not retried after the backend recovered")
	}
}
