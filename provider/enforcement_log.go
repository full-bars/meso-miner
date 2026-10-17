package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect"
)

// The enforcement log is the durable record of what the governors did: the
// ramlogs are RAM and roll over within the hour, and events.log is small and
// shared with the proxy grade lines. Every [govern] event from the connect
// library lands here as one JSON line, and per-day counts by kind survive
// restarts so a box can answer "how much has it enforced in the last weeks"
// without scanning lines.
//
// During a HotSwap the parent and the candidate both run this: appends and
// rotation happen under the same inter-process lock the action ledger uses, and
// the daily counts are merged into the file as deltas at flush time, never
// overwritten from a stale in-memory copy.

const (
	enforcementLogName     = "enforcement.jsonl"
	enforcementDailyName   = "enforcement-daily.json"
	enforcementLogMaxBytes = 5 * 1024 * 1024
	// a flapping governor must not flood the file: per kind and mode, at most
	// this many lines a minute are written, the rest are only counted
	enforcementMaxLinesPerMinute = 20
	enforcementKeepDays          = 120
	enforcementFlushInterval     = 30 * time.Second
)

// enforcementRecord is one line of enforcement.jsonl.
type enforcementRecord struct {
	Time string `json:"ts"`
	Kind string `json:"kind"`
	Mode string `json:"mode"`
	Line string `json:"line"`
}

// enforcementDaily is the persisted counts: per UTC day, and a lifetime total
// that is never pruned so the exported counter only ever grows.
type enforcementDaily struct {
	Total map[string]int            `json:"total"`
	Days  map[string]map[string]int `json:"days"`
}

type enforcementLog struct {
	mu       sync.Mutex
	dir      string
	now      func() time.Time
	maxBytes int64

	// counts not yet merged into the file: UTC date -> "kind|mode" -> count
	pending    map[string]map[string]int
	persisted  enforcementDaily // the file as of the last flush
	lastFlush  time.Time
	minuteAt   time.Time
	minuteSeen map[string]int
	suppressed int

	// queue decouples the callers that emit events (the reconnect budget's
	// dispatcher among them) from this log's file work: the sink only ever does a
	// non-blocking send, and one worker does the flock, append and flush. A full
	// queue drops the event and counts it. Nil until startAsync.
	queue   chan connect.GovernEvent
	dropped atomic.Int64
	// writeErrors counts file operations that failed and were swallowed.
	writeErrors atomic.Int64
}

// enforcementQueueLen is how many events can wait for the worker. Events are rare
// (rate-limited at the source and per minute here), so this only fills when the
// disk has stalled for a long time.
const enforcementQueueLen = 1024

// startAsync starts the worker that drains the queue into record.
func (l *enforcementLog) startAsync() {
	l.queue = make(chan connect.GovernEvent, enforcementQueueLen)
	go func() {
		for e := range l.queue {
			l.record(e)
		}
	}()
}

// enqueue is the event sink. It never blocks: the governors emit from latency
// sensitive goroutines, and a stalled disk must not stall them or make them look
// stalled to their own watchdogs.
func (l *enforcementLog) enqueue(e connect.GovernEvent) {
	// observations are never recorded; they must not take a slot an enforcement
	// event needs
	if e.Kind == connect.GovernKindObservation {
		return
	}
	if l.queue == nil {
		l.record(e)
		return
	}
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}

func newEnforcementLog(dir string, now func() time.Time) *enforcementLog {
	l := &enforcementLog{
		dir:        dir,
		now:        now,
		maxBytes:   enforcementLogMaxBytes,
		pending:    map[string]map[string]int{},
		minuteSeen: map[string]int{},
	}
	l.persisted = l.readDaily()
	return l
}

func (l *enforcementLog) dailyPath() string { return filepath.Join(l.dir, enforcementDailyName) }

func (l *enforcementLog) readDaily() enforcementDaily {
	d := enforcementDaily{Total: map[string]int{}, Days: map[string]map[string]int{}}
	b, err := os.ReadFile(l.dailyPath())
	if err != nil {
		return d
	}
	var saved enforcementDaily
	if json.Unmarshal(b, &saved) != nil {
		// keep the unreadable file for inspection instead of silently replacing it
		os.Rename(l.dailyPath(), l.dailyPath()+".corrupt")
		return d
	}
	if saved.Total != nil {
		d.Total = saved.Total
	}
	if saved.Days != nil {
		d.Days = saved.Days
	}
	return d
}

