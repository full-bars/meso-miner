package connect

import (
	"strings"
	"time"
)

// GovernEvent is one [govern] event line with what it means: which kind of
// action it reports and which mode that action ran under. The embedding program
// keeps these durably (the ramlogs roll over within the hour).
type GovernEvent struct {
	Kind string
	Mode string
	Line string
	// At is when the event was emitted. A sink that records it later (a queue
	// behind a stalled disk) keeps the emit time instead of the time it got to it.
	At time.Time
}

// Kinds that are not enforcement. They still pass through the sink so the
// program can decide, but they must not be counted as something the governors did.
const GovernKindObservation = "observation"

// govern event markers. governEventMarkers is checked by TestGovernEventMarkersAreEmitted
// against the source, so rewording an emit site without updating the marker
// fails a test instead of quietly turning events into "other".
var governEventMarkers = []struct{ kind, marker string }{
	{"dead_client_release", "released dead client"},
	{"dead_client_release", "shadow, not released"},
	{"budget_cut", "budget cut"},
	{"budget_reopen", "budget reopened"},
	{"attempt_cap_on", "attempt cap on"},
	{"attempt_cap_off", "attempt cap off"},
	{"memory_hold", "memory pressure: holding"},
	{"memory_hold_end", "memory pressure over"},
	{"crash_loop", "crash loop"},
	{"restart_marker", "restart marker"},
	{"retry_budget", "retry budget"},
	{GovernKindObservation, "[t]mass disconnect"},
}

// ClassifyGovernLine returns the kind and mode of a [govern] event line. It
// reads only atomics: it is called from emit sites that may hold the budget
// lock, so it must never take a lock of its own or call back into the governor.
func ClassifyGovernLine(line string) (kind, mode string) {
	kind = "other"
	for _, m := range governEventMarkers {
		if strings.Contains(line, m.marker) {
			kind = m.kind
			break
		}
	}
	switch kind {
	case "dead_client_release":
		// the dead-client feature has its own switch, URNETWORK_DEAD_CLIENT
		mode = govModeName(currentDeadClientMode())
		if strings.Contains(line, "shadow, not released") {
			mode = "shadow"
		}
	default:
		mode = govModeName(currentGovernMode())
	}
	return kind, mode
}
