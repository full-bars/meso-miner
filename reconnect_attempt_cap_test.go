package connect

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func newCapHarness(proxies int) (*budgetHarness, *[]string) {
	h := newBudgetHarness()
	h.budget.attemptCap.proxyCount = func() int { return proxies }
	var mu sync.Mutex
	lines := []string{}
	h.budget.emit = func(line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}
	return h, &lines
}

// capMinute records one minute of attempts and runs the control step in the
// middle of it, so each call judges exactly one new unix minute.
func (h *budgetHarness) capMinute(class connectClass, failed, ok int) {
	h.attempts(class, failed)
	h.attempts(connectOK, ok)
	h.advance(30 * time.Second)
	h.budget.control()
	h.advance(30 * time.Second)
}

func TestAttemptCap_EngagesOnlyOnSustainedFailureOfLargeNodes(t *testing.T) {
	type minute struct {
		class      connectClass
		failed, ok int
	}
	storm := func(class connectClass) minute { return minute{class, 90, 10} }
	cases := []struct {
		name    string
		proxies int
		minutes []minute
		want    float64
	}{
		{"small node is never capped", attemptCapMinProxies - 1, []minute{storm(connectSocksFail), storm(connectSocksFail), storm(connectSocksFail)}, 0},
		{"one storm minute is not enough", 4000, []minute{storm(connectSocksFail)}, 0},
		{"socks storm", 4000, []minute{storm(connectSocksFail), storm(connectSocksFail)}, 4000.0 / attemptCapProxiesPerToken},
		{"silent timeout storm", 4000, []minute{storm(connectTimeoutNoReply), storm(connectTimeoutNoReply)}, 4000.0 / attemptCapProxiesPerToken},
		{"tls storm", 4000, []minute{storm(connectTlsFail), storm(connectTlsFail)}, 4000.0 / attemptCapProxiesPerToken},
		{"other storm", 4000, []minute{storm(connectOtherFail), storm(connectOtherFail)}, 4000.0 / attemptCapProxiesPerToken},
		{"refused storm", 4000, []minute{storm(connectRefused), storm(connectRefused)}, 4000.0 / attemptCapProxiesPerToken},
		{"cap never below the floor", attemptCapMinProxies, []minute{storm(connectSocksFail), storm(connectSocksFail)}, budgetFloorRate},
		{"exactly 80% failing is not a storm", 4000, []minute{{connectSocksFail, 80, 20}, {connectSocksFail, 80, 20}}, 0},
		{"a healthy minute between storms resets", 4000, []minute{storm(connectSocksFail), {connectSocksFail, 10, 90}, storm(connectSocksFail)}, 0},
		{"too few attempts say nothing", 4000, []minute{{connectSocksFail, 19, 0}, {connectSocksFail, 19, 0}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newCapHarness(tc.proxies)
			for _, m := range tc.minutes {
				h.capMinute(m.class, m.failed, m.ok)
			}
			if got := h.budget.currentCap(); got != tc.want {
				t.Fatalf("cap = %v, want %v", got, tc.want)
			}
			wantEffective := float64(budgetCeilingRate)
			if 0 < tc.want {
				wantEffective = tc.want
			}
			if got := h.budget.effectiveRate(); got != wantEffective {
				t.Fatalf("effective rate = %v, want %v", got, wantEffective)
			}
			if h.budget.throttled() || h.budget.currentRate() != budgetCeilingRate {
				t.Fatal("the cap is a host hold and must never read as a backend outage")
			}
		})
	}
}