// record keeps one event. Observations are not enforcement and are not kept.
// It never blocks on anything but a short mutex, a flock and a small append,
// and every failure is swallowed: this log must not be the reason the provider
// stalls.
func (l *enforcementLog) record(e connect.GovernEvent) {
	if e.Kind == connect.GovernKindObservation {
		return
	}
	// the time the event was emitted, not the time a queue behind a stalled disk
	// got to it, so a backlog lands on the right minute and the right day
	now := l.now().UTC()
	if !e.At.IsZero() {
		now = e.At.UTC()
	}
	key := e.Kind + "|" + e.Mode

	l.mu.Lock()
	defer l.mu.Unlock()

	date := now.Format("2006-01-02")
	day := l.pending[date]
	if day == nil {
		day = map[string]int{}
		l.pending[date] = day
	}
	day[key]++

	if minute := now.Truncate(time.Minute); !minute.Equal(l.minuteAt) {
		l.minuteAt = minute
		l.minuteSeen = map[string]int{}
	}
	l.minuteSeen[key]++
	if enforcementMaxLinesPerMinute < l.minuteSeen[key] {
		l.suppressed++
	} else {
		l.appendLocked(enforcementRecord{Time: now.Format(time.RFC3339), Kind: e.Kind, Mode: e.Mode, Line: e.Line})
	}
	if enforcementFlushInterval <= now.Sub(l.lastFlush) {
		l.flushLocked(now)
	}
}

func (l *enforcementLog) appendLocked(r enforcementRecord) {
	b, err := json.Marshal(r)
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	path := filepath.Join(l.dir, enforcementLogName)
	if os.MkdirAll(l.dir, 0700) != nil {
		l.writeErrors.Add(1)
		return
	}
	release, err := acquireJWTStoreLock(path)
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	defer release()
	if info, err := os.Stat(path); err == nil && l.maxBytes < info.Size() {
		if os.Rename(path, path+".1") != nil {
			l.writeErrors.Add(1)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		l.writeErrors.Add(1)
	}
	if err := f.Close(); err != nil {
		l.writeErrors.Add(1)
	}
}

// flushLocked merges the pending counts into the file under the lock: it
// re-reads what the other process may have written, adds this process's deltas,
// and replaces the file through a unique temp file. The deltas are dropped only
// once the rename succeeded.
func (l *enforcementLog) flushLocked(now time.Time) {
	l.lastFlush = now
	if len(l.pending) == 0 {
		return
	}
	if os.MkdirAll(l.dir, 0700) != nil {
		l.writeErrors.Add(1)
		return
	}
	release, err := acquireJWTStoreLock(l.dailyPath())
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	defer release()

	d := l.readDaily()
	for date, counts := range l.pending {
		if d.Days[date] == nil {
			d.Days[date] = map[string]int{}
		}
		for key, n := range counts {
			d.Days[date][key] += n
			d.Total[key] += n
		}
	}
	cutoff := now.AddDate(0, 0, -enforcementKeepDays).Format("2006-01-02")
	for date := range d.Days {
		if date < cutoff {
			delete(d.Days, date)
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	tmp, err := os.CreateTemp(l.dir, enforcementDailyName+".*.tmp")
	if err != nil {
		l.writeErrors.Add(1)
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0600) != nil || os.Rename(tmp.Name(), l.dailyPath()) != nil {
		os.Remove(tmp.Name())
		l.writeErrors.Add(1)
		return
	}
	l.persisted = d
	l.pending = map[string]map[string]int{}
}

func (l *enforcementLog) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked(l.now().UTC())
}

// merged is the persisted counts plus what is still pending in this process.
func (l *enforcementLog) mergedLocked() enforcementDaily {
	d := enforcementDaily{Total: map[string]int{}, Days: map[string]map[string]int{}}
	for key, n := range l.persisted.Total {
		d.Total[key] = n
	}
	for date, counts := range l.persisted.Days {
		d.Days[date] = map[string]int{}
		for key, n := range counts {
			d.Days[date][key] = n
		}
	}
	for date, counts := range l.pending {
		if d.Days[date] == nil {
			d.Days[date] = map[string]int{}
		}
		for key, n := range counts {
			d.Days[date][key] += n
			d.Total[key] += n
		}
	}
	return d
}

// enforcementCount is how many events of one kind and mode fell in a window.
type enforcementCount struct {
	Kind, Mode, Window string
	Count              int
}

func splitKindMode(key string) (kind, mode string) {
	kind, mode, _ = strings.Cut(key, "|")
	return
}

// windowCounts sums the daily counts over today, the last 7 days and the last
// 30 days (UTC dates, today included), for /metrics.
func (l *enforcementLog) windowCounts() []enforcementCount {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.mergedLocked()
	today := l.now().UTC()
	totals := map[string]int{}
	for _, w := range []struct {
		name string
		days int
	}{{"today", 1}, {"7d", 7}, {"30d", 30}} {
		for i := 0; i < w.days; i++ {
			for key, n := range d.Days[today.AddDate(0, 0, -i).Format("2006-01-02")] {
				totals[w.name+"|"+key] += n
			}
		}
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]enforcementCount, 0, len(keys))
	for _, k := range keys {
		window, rest, _ := strings.Cut(k, "|")
		kind, mode := splitKindMode(rest)
		out = append(out, enforcementCount{Window: window, Kind: kind, Mode: mode, Count: totals[k]})
	}
	return out
}

// totals are the lifetime counts per kind and mode (monotonic, survive restarts).
func (l *enforcementLog) totals() []enforcementCount {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.mergedLocked()
	keys := make([]string, 0, len(d.Total))
	for k := range d.Total {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]enforcementCount, 0, len(keys))
	for _, k := range keys {
		kind, mode := splitKindMode(k)
		out = append(out, enforcementCount{Kind: kind, Mode: mode, Count: d.Total[k]})
	}
	return out
}

