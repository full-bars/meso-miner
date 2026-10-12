package connect

import (
	"strings"
	"testing"
)

func TestGovernFeedFieldsEmptyWhenNoFeed(t *testing.T) {
	SetGovernFeed(nil)
	if got := governFeedFields(); got != "" {
		t.Fatalf("with no feed the [govern] line must be unchanged, got %q", got)
	}
}

func TestGovernFeedFieldsShowsStateAndNone(t *testing.T) {
	defer SetGovernFeed(nil)
	SetGovernFeed(func() FeedAdvice { return FeedAdvice{} })
	if got := governFeedFields(); got != " feed=none" {
		t.Fatalf("invalid advice: %q", got)
	}
	SetGovernFeed(func() FeedAdvice {
		return FeedAdvice{Valid: true, State: "degraded", Confidence: 0.85, Coverage: 0.84}
	})
	got := governFeedFields()
	for _, want := range []string{"feed=degraded", "feed_conf=0.85", "feed_cov=0.84"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing %q", got, want)
		}
	}
}

func TestBudgetFieldsAppendsFeedOnlyWhenSet(t *testing.T) {
	defer SetGovernFeed(nil)
	r := &govReporter{}
	SetGovernFeed(nil)
	base := r.budgetFields()
	if strings.Contains(base, "feed") {
		t.Fatalf("no feed set but the line mentions it: %q", base)
	}
	SetGovernFeed(func() FeedAdvice { return FeedAdvice{Valid: true, State: "outage", Confidence: 1, Coverage: 1} })
	if got := r.budgetFields(); !strings.HasPrefix(got, base) || !strings.Contains(got, "feed=outage") {
		t.Fatalf("feed not appended after the existing fields: %q (base %q)", got, base)
	}
}

// the feed is observation only: it may add fields to the line, never change the verdict part
func TestFeedNeverChangesTheGovernorLineBody(t *testing.T) {
	defer SetGovernFeed(nil)
	line := func() string {
		r := &govReporter{}
		s, _ := r.next(govSnapshot{Degraded: true, Attempts: 100})
		return s
	}
	SetGovernFeed(nil)
	without := line()
	SetGovernFeed(func() FeedAdvice { return FeedAdvice{Valid: true, State: "ok", Confidence: 1, Coverage: 1} })
	with := line()
	if !strings.HasPrefix(with, without) || !strings.Contains(with, "DEGRADED") {
		t.Fatalf("feed altered the governor line body:\nwithout: %s\nwith:    %s", without, with)
	}
}