func TestAttemptCap_ReleasesWhenAttemptsSucceed(t *testing.T) {
	cases := []struct {
		name       string
		failed, ok int
		wantOff    bool
	}{
		{"20% success releases", 8, 2, true},
		{"25% success releases", 9, 3, true},
		{"10% success holds", 18, 2, false},
		{"too few attempts hold", 6, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, lines := newCapHarness(4000)
			h.capMinute(connectTimeoutNoReply, 90, 10)
			h.capMinute(connectTimeoutNoReply, 90, 10)
			if h.budget.currentCap() == 0 {
				t.Fatal("setup: cap should be on")
			}
			// the next minute is still a storm, judged before the recent window
			h.attempts(connectTimeoutNoReply, 90)
			h.advance(budgetRecentWindow + time.Second)
			h.budget.control()
			if h.budget.currentCap() == 0 {
				t.Fatal("setup: a third storm minute keeps the cap on")
			}
			// a recent window, inside the same minute
			h.attempts(connectTimeoutNoReply, tc.failed)
			h.attempts(connectOK, tc.ok)
			h.advance(time.Second)
			h.budget.control()
			if off := h.budget.currentCap() == 0; off != tc.wantOff {
				t.Fatalf("cap off = %v, want %v (lines %q)", off, tc.wantOff, *lines)
			}
		})
	}
}

func TestAttemptCap_NeverAppliesInsideTheStartupGrace(t *testing.T) {
	h, _ := newCapHarness(4000)
	h.gov.started = *h.now
	// judged 30 s in: inside the grace, so it must not count
	h.capMinute(connectSocksFail, 90, 10)
	if h.budget.currentCap() != 0 {
		t.Fatal("the cap must not engage inside the startup grace")
	}
	// judged 90 s in: the grace has just ended, and this is the first storm
	// minute that counts, so it alone must not engage the cap
	h.capMinute(connectSocksFail, 90, 10)
	if h.budget.currentCap() != 0 {
		t.Fatal("a storm minute inside the grace must not count toward the cap")
	}
	h.capMinute(connectSocksFail, 90, 10)
	if h.budget.currentCap() == 0 {
		t.Fatal("two storm minutes after the grace engage the cap")
	}
}

func TestAttemptCap_LogsTransitionsAndShowsInTheGovernLine(t *testing.T) {
	h, lines := newCapHarness(4000)
	h.capMinute(connectSocksFail, 90, 10)
	if len(*lines) != 0 {
		t.Fatalf("nothing to say before the cap engages: %q", *lines)
	}
	h.capMinute(connectSocksFail, 90, 10)
	h.capMinute(connectSocksFail, 90, 10)
	on := 0
	for _, line := range *lines {
		if strings.HasPrefix(line, "[govern] attempt cap on:") {
			on += 1
		}
	}
	if on != 1 {
		t.Fatalf("the cap must log its start once, got %q", *lines)
	}

	reporter := &govReporter{rate: h.budget.currentRate, waiting: h.budget.queued, capRate: h.budget.currentCap}
	if fields := reporter.budgetFields(); !strings.Contains(fields, " cap=6.7/s") {
		t.Fatalf("the [govern] line must show the cap, got %q", fields)
	}

	h.capMinute(connectSocksFail, 10, 90)
	if h.budget.currentCap() != 0 {
		t.Fatal("a healthy minute releases the cap")
	}
	if last := (*lines)[len(*lines)-1]; !strings.HasPrefix(last, "[govern] attempt cap off:") {
		t.Fatalf("the release must be logged, got %q", last)
	}
	if fields := reporter.budgetFields(); strings.Contains(fields, "cap=") {
		t.Fatalf("no cap field while it is off, got %q", fields)
	}
}

// A capped budget really holds connects: a proxied connect waits for a token.
func TestAttemptCap_ProxiedConnectsQueue(t *testing.T) {
	h, _ := newCapHarness(4000)
	h.capMinute(connectSocksFail, 90, 10)
	h.capMinute(connectSocksFail, 90, 10)
	if !h.budget.admit(t.Context(), true) {
		t.Fatal("the direct path never waits")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter(ctx, h.budget, false)
	waitQueued(t, h.budget, 1)
}

func TestProxiedCount_LeavesOutTheDirectPath(t *testing.T) {
	ResetProxyHealthForTesting()
	defer ResetProxyHealthForTesting()
	RegisterProxy(0, "direct", "direct")
	for i := 1; i <= 3; i++ {
		RegisterProxy(i, "127.0.0.1:1080", "k")
	}
	if got := proxiedCount(); got != 3 {
		t.Fatalf("proxiedCount = %d, want 3", got)
	}
}
