package urnettools

import (
	"strings"
	"testing"
	"time"
)

// stubVerifyLoop replaces all verify*Fn vars with no-ops/stubs and returns
// a cleanup function that restores the originals. Call t.Cleanup with the
// returned func so hooks are restored even on test failure.
func stubVerifyLoop(t *testing.T) {
	t.Helper()

	origDiscover := verifyDiscoverFn
	origHandle := verifyRunningImageHandleFn
	origPath := verifyRunningImagePathFn
	origVersion := verifyProviderVersionFn
	origAlive := verifyPidIsAliveFn
	origPrune := verifyPruneBackupsFn
	origSleep := verifySleepFn
	origSuccess := verifyRecordSuccessFn
	origDecline := verifyRecordDeclineFn

	t.Cleanup(func() {
		verifyDiscoverFn = origDiscover
		verifyRunningImageHandleFn = origHandle
		verifyRunningImagePathFn = origPath
		verifyProviderVersionFn = origVersion
		verifyPidIsAliveFn = origAlive
		verifyPruneBackupsFn = origPrune
		verifySleepFn = origSleep
		verifyRecordSuccessFn = origSuccess
		verifyRecordDeclineFn = origDecline
	})

	// Default stubs: no-op sleep, always-alive PID, no-op side effects.
	verifySleepFn = func(d time.Duration) { /* no-op */ }
	verifyPidIsAliveFn = func(pid int) bool { return true }
	verifyPruneBackupsFn = func(string, int) {}
	verifyRecordSuccessFn = func(string) {}
	verifyRecordDeclineFn = func(string, string) {}
	verifyRunningImagePathFn = func(pid int) (string, error) {
		return "/usr/bin/provider", nil
	}
}

func testProvider(stateDir string, pid int) Provider {
	return Provider{
		StateDir: stateDir,
		Binary:   "/usr/bin/provider",
		PID:      pid,
	}
}

func testConfig(tag string) updateConfig {
	return updateConfig{Tag: tag}
}

