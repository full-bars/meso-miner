package connect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRecordStart_CountsRecentStartsAndAgesThemOut(t *testing.T) {
	now := time.Unix(3_000_000, 0)
	line := func(at time.Time) string { return strconv.FormatInt(at.Unix(), 10) + "\n" }
	cases := []struct {
		name     string
		existing string
		want     int
	}{
		{"no record", "", 1},
		{"two recent starts", line(now.Add(-time.Minute)) + line(now.Add(-2*time.Minute)), 3},
		{"old starts age out", line(now.Add(-govCrashLoopWindow)) + line(now.Add(-time.Hour)) + line(now.Add(-time.Minute)), 2},
		{"garbage lines are dropped", "not a number\n\n" + line(now.Add(-time.Minute)) + "12x\n", 2},
		{"starts in the future are dropped", line(now.Add(time.Hour)) + line(now.Add(-time.Minute)), 2},
		{"the record is bounded", strings.Repeat(line(now.Add(-time.Minute)), 3*govStartsKeep), govStartsKeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), govStartsFileName)
			if tc.existing != "" {
				if err := os.WriteFile(path, []byte(tc.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := recordStart(path, now); got != tc.want {
				t.Fatalf("recordStart = %d, want %d", got, tc.want)
			}
			// the record now holds exactly what was counted
			if got := len(readStarts(path, now)); got != tc.want {
				t.Fatalf("record holds %d starts, want %d", got, tc.want)
			}
		})
	}
}

// The record is a courtesy: a path that cannot be read or written never fails
// the start and never reads as a crash loop.
func TestRecordStart_UnusableRecordIsNoHistory(t *testing.T) {
	now := time.Unix(3_000_000, 0)

	// the path is a directory: unreadable as a file and not replaceable
	dirPath := filepath.Join(t.TempDir(), govStartsFileName)
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if got := recordStart(dirPath, now); got != 1 {
			t.Fatalf("an unreadable record must count only this start, got %d", got)
		}
	}

	// a read-only directory
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o700)
	roPath := filepath.Join(ro, govStartsFileName)
	recordStart(roPath, now)
	recordStart(roPath, now)
	if _, err := os.Stat(roPath); err == nil {
		t.Skip("running with privileges that ignore directory permissions")
	}
	if got := recordStart(roPath, now); got != 1 {
		t.Fatalf("an unwritable record must count only this start, got %d", got)
	}
}

func TestNoteStart_ThirdStartInTenMinutesIsGoverned(t *testing.T) {
	cases := []struct {
		name         string
		starts       []time.Duration // offsets of earlier starts before now
		wantGoverned bool
	}{
		{"first start", nil, false},
		{"second start", []time.Duration{-time.Minute}, false},
		{"third start in ten minutes", []time.Duration{-4 * time.Minute, -2 * time.Minute}, true},
		{"three starts spread over an hour", []time.Duration{-50 * time.Minute, -20 * time.Minute}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			h := newBudgetHarness()
			var lines []string
			h.budget.emitAt = nil
			h.budget.emit = func(line string) { lines = append(lines, line) }
			// a fresh process: inside its startup grace
			h.gov.started = *h.now
			path := filepath.Join(dir, govStartsFileName)
			for _, off := range tc.starts {
				recordStart(path, h.now.Add(off))
			}

			noteStart(h.gov, h.budget, dir, *h.now)

			grace := h.gov.snapshot().StartupGrace
			if tc.wantGoverned {
				if grace {
					t.Fatal("a crash loop must skip the startup grace")
				}
				if got := h.budget.currentRate(); got != budgetFloorRate {
					t.Fatalf("a crash loop starts at the floor, got %v", got)
				}
				if !h.budget.outageRecent(time.Minute) {
					t.Fatal("a crash loop start reads as an outage in progress")
				}
				if len(lines) != 1 || !strings.HasPrefix(lines[0], "[govern] crash loop: 3 starts") {
					t.Fatalf("one crash-loop line expected, got %q", lines)
				}
				// no evidence right after: the probe creeps, it does not double
				h.advance(budgetControlInterval)
				h.budget.control()
				if got := h.budget.currentRate(); got != budgetFloorRate {
					t.Fatalf("a crash-loop start must not double open on no evidence, got %v", got)
				}
				// evidence that the backend works opens it at the usual pace
				h.advance(budgetControlInterval)
				h.attempts(connectOK, 20)
				h.budget.control()
				if got := h.budget.currentRate(); got != budgetFloorRate*budgetIncreaseFactor {
					t.Fatalf("connects that land must ramp a crash-loop start, got %v", got)
				}
				return
			}
			if !grace {
				t.Fatal("an ordinary start keeps its startup grace")
			}
			if h.budget.currentRate() != budgetCeilingRate || h.budget.throttled() {
				t.Fatal("an ordinary start begins open")
			}
			if len(lines) != 0 {
				t.Fatalf("an ordinary start logs nothing, got %q", lines)
			}
		})
	}
}

// Planned restarts, however close together, are not a crash loop; a crash
// between them (no clean-exit marker) still counts.
func TestNoteStart_RoutineRestartsAreNotACrashLoop(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "govern_outage")
	h := newBudgetHarness()
	var lines []string
	h.budget.emitAt = nil
	h.budget.emit = func(line string) { lines = append(lines, line) }
	h.gov.started = *h.now

	for i := 0; i < 6; i++ { // six updates or toggles in six minutes
		NoteGovernCleanExit(marker)
		noteStart(h.gov, h.budget, dir, h.now.Add(time.Duration(i)*time.Minute))
	}
	if len(lines) != 0 || h.budget.currentRate() == budgetFloorRate {
		t.Fatalf("routine restarts were treated as a crash loop: %q", lines)
	}
	// now it crashes twice without a clean exit: two more starts after the last
	// routine one make three in ten minutes
	noteStart(h.gov, h.budget, dir, h.now.Add(7*time.Minute))
	noteStart(h.gov, h.budget, dir, h.now.Add(8*time.Minute))
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "[govern] crash loop: 3 starts") {
		t.Fatalf("crashes after routine restarts must still count: %q", lines)
	}
	// the marker is consumed by the start that reads it
	NoteGovernCleanExit(marker)
	noteStart(h.gov, h.budget, dir, h.now.Add(9*time.Minute))
	if consumeCleanExit(dir) {
		t.Fatal("the clean-exit marker must be consumed by the next start")
	}
}
