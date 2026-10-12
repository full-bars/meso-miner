package connect

import (
	"testing"
	"time"
)

// quietInterval moves past the recent window so the next control interval sees
// no attempts at all, then runs it.
func (h *budgetHarness) quietInterval() {
	h.advance(budgetRecentWindow + time.Second)
	h.budget.control()
}

// After a cut on evidence, an interval in which nothing reached the platform is
// what an outage the classifier cannot see looks like. The no-evidence reopen
// must creep instead of doubling straight back to the rate that was just cut.
func TestBudget_ProbeAfterACutCreepsInsteadOfDoubling(t *testing.T) {
	cutBytes := func(h *budgetHarness) { h.failedBytes(connectTimeoutNoReply, 400, 20_000) }
	cutVerdict := func(h *budgetHarness) { h.attempts(connectTimeoutPlatform, 200) }
	afterCut := budgetCeilingRate / budgetDecreaseFactor

	cases := []struct {
		name string
		// cut runs before the first control interval; nil means no cut
		cut   func(h *budgetHarness)
		start float64
		// quiet is how many no-evidence intervals follow
		quiet int
		want  float64
	}{
		{name: "no recent cut doubles as before", start: budgetFloorRate, quiet: 1, want: budgetFloorRate * budgetIncreaseFactor},
		{name: "no recent cut doubles twice", start: budgetFloorRate, quiet: 2, want: budgetFloorRate * budgetIncreaseFactor * budgetIncreaseFactor},
		{name: "bytes cut holds below the step", cut: cutBytes, start: budgetCeilingRate, quiet: budgetProbeRampEvery - 1, want: afterCut},
		{name: "bytes cut takes one step", cut: cutBytes, start: budgetCeilingRate, quiet: budgetProbeRampEvery, want: afterCut + budgetProbeStep},
		{name: "bytes cut takes two steps", cut: cutBytes, start: budgetCeilingRate, quiet: 2 * budgetProbeRampEvery, want: afterCut + 2*budgetProbeStep},
		{name: "verdict cut holds below the step", cut: cutVerdict, start: budgetCeilingRate, quiet: budgetProbeRampEvery - 1, want: afterCut},
		{name: "verdict cut takes one step", cut: cutVerdict, start: budgetCeilingRate, quiet: budgetProbeRampEvery, want: afterCut + budgetProbeStep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBudgetHarness()
			h.budget.setRate(tc.start)
			if tc.cut != nil {
				tc.cut(h)
				h.budget.control()
				if got := h.budget.currentRate(); got != afterCut {
					t.Fatalf("setup: rate after the cut = %v, want %v", got, afterCut)
				}
			}
			for i := 0; i < tc.quiet; i++ {
				h.quietInterval()
			}
			if got := h.budget.currentRate(); got != tc.want {
				t.Fatalf("rate after %d quiet intervals = %v, want %v", tc.quiet, got, tc.want)
			}
		})
	}
}

// Once the holdoff has passed with no further cut, the old probe-harder doubling
// returns, so a budget can never be stuck low on no evidence for ever.
func TestBudget_ProbeHoldoffExpires(t *testing.T) {
	h := newBudgetHarness()
	h.failedBytes(connectTimeoutNoReply, 400, 20_000)
	h.budget.control()
	before := h.budget.currentRate()
	h.advance(budgetProbeHoldoff)
	h.budget.control()
	if got := h.budget.currentRate(); got != before*budgetIncreaseFactor {
		t.Fatalf("after the holdoff a quiet interval must double: got %v, want %v", got, before*budgetIncreaseFactor)
	}
}

// Real evidence of recovery is never slowed by the holdoff: attempts that reach
// the platform and connect open the budget at the usual pace.
func TestBudget_EvidenceStillReopensFastAfterACut(t *testing.T) {
	h := newBudgetHarness()
	h.failedBytes(connectTimeoutNoReply, 400, 20_000)
	h.budget.control()
	before := h.budget.currentRate()
	h.advance(budgetRecentWindow + time.Second)
	h.attempts(connectOK, 20)
	h.budget.control()
	if got := h.budget.currentRate(); got != before*budgetIncreaseFactor {
		t.Fatalf("connects that land must reopen at the usual pace: got %v, want %v", got, before*budgetIncreaseFactor)
	}
}
