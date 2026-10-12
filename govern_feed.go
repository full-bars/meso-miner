package connect

import (
	"fmt"
	"sync/atomic"
)

// FeedAdvice is a verified reading of the external signed backend-health feed
// (the verdict service). It is observation only: nothing in the governor reads
// it to decide anything. Valid=false means no usable reading, which is the same
// as the feed not existing.
type FeedAdvice struct {
	Valid      bool
	State      string // "ok", "degraded" or "outage"
	Confidence float64
	Coverage   float64
}

var governFeed atomic.Pointer[func() FeedAdvice]

// SetGovernFeed registers the function the governor's log lines call to show
// what the external feed says next to the host's own verdict, so an operator
// can compare the two before anything is ever allowed to act on the feed. The
// connect package does not import the feed client: the provider injects this.
func SetGovernFeed(fn func() FeedAdvice) {
	if fn == nil {
		governFeed.Store(nil)
		return
	}
	governFeed.Store(&fn)
}

// governFeedFields is appended to every [govern] line; empty when no feed is set.
func governFeedFields() string {
	fn := governFeed.Load()
	if fn == nil {
		return ""
	}
	a := (*fn)()
	if !a.Valid {
		return " feed=none"
	}
	return fmt.Sprintf(" feed=%s feed_conf=%.2f feed_cov=%.2f", a.State, a.Confidence, a.Coverage)
}
