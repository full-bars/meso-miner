package main

import (
	"github.com/urnetwork/connect"
)

// hourlyPulseBackendDegraded and hourlyPulseTrigger are replaceable so tests do
// not depend on the process-wide governor or wake real transports.
var (
	hourlyPulseBackendDegraded = connect.BackendDegraded
	hourlyPulseTrigger         = connect.TriggerPulse
)

// hourlyPulse runs one hourly maintenance pulse, which wakes every stalled
// transport and proxy and restarts their backoff ladders. While the backend is
// degraded, or the connect budget was throttled within the last few minutes,
// that reset only sends every failing proxy back into the outage at once, so
// the pulse is skipped and the next hour tries again. It reports whether the
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
