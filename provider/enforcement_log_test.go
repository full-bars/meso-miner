package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

func ev(kind, mode string) connect.GovernEvent {
	return connect.GovernEvent{Kind: kind, Mode: mode, Line: "[govern] " + kind + " " + mode}
}

func testEnforcementLog(t *testing.T, dir string, now *time.Time) *enforcementLog {
	t.Helper()
	return newEnforcementLog(dir, func() time.Time { return *now })
}

func countOf(l *enforcementLog, window, kind, mode string) int {
	for _, c := range l.windowCounts() {
		if c.Window == window && c.Kind == kind && c.Mode == mode {
			return c.Count
		}
	}
	return 0
}

func totalOf(l *enforcementLog, kind, mode string) int {
	for _, c := range l.totals() {
		if c.Kind == kind && c.Mode == mode {
			return c.Count
		}
	}
	return 0
}

func TestEnforcementRecordWritesJsonl(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.record(ev("dead_client_release", "enforce"))
	l.record(ev("dead_client_release", "shadow"))

	b, err := os.ReadFile(filepath.Join(l.dir, enforcementLogName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), lines)
	}
	var first, second enforcementRecord
	if json.Unmarshal([]byte(lines[0]), &first) != nil || json.Unmarshal([]byte(lines[1]), &second) != nil {
		t.Fatalf("not json: %q", lines)
	}
	if first.Kind != "dead_client_release" || first.Mode != "enforce" || second.Mode != "shadow" {
		t.Errorf("records = %+v %+v", first, second)
	}
	if first.Time != "2026-10-12T20:00:00Z" {
		t.Errorf("time = %q", first.Time)
	}
}

func TestEnforcementSkipsObservations(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.record(ev(connect.GovernKindObservation, "enforce"))
	if _, err := os.Stat(filepath.Join(l.dir, enforcementLogName)); err == nil {
		t.Error("an observation was written to the enforcement log")
	}
	if len(l.windowCounts()) != 0 || len(l.totals()) != 0 {
		t.Errorf("an observation was counted: %v %v", l.windowCounts(), l.totals())
	}
}

func TestEnforcementRateLimitCountsButDoesNotWrite(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	total := enforcementMaxLinesPerMinute + 15
	for i := 0; i < total; i++ {
		l.record(ev("dead_client_release", "enforce"))
	}
	b, _ := os.ReadFile(filepath.Join(l.dir, enforcementLogName))
	if got := strings.Count(string(b), "\n"); got != enforcementMaxLinesPerMinute {
		t.Errorf("wrote %d lines, want %d", got, enforcementMaxLinesPerMinute)
	}
	if l.suppressedCount() != 15 {
		t.Errorf("suppressed = %d, want 15", l.suppressedCount())
	}
	if got := countOf(l, "today", "dead_client_release", "enforce"); got != total {
		t.Errorf("today = %d, want every event counted: %d", got, total)
	}
	now = now.Add(time.Minute)
	l.record(ev("dead_client_release", "enforce"))
	b, _ = os.ReadFile(filepath.Join(l.dir, enforcementLogName))
	if got := strings.Count(string(b), "\n"); got != enforcementMaxLinesPerMinute+1 {
		t.Errorf("after the minute turned wrote %d lines", got)
	}
}

func TestEnforcementCountsSurviveRestartAndWindows(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, dir, &now)
	l.record(ev("dead_client_release", "enforce")) // today
	now = now.AddDate(0, 0, -3)
	l.record(ev("budget_cut", "enforce")) // 3 days before
	now = now.AddDate(0, 0, -20)
	l.record(ev("budget_cut", "enforce")) // 23 days before
	now = now.AddDate(0, 0, -50)
	l.record(ev("budget_cut", "enforce")) // 73 days before: outside 30d
	now = time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l.flush()

	l2 := testEnforcementLog(t, dir, &now) // a new process
	for _, c := range []struct {
		window, kind string
		want         int
	}{
		{"today", "dead_client_release", 1},
		{"7d", "budget_cut", 1},
		{"30d", "budget_cut", 2},
		{"today", "budget_cut", 0},
	} {
		if got := countOf(l2, c.window, c.kind, "enforce"); got != c.want {
			t.Errorf("%s/%s = %d, want %d", c.window, c.kind, got, c.want)
		}
	}
	if got := totalOf(l2, "budget_cut", "enforce"); got != 3 {
		t.Errorf("lifetime budget_cut = %d, want all 3 (the 73-day-old one is only in the lifetime total)", got)
	}
}

