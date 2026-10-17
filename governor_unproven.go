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
// minutes running, they count as hard failures too. Two things keep it from
// flagging a host whose poor proxy list always dies young. It needs the learned
// platform baseline (kept across restarts in govern_baseline), so a brand-new
// host keeps the old, conservative behaviour. And the unproven share must also
// be at least govUnprovenRelative times this process's own running average of
// the shares it saw outside such episodes, so a host that normally sees 25% has
// learned that as normal and needs 50% to be flagged. Shares above the absolute
// line are never fed into that average, so an incident cannot teach itself as
// normal. Until a few ordinary minutes have been seen there is nothing to compare
// with, and the absolute share alone decides (a fresh process restarted into an
// accept-then-drop backend never sees an ordinary minute, and must still be
// governed).

const (
	// govUnprovenShare: a minute qualifies when more than this share of its
	// attempts ended in an early death of an unproven proxy
	govUnprovenShare = 0.3
	// govUnprovenMinutes is how many consecutive qualifying minutes it takes
	govUnprovenMinutes = 2
	// govUnprovenRelative: and the share must be this many times the host's usual
	govUnprovenRelative = 2.0
	// govUnprovenLearnMinutes: ordinary minutes seen before the rule may fire
	govUnprovenLearnMinutes = 5
	// govUnprovenAlpha is the weight of each new ordinary minute in the average
	govUnprovenAlpha = 0.1
)

type unprovenEarlyState struct {
	// lastMinute is the unix minute last judged, streak the consecutive
	// qualifying minutes ending there
	lastMinute int64
	streak     int
	// usual is the running average share of unproven early deaths in minutes
	// that did not qualify, learned counts those minutes
	usual   float64
	learned int
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
	if attempts < govMinAttempts {
		u.streak = 0
		return
	}
	share := float64(early-proven) / float64(attempts)
	if share <= govUnprovenShare {
		// an ordinary minute: it is what this host normally sees
		if u.learned == 0 {
			u.usual = share
		} else {
			u.usual += govUnprovenAlpha * (share - u.usual)
		}
		u.learned += 1
	}
	if now.Sub(self.started) < govStartupGrace || self.baseline <= 0 ||
		share <= govUnprovenShare ||
		(govUnprovenLearnMinutes <= u.learned && share < govUnprovenRelative*u.usual) {
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