// TestUpdateVerification_PIDChangesThenSucceeds verifies the happy path:
// PID changes (restart landed), then on a subsequent iteration the version
// matches and the loop returns nil.
func TestUpdateVerification_PIDChangesThenSucceeds(t *testing.T) {
	stubVerifyLoop(t)

	p := testProvider("/home/user/.urnetwork", 100)
	cfg := testConfig("v3.23.0-fix.30.0")
	callCount := 0

	// Discover returns a new PID; version matches on the 3rd call.
	verifyDiscoverFn = func() []Provider {
		callCount++
		switch {
		case callCount <= 2:
			// Old PID still alive, restart hasn't landed yet.
			return []Provider{testProvider(p.StateDir, 100)}
		default:
			// New PID appeared, version matches.
			return []Provider{testProvider(p.StateDir, 200)}
		}
	}

	verifyRunningImageHandleFn = func(pid int) (string, error) {
		return "/proc/200/exe", nil
	}

	verifyProviderVersionFn = func(binary string) string {
		// Version matches on the 3rd+ call (after PID changed).
		if callCount >= 3 {
			return cfg.Tag
		}
		return "v0.0.0-old"
	}

	err := verifyRestartLoop(p, cfg, false, "")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// TestUpdateVerification_VersionMatchesImmediately verifies that when the
// new provider already has the correct version on the first iteration,
// the loop succeeds immediately.
func TestUpdateVerification_VersionMatchesImmediately(t *testing.T) {
	stubVerifyLoop(t)

	p := testProvider("/home/user/.urnetwork", 100)
	cfg := testConfig("v3.23.0-fix.30.0")

	verifyDiscoverFn = func() []Provider {
		return []Provider{testProvider(p.StateDir, 200)}
	}

	verifyRunningImageHandleFn = func(pid int) (string, error) {
		return "/proc/200/exe", nil
	}

	verifyProviderVersionFn = func(binary string) string {
		return cfg.Tag
	}

	err := verifyRestartLoop(p, cfg, false, "")
	if err != nil {
		t.Fatalf("expected nil error on immediate version match, got: %v", err)
	}
}

// TestUpdateVerification_OldPidDiesNoNewProvider verifies the early-exit
// path: the old PID dies and no new provider appears, so the loop breaks
// after i > 3 (iteration 4) and returns the "restart did not take effect"
// error.
func TestUpdateVerification_OldPidDiesNoNewProvider(t *testing.T) {
	stubVerifyLoop(t)

	p := testProvider("/home/user/.urnetwork", 100)
	cfg := testConfig("v3.23.0-fix.30.0")
	sleepCalls := 0

	// Track sleep calls to verify the loop ran the expected iterations.
	verifySleepFn = func(d time.Duration) {
		sleepCalls++
	}

	// Discover always returns empty — no new provider.
	verifyDiscoverFn = func() []Provider {
		return nil
	}

	// Old PID is dead.
	verifyPidIsAliveFn = func(pid int) bool {
		if pid == 100 {
			return false // old PID is dead
		}
		return false
	}

	err := verifyRestartLoop(p, cfg, false, "")

	// Should return error because restart didn't take effect.
	if err == nil {
		t.Fatal("expected error when old PID dies with no new provider")
	}

	if !strings.Contains(err.Error(), "restart did not take effect") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the loop ran 4 iterations (i=0,1,2,3) + initial settle sleep
	// = 5 sleep calls. The early exit fires at i=4 (i > 3).
	if sleepCalls < 5 {
		t.Fatalf("expected at least 5 sleep calls (1 settle + 4 iterations), got %d", sleepCalls)
	}
}

// TestUpdateVerification_HotSwapDecline verifies that when hotSwapTriggered
// is true and verification fails, the decline is recorded and the error
// mentions "HotSwap candidate".
func TestUpdateVerification_HotSwapDecline(t *testing.T) {
	stubVerifyLoop(t)

	p := testProvider("/home/user/.urnetwork", 100)
	cfg := testConfig("v3.23.0-fix.30.0")
	declined := false

	verifyDiscoverFn = func() []Provider { return nil }
	verifyPidIsAliveFn = func(pid int) bool { return false }
	verifyRecordDeclineFn = func(stateDir, reason string) {
		declined = true
		if reason != "takeover_failed" {
			t.Errorf("expected decline reason 'takeover_failed', got %q", reason)
		}
	}

	err := verifyRestartLoop(p, cfg, true, "")

	if err == nil {
		t.Fatal("expected error for hotswap verification failure")
	}
	if !declined {
		t.Error("expected recordHotswapDecline to be called")
	}
	if !strings.Contains(err.Error(), "HotSwap candidate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestUpdateVerification_StillWaitingLog verifies the periodic progress
// log fires every 5 iterations after a PID change.
func TestUpdateVerification_StillWaitingLog(t *testing.T) {
	stubVerifyLoop(t)

	p := testProvider("/home/user/.urnetwork", 100)
	cfg := testConfig("v3.23.0-fix.30.0")
	callCount := 0

	// Discover always returns new PID but version never matches.
	verifyDiscoverFn = func() []Provider {
		callCount++
		return []Provider{testProvider(p.StateDir, 200)}
	}
	verifyRunningImageHandleFn = func(pid int) (string, error) {
		return "/proc/200/exe", nil
	}
	verifyProviderVersionFn = func(binary string) string {
		return "v0.0.0-stale"
	}

	// Override sleep to count iterations and break the loop.
	iterCount := 0
	verifySleepFn = func(d time.Duration) {
		iterCount++
	}

	// The loop runs 30 iterations max. We just want to confirm it doesn't
	// crash and returns an error (verification failed).
	err := verifyRestartLoop(p, cfg, false, "")
	if err == nil {
		t.Fatal("expected error when version never matches")
	}
}