// A HotSwap parent and candidate both run the log. Their counts must add, not
// overwrite each other.
func TestEnforcementTwoProcessesMergeTheirCounts(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	parent := testEnforcementLog(t, dir, &now)
	candidate := testEnforcementLog(t, dir, &now)

	for i := 0; i < 3; i++ {
		parent.record(ev("budget_cut", "enforce"))
	}
	for i := 0; i < 4; i++ {
		candidate.record(ev("budget_cut", "enforce"))
	}
	parent.flush()
	candidate.flush()
	parent.flush() // a second flush with nothing pending changes nothing

	fresh := testEnforcementLog(t, dir, &now)
	if got := totalOf(fresh, "budget_cut", "enforce"); got != 7 {
		t.Errorf("lifetime = %d, want 3+4", got)
	}
	if got := countOf(fresh, "today", "budget_cut", "enforce"); got != 7 {
		t.Errorf("today = %d, want 7", got)
	}
	left, _ := filepath.Glob(filepath.Join(dir, enforcementDailyName+".*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestEnforcementPendingCountsAreVisibleBeforeFlush(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.record(ev("crash_loop", "enforce"))
	if got := totalOf(l, "crash_loop", "enforce"); got != 1 {
		t.Errorf("total = %d, want the unflushed event", got)
	}
	l.flush()
	if got := totalOf(l, "crash_loop", "enforce"); got != 1 {
		t.Errorf("total after flush = %d, want it counted once, not twice", got)
	}
}

func TestEnforcementCorruptDailyFileIsKeptNotWiped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, enforcementDailyName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, dir, &now)
	l.record(ev("budget_cut", "enforce"))
	l.flush()
	b, err := os.ReadFile(filepath.Join(dir, enforcementDailyName+".corrupt"))
	if err != nil || string(b) != "{not json" {
		t.Errorf("the unreadable file was not preserved: %q %v", b, err)
	}
}

func TestEnforcementDailyPrunesOldDaysButKeepsLifetime(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	now = now.AddDate(0, 0, -(enforcementKeepDays + 5))
	l.record(ev("budget_cut", "enforce"))
	now = time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l.record(ev("budget_cut", "enforce"))
	l.flush()
	b, _ := os.ReadFile(filepath.Join(l.dir, enforcementDailyName))
	var saved enforcementDaily
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Days) != 1 {
		t.Errorf("kept %d days, want only the recent one: %v", len(saved.Days), saved.Days)
	}
	if saved.Total["budget_cut|enforce"] != 2 {
		t.Errorf("lifetime = %v, want both events", saved.Total)
	}
}

func TestEnforcementLogRotatesAtCap(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.maxBytes = 200
	for i := 0; i < 20; i++ {
		now = now.Add(time.Minute)
		l.record(connect.GovernEvent{Kind: "budget_cut", Mode: "enforce", Line: "[govern] budget cut with a longer tail to fill the file quickly"})
	}
	info, err := os.Stat(filepath.Join(l.dir, enforcementLogName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.dir, enforcementLogName+".1")); err != nil {
		t.Errorf("no rotated file: %v", err)
	}
	if info.Size() > l.maxBytes+400 {
		t.Errorf("active file is %d bytes, cap %d", info.Size(), l.maxBytes)
	}
}

func TestEnforcementMetricsFormat(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.record(ev("dead_client_release", "enforce"))
	old := globalEnforcementLog
	globalEnforcementLog = l
	defer func() { globalEnforcementLog = old }()

	var b strings.Builder
	writeEnforcementMetrics(&b)
	out := b.String()
	for _, want := range []string{
		`urnet_enforcement_events_total{kind="dead_client_release",mode="enforce"} 1`,
		`urnet_enforcement_events{kind="dead_client_release",mode="enforce",window="today"} 1`,
		`urnet_enforcement_events{kind="dead_client_release",mode="enforce",window="30d"} 1`,
		"urnet_enforcement_log_suppressed_total 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q in:\n%s", want, out)
		}
	}
}

