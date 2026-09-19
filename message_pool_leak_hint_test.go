package connect

import (
	"testing"
)

// TestMessagePoolLeakHintAttribution verifies the leak-reporting snapshot
// surfaced in the [health][pool][leak] heartbeat line. It is the operator
// hook that turns "buffers were taken and never given back" (LA7, 4.19M
// buffers) into a named call site: with URNETWORK_POOL_DEBUG_TAGS=1, a buffer
// that is acquired but never returned must show up as a tag whose leaked
// count grows while its returned ratio stays below 100%.
func TestMessagePoolLeakHintAttribution(t *testing.T) {
	// debugTags is permanently on; just make sure the leak hint reflects a
	// deliberately-leaked buffer so the heartbeat can name the caller.
	if !debugTags {
		t.Fatal("debugTags must be true (on by default)")
	}

	// Sanity: with tags on, a clean get+return cycle must not leak.
	ResetMessagePoolStats()
	b := MessagePoolGet(64)
	MessagePoolReturn(b)
	hints := MessagePoolLeakHint(10)
	for _, h := range hints {
		if h.Leaked == 0 {
			continue
		}
		t.Fatalf("clean get+return must not leak, but tag=%d caller=%s leaked=%d", h.Tag, h.Caller, h.Leaked)
	}

	// Now actually leak one: acquire and never return. The snapshot must
	// reflect it (leaked > 0) so the heartbeat can name the caller.
	ResetMessagePoolStats()
	leaked := MessagePoolGet(2048)
	if leaked == nil {
		t.Fatal("MessagePoolGet(2048) returned nil")
	}
	hints = MessagePoolLeakHint(10)
	found := false
	for _, h := range hints {
		if h.Leaked > 0 {
			found = true
			if h.Caller == "" {
				t.Errorf("leaky tag %d has empty caller (debugTag not stamped?)", h.Tag)
			}
		}
	}
	if !found {
		t.Fatal("deliberately leaked buffer did not appear in MessagePoolLeakHint")
	}
}

// TestMessagePoolLeakHintRequiresDebugTags is retained as documentation of the
// no-tags behavior guard: even if debugTags were ever disabled, the leak hint
// must return nothing rather than a meaningless tag-0 aggregate.
func TestMessagePoolLeakHintRequiresDebugTags(t *testing.T) {
	debugTags = false
	t.Cleanup(func() { debugTags = true })
	ResetMessagePoolStats()
	_ = MessagePoolGet(64)
	if hints := MessagePoolLeakHint(5); len(hints) != 0 {
		t.Fatalf("MessagePoolLeakHint must be empty when debugTags is off, got %v", hints)
	}
}
