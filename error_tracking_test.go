package connect

import (
	"strings"
	"testing"
	"time"
)

func resetErrorTracker() {
	globalErrorTracker.mu.Lock()
	globalErrorTracker.recent = map[ErrorCategory][]string{}
	globalErrorTracker.mu.Unlock()
	globalErrorTracker.throttleMu.Lock()
	globalErrorTracker.throttles = map[ErrorCategory]*logThrottle{}
	globalErrorTracker.throttleMu.Unlock()
}

func TestRecordErrorAndMetrics(t *testing.T) {
	resetErrorTracker()
	// Distinct categories each get their own 1/min limiter, so each first
	// error lands in the buffer.
	RecordError(ErrorTransport, "dial failed")
	RecordError(ErrorIP, "no route")

	m := ErrorMetrics()
	if len(m) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(m))
	}
	tm, ok := m[string(ErrorTransport)].(map[string]any)
	if !ok {
		t.Fatalf("transport metrics not a map: %T", m[string(ErrorTransport)])
	}
	if tm["count"].(int) != 1 {
		t.Fatalf("expected 1 transport error, got %v", tm["count"])
	}
	recent := tm["recent"].([]string)
	if !strings.Contains(recent[0], "dial failed") {
		t.Fatalf("recent error missing message: %q", recent[0])
	}
	if !strings.Contains(recent[0], ".go:") {
		t.Fatalf("recent error missing stack frame: %q", recent[0])
	}
}

func TestErrorTrackerRateLimit(t *testing.T) {
	resetErrorTracker()
	// The throttle allows one per minute. A burst must be suppressed after
	// the first in the same window.
	RecordError(ErrorWebRTC, "ice failed")
	RecordError(ErrorWebRTC, "ice failed 2")
	m := ErrorMetrics()
	wm := m[string(ErrorWebRTC)].(map[string]any)
	if wm["count"].(int) != 1 {
		t.Fatalf("expected rate limiting to keep 1, got %v", wm["count"])
	}
}

func TestRecordErrorTrim(t *testing.T) {
	resetErrorTracker()
	// Overfill past maxRecentErrors to exercise the trim path (and confirm it
	// does not panic, as the original cap()/len() bug did).
	for i := 0; i < maxRecentErrors+20; i++ {
		// different message each time to avoid the 1/min throttle
		globalErrorTracker.mu.Lock()
		globalErrorTracker.throttleMu.Lock()
		globalErrorTracker.throttles[ErrorProxy] = newLogThrottle(0)
		globalErrorTracker.throttleMu.Unlock()
		globalErrorTracker.mu.Unlock()
		RecordError(ErrorProxy, "proxy error")
	}
	m := ErrorMetrics()
	pm := m[string(ErrorProxy)].(map[string]any)
	if pm["count"].(int) != maxRecentErrors {
		t.Fatalf("expected trim to %d, got %v", maxRecentErrors, pm["count"])
	}
}

// TestErrorCounterBurstThrottled verifies that the Prometheus cumulative
// counter increments for EVERY error, even when the rate limiter suppresses
// the log/buffer. The counter is a count of events, not log lines.
func TestErrorCounterBurstThrottled(t *testing.T) {
	resetErrorTracker()

	// Reset the Prometheus counter so we isolate this test's increments.
	globalProm.initErrors()
	globalProm.errorsTotal[ErrorTransport].Store(0)

	burst := 10
	for i := 0; i < burst; i++ {
		RecordError(ErrorTransport, "burst error")
	}
	after := globalProm.errorsTotal[ErrorTransport].Load()
	if after != int64(burst) {
		t.Fatalf("expected %d error counter increments, got %d", burst, after)
	}

	// The buffer should contain exactly 1 (the first, allowed by the throttle).
	m := ErrorMetrics()
	tm := m[string(ErrorTransport)].(map[string]any)
	if tm["count"].(int) != 1 {
		t.Fatalf("expected 1 buffered error (rate-limited), got %v", tm["count"])
	}
}

// TestErrorCounterAllCategoriesBurst verifies the counter increments
// independently per category even when each is rate-limited.
func TestErrorCounterAllCategoriesBurst(t *testing.T) {
	resetErrorTracker()
	globalProm.initErrors()
	for _, cat := range []ErrorCategory{ErrorTransport, ErrorIP, ErrorProxy, ErrorWebRTC} {
		globalProm.errorsTotal[cat].Store(0)
	}

	for _, cat := range []ErrorCategory{ErrorTransport, ErrorIP, ErrorProxy, ErrorWebRTC} {
		for i := 0; i < 5; i++ {
			RecordError(cat, "burst")
		}
		if got := globalProm.errorsTotal[cat].Load(); got != 5 {
			t.Errorf("category %s: expected 5 counter increments, got %d", cat, got)
		}
	}
}

var _ = time.Now
