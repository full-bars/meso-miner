package connect

import (
	"time"
)

// Early deaths of proxies that are not yet known stable.
//
// An early death counts as a hard failure only for a proxy that has held a
// stable session in THIS process (flapGuard.everStable): a proxy that never
// lived long may simply be a poor one. That leaves a fresh process blind to the
// incident's own shape. Restarted into a backend that accepts the websocket and
// drops it within seconds, no proxy ever becomes stable, so no early death ever
// counts, the verdict never trips and the byte breaker (which only sees attempts
// that failed outright) stays quiet.
//
// So once unproven early deaths are a large share of the attempts for two
// minutes running, they count as hard failures too. It needs a learned baseline
// (kept across restarts in govern_baseline): the degraded threshold is then
// relative to what this host normally sees, so a host whose poor proxy list
// always dies young has learned that as its normal and is not flagged by it,
// and a brand-new host with no baseline keeps the old, conservative behaviour.

const (
	// govUnprovenShare: a minute qualifies when more than this share of its
	// attempts ended in an early death of an unproven proxy
	govUnprovenShare = 0.3
	// govUnprovenMinutes is how many consecutive qualifying minutes it takes
	govUnprovenMinutes = 2
)

type unprovenEarlyState struct {
	// lastMinute is the unix minute last judged, streak the consecutive
	// qualifying minutes ending there
	lastMinute int64
	streak     int
}

// observeUnprovenLocked judges the trailing minute once per unix minute. It is
// called from snapshotWindow, which every consumer of the verdict goes through
// (the budget's control step every 5 s, the reporter every minute), so the
// minutes it judges are consecutive whenever the governor is in use. Caller
// holds mu.
func (self *connectGovernor) observeUnprovenLocked() {
	now := self.now()
	m := now.Unix() / 60
	u := &self.unproven
	if u.lastMinute == m {
		return
	}
	consecutive := u.lastMinute == m-1
	u.lastMinute = m
	if now.Sub(self.started) < govStartupGrace || self.baseline <= 0 {
		u.streak = 0
		return
	}
	var attempts, early, proven int64
	cutoff := now.Unix() - int64(govWindow/time.Second)
	for i := range self.ring {
		b := &self.ring[i]
		if b.sec <= cutoff {
			continue
		}
		attempts += b.attempts
		early += b.early
		proven += b.earlyProven
	}
	unproven := early - proven
	if attempts < govMinAttempts || float64(unproven) <= govUnprovenShare*float64(attempts) {
		u.streak = 0
		return
	}
	if consecutive {
		u.streak += 1
	} else {
		u.streak = 1
	}
}

// unprovenEarlyHardLocked reports whether unproven early deaths currently count
// as hard failures. Caller holds mu.
func (self *connectGovernor) unprovenEarlyHardLocked() bool {
	return govUnprovenMinutes <= self.unproven.streak
}
