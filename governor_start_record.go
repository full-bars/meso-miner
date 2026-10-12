package connect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Crash-loop awareness.
//
// The startup grace (govStartupGrace) lets a start stampede run unthrottled, and
// the outage marker is only written once the budget has cut. A provider that
// crashes, or is killed, in less than the grace never cuts and never writes the
// marker, so every restart of a crash loop runs a full unthrottled stampede into
// whatever the backend is doing. Each start therefore leaves a timestamp beside
// the other govern state, and a start that finds govCrashLoopStarts starts
// (itself included) within govCrashLoopWindow is treated as a restart into an
// outage: no startup grace, and the budget begins at the floor and ramps on
// evidence, exactly as after a fresh outage marker.
//
// The record is a courtesy, never a dependency: an unreadable or unwritable file
// is treated as no history, garbage lines are dropped, and old starts age out.

const (
	// govStartsFileName lives beside govern_outage and govern_baseline
	govStartsFileName = "govern_starts"
	// govCrashLoopWindow and govCrashLoopStarts: this many starts within the
	// window is a crash loop
	govCrashLoopWindow = 10 * time.Minute
	govCrashLoopStarts = 3
	// govStartsKeep bounds the file however often the process starts
	govStartsKeep = 16
	// govStartsMaxRead bounds what is read, so a corrupted file costs nothing
	govStartsMaxRead = 4 << 10
)

// recordStart appends now to the start record at path and returns how many
// starts, this one included, fall within govCrashLoopWindow. File errors are
// ignored: with no readable history the answer is 1.
func recordStart(path string, now time.Time) int {
	starts := readStarts(path, now)
	starts = append(starts, now.Unix())
	if govStartsKeep < len(starts) {
		starts = starts[len(starts)-govStartsKeep:]
	}
	writeStarts(path, starts)
	return len(starts)
}

// readStarts returns the recorded starts within govCrashLoopWindow before now,
// oldest first. Lines that do not parse, and starts in the future (a clock that
// went backwards), are dropped.
func readStarts(path string, now time.Time) []int64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, govStartsMaxRead))
	if err != nil {
		return nil
	}
	var starts []int64
	for _, line := range strings.Split(string(raw), "\n") {
		sec, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(sec, 0)
		if at.After(now) || govCrashLoopWindow <= now.Sub(at) {
			continue
		}
		starts = append(starts, sec)
	}
	return starts
}

func writeStarts(path string, starts []int64) {
	var b strings.Builder
	for _, sec := range starts {
		b.WriteString(strconv.FormatInt(sec, 10))
		b.WriteByte('\n')
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// skipStartupGrace ends the startup grace now: a crash loop's start is not a
// one-off stampede and its failures are evidence.
func (self *connectGovernor) skipStartupGrace() {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.started = self.now().Add(-govStartupGrace)
}

// startCrashLoopThrottled puts the budget at the floor as a found outage marker
// does, and counts it as a fresh cut so the no-evidence reopen only creeps.
func (self *reconnectBudget) startCrashLoopThrottled(starts int) {
	self.mu.Lock()
	now := self.now()
	self.rate = budgetFloorRate
	self.lastOutage.Store(now.UnixNano())
	if self.throttledSince.IsZero() {
		self.throttledSince = now
	}
	self.noteCutLocked()
	emit := self.emit
	self.mu.Unlock()
	if emit != nil {
		emit(fmt.Sprintf("[govern] crash loop: %d starts in the last %s, skipping the startup grace and starting at %s, ramping on evidence",
			starts, govCrashLoopWindow, rateString(budgetFloorRate)))
	}
}

// noteStart records this start beside the other govern state and, on a crash
// loop, starts governed. dir is the govern state directory.
func noteStart(gov *connectGovernor, budget *reconnectBudget, dir string, now time.Time) {
	starts := recordStart(filepath.Join(dir, govStartsFileName), now)
	if starts < govCrashLoopStarts {
		return
	}
	gov.skipStartupGrace()
	budget.startCrashLoopThrottled(starts)
}

// NoteGovernStart records this process start beside the outage marker at
// markerPath and, when it is the third start within ten minutes, skips the
// startup grace and starts the budget throttled. Call once at startup, after
// InitGovernState, and not for a hot-swap successor: a planned handoff is not a
// crash. It does nothing unless the governor is enforcing.
func NoteGovernStart(markerPath string) {
	if currentGovernMode() != governEnforce {
		return
	}
	noteStart(connectGov, connectBudget, filepath.Dir(markerPath), time.Now())
}
