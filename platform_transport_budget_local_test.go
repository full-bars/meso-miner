package connect

import (
	"context"
	"testing"
	"time"
)

// A small local check of the primitives this stage adopts, in the
// NewPlatformTransportBudget(bytes, 0) style: admission against the byte cap,
// a refusal past it, and a release that returns the budget to zero. The
// upstream test file next to this one covers the full reservation and handoff
// machinery; this is the minimal smoke the port is accepted against.
func TestPlatformTransportBudgetAdmissionAndReleasePortStage1(t *testing.T) {
	budget := NewPlatformTransportBudget(8, 0)
	first := budget.register(platformTransportBudgetH1, 8, true)
	if !first.Acquire(t.Context()) {
		t.Fatal("an 8 byte claim against an 8 byte budget was refused")
	}
	if stats := budget.Stats(); stats.UsedByteCount != 8 {
		t.Fatalf("used bytes after acquire = %d, want 8", stats.UsedByteCount)
	}

	second := budget.register(platformTransportBudgetH3Auto, 1, false)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if second.Acquire(ctx) {
		t.Fatal("a claim beyond the byte cap was admitted")
	}

	first.Release()
	if stats := budget.Stats(); stats.UsedByteCount != 0 || stats.UsedTransportCount != 0 {
		t.Fatalf("budget not returned after release: %+v", stats)
	}
}
