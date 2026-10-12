package main

import (
	"context"
	"time"

	"github.com/urnetwork/connect"
)

// hourlyPulseBackendDegraded and hourlyPulseTrigger are replaceable so tests do
// not depend on the process-wide governor or wake real transports.
var (
	hourlyPulseBackendDegraded = connect.BackendDegraded
	hourlyPulseTrigger         = connect.TriggerPulse
)

// hourlyPulseRetry is how long a skipped pulse waits before trying again.
const hourlyPulseRetry = 5 * time.Minute

// hourlyPulse runs one hourly maintenance pulse, which wakes every stalled
// transport and proxy and restarts their backoff ladders. While the backend is
// degraded, or the connect budget was throttled within the last few minutes,
// that reset only sends every failing proxy back into the outage at once, so
// the pulse is skipped and the caller tries again every hourlyPulseRetry until it can fire. It reports whether the
// pulse fired.
func hourlyPulse() bool {
	if hourlyPulseBackendDegraded() {
		tlog("[pulse] skipped hourly reset (backend degraded)\n")
		return false
	}
	if connect.ProxyHealthCount() > 0 {
		_, dead, degraded, _, connecting := connect.ProxyHealthSnapshot()
		down := len(dead) + len(degraded)
		tlog("[hourly-maintenance] reconnecting stalled transports: down=%d dead=%d degraded=%d connecting=%d\n",
			down, len(dead), len(degraded), len(connecting))
	}
	hourlyPulseTrigger()
	return true
}

// hourlyPulseLoop runs the pulse every interval. A pulse that is skipped (the
// backend is degraded) is retried every retry until it fires, so a node that
// happens to be degraded at the hour mark is not starved of its pulse for
// another whole hour.
func hourlyPulseLoop(ctx context.Context, interval, retry time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		for !hourlyPulse() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retry):
			}
		}
	}
}