func TestEnforcementLogNeverPanicsOnUnwritableDir(t *testing.T) {
	now := time.Now()
	l := newEnforcementLog("/proc/definitely/not/writable", func() time.Time { return now })
	l.record(ev("budget_cut", "shadow")) // must not panic or block
	l.flush()
}

// A stalled disk must not stall the goroutine that emits the event: the sink only
// does a non-blocking send, and a full queue drops and counts.
func TestEnforcementSinkNeverBlocksWhenTheWorkerIsStuck(t *testing.T) {
	now := time.Now()
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.queue = make(chan connect.GovernEvent, 2) // no worker: nothing drains it

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			l.enqueue(ev("budget_cut", "enforce"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked while the queue was full")
	}
	if got := l.dropped.Load(); got != 48 {
		t.Fatalf("dropped = %d, want 48 (50 sent, 2 queued)", got)
	}
}

func TestEnforcementAsyncWorkerRecordsQueuedEvents(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.startAsync()
	l.enqueue(ev("restart_marker", "enforce"))
	l.enqueue(ev("crash_loop", "enforce"))
	deadline := time.Now().Add(2 * time.Second)
	for totalOf(l, "restart_marker", "enforce")+totalOf(l, "crash_loop", "enforce") < 2 {
		if time.Now().After(deadline) {
			t.Fatal("worker never recorded the queued events")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if l.dropped.Load() != 0 {
		t.Fatalf("dropped = %d", l.dropped.Load())
	}
}

// Without the worker (tests, a log built by hand) the sink falls back to a direct
// write instead of losing the event.
func TestEnforcementEnqueueWithoutWorkerWritesDirectly(t *testing.T) {
	now := time.Now()
	l := testEnforcementLog(t, t.TempDir(), &now)
	l.enqueue(ev("budget_cut", "enforce"))
	if totalOf(l, "budget_cut", "enforce") != 1 {
		t.Fatal("event lost")
	}
}

func TestEnforcementWriteFailuresAreCounted(t *testing.T) {
	now := time.Now()
	l := newEnforcementLog("/proc/definitely/not/writable", func() time.Time { return now })
	l.record(ev("budget_cut", "enforce"))
	l.flush()
	if l.writeErrors.Load() < 2 {
		t.Fatalf("writeErrors = %d, want the failed append and the failed flush counted", l.writeErrors.Load())
	}
	old := globalEnforcementLog
	globalEnforcementLog = l
	defer func() { globalEnforcementLog = old }()
	var b strings.Builder
	writeEnforcementMetrics(&b)
	for _, want := range []string{"urnet_enforcement_log_write_errors_total ", "urnet_enforcement_log_dropped_total 0"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if strings.Contains(b.String(), "urnet_enforcement_log_write_errors_total 0\n") {
		t.Error("write errors exported as 0 after failures")
	}
}

// restart_marker and crash_loop are emitted inside InitGovernState and
// NoteGovernStart, so the sink has to exist before they run.
func TestEnforcementLogIsInstalledBeforeTheGovernorsEmit(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	init := strings.Index(s, "initEnforcementLog()")
	state := strings.Index(s, "connect.InitGovernState(")
	start := strings.Index(s, "connect.NoteGovernStart(")
	if init < 0 || state < 0 || start < 0 {
		t.Fatalf("call sites not found: %d %d %d", init, state, start)
	}
	if init > state || init > start {
		t.Fatal("initEnforcementLog must be called before InitGovernState and NoteGovernStart")
	}
	if strings.Count(s, "initEnforcementLog()") != 1 {
		t.Fatal("initEnforcementLog must be called exactly once")
	}
}

// A rotation that cannot rename the old file is a swallowed file error and must be
// counted, or the write-error metric understates what is lost.
func TestEnforcementRotationFailureIsCounted(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := testEnforcementLog(t, dir, &now)
	l.maxBytes = 1 // rotate on every append after the first
	// a non-empty directory where the rotated file would go makes the rename fail
	rotated := filepath.Join(dir, enforcementLogName+".1")
	if err := os.MkdirAll(filepath.Join(rotated, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	l.record(ev("budget_cut", "enforce"))
	l.record(ev("budget_cut", "shadow"))
	if l.writeErrors.Load() == 0 {
		t.Fatal("a failed rotation was not counted")
	}
}