func (l *enforcementLog) suppressedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.suppressed
}

var globalEnforcementLog *enforcementLog

// initEnforcementLog starts the durable record and routes the connect
// library's [govern] events into it. A box with no home directory simply runs
// without it.
func initEnforcementLog() {
	dir := mustStateDir()
	if dir == "" {
		return
	}
	l := newEnforcementLog(dir, time.Now)
	globalEnforcementLog = l
	l.startAsync()
	connect.SetGovernEventSink(l.enqueue)
	go func() {
		for range time.Tick(enforcementFlushInterval) {
			l.flush()
		}
	}()
}

// writeEnforcementMetrics exposes the durable counts next to the in-process
// governor counters. The _total counter is monotonic across restarts, so
// Prometheus can take increase() over any window; the window gauges answer
// "the last 30 days" without a Prometheus.
func writeEnforcementMetrics(b *strings.Builder) {
	l := globalEnforcementLog
	if l == nil {
		return
	}
	fmt.Fprintf(b, "# HELP urnet_enforcement_events_total Governor events recorded in the durable enforcement log since it began, by kind and mode (survives restarts). Each kind counts events as the governors emit them: dead_client_release is rate-limited per destination, retry_budget is an aggregate report.\n# TYPE urnet_enforcement_events_total counter\n")
	for _, c := range l.totals() {
		fmt.Fprintf(b, "urnet_enforcement_events_total{kind=%q,mode=%q} %d\n", c.Kind, c.Mode, c.Count)
	}
	fmt.Fprintf(b, "# HELP urnet_enforcement_events Governor events in the durable log over today / the last 7 / 30 UTC days.\n# TYPE urnet_enforcement_events gauge\n")
	for _, c := range l.windowCounts() {
		fmt.Fprintf(b, "urnet_enforcement_events{kind=%q,mode=%q,window=%q} %d\n", c.Kind, c.Mode, c.Window, c.Count)
	}
	fmt.Fprintf(b, "# HELP urnet_enforcement_log_suppressed_total Event lines counted but not written because a kind exceeded the per-minute line limit.\n# TYPE urnet_enforcement_log_suppressed_total counter\nurnet_enforcement_log_suppressed_total %d\n", l.suppressedCount())
	fmt.Fprintf(b, "# HELP urnet_enforcement_log_dropped_total Events dropped because the log's queue was full (the disk stalled).\n# TYPE urnet_enforcement_log_dropped_total counter\nurnet_enforcement_log_dropped_total %d\n", l.dropped.Load())
	fmt.Fprintf(b, "# HELP urnet_enforcement_log_write_errors_total File operations of the enforcement log that failed and were swallowed. Non-zero means the durable record is incomplete.\n# TYPE urnet_enforcement_log_write_errors_total counter\nurnet_enforcement_log_write_errors_total %d\n", l.writeErrors.Load())
}
