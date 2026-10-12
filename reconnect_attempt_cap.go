package connect

import (
	"fmt"
	"time"
)

// The per-host attempt cap.
//
// The verdict only weighs evidence that the platform answered, so a storm made
// of attempts it gives no weight (SOCKS failures, silent timeouts, TLS errors,
// unclassified failures, refusals) never cuts the budget: the node keeps making
// about N/60 full connect attempts a second for as long as it lasts. The cap is
// the blunt backstop for that blind spot. It does not judge WHY attempts fail,
// only that almost all of them do, minute after minute, and then holds the
// node's connect rate to a fraction of its proxy count until attempts start
// succeeding again.
//
// It is a host-level hold like memory pressure, not a backend verdict: it never
// touches the budget's rate, the outage marker or OutageRecent, so the reapers
// keep culling dead proxies while it is on.

const (
	// attemptCapMinProxies: nodes with fewer proxies than this are never capped.
	// They cannot make enough attempts to matter, and their behaviour must not
	// change.
	attemptCapMinProxies = 60
	// attemptCapProxiesPerToken: the cap is one attempt per second for every
	// this many proxies (each proxy may try about once every 10 minutes), and
	// never below budgetFloorRate
	attemptCapProxiesPerToken = 600
	// attemptCapFailShare: a minute is a storm minute when more than this share
	// of its attempts failed, in any class
	attemptCapFailShare = 0.8
	// attemptCapPoolShare: a storm minute also needs at least this share of the
	// proxy pool to have attempted in it. A healthy node still has a dead
	// minority retrying about once a minute, and that minority fails nearly every
	// attempt without anything being wrong; a real storm has most of the pool
	// cycling at once.
	attemptCapPoolShare = 0.5
	// attemptCapStormMinutes is how many consecutive storm minutes engage the cap
	attemptCapStormMinutes = 2
	// attemptCapCooldown: after the cap releases it does not engage again for this
	// long, so a borderline node cannot flap on and off
	attemptCapCooldown = 10 * time.Minute
	// attemptCapMinAttempts: a minute with fewer attempts says nothing
	attemptCapMinAttempts = govMinAttempts
	// attemptCapReleaseShare: a recent window with at least this share of
	// successful attempts releases the cap
	attemptCapReleaseShare = 0.2
	// attemptCapReleaseMinAttempts: the recent window needs at least this many
	// attempts to release on success
	attemptCapReleaseMinAttempts = budgetMinDecreaseAttempts
)

type attemptCapState struct {
	// rate is the cap in attempts per second, 0 while off
	rate float64
	// storm counts consecutive storm minutes; lastMinute is the unix minute last judged
	storm      int
	lastMinute int64
	// releasedAt is when the cap last released, zero if it never has
	releasedAt time.Time
	// proxyCount reads how many proxies the node runs; replaceable so tests are exact
	proxyCount func() int
}

// proxiedCount is how many proxies are registered, the direct path (index 0)
// not included.
func proxiedCount() int {
	proxyHealthMu.Lock()
	defer proxyHealthMu.Unlock()
	n := len(proxyHealthByIndex)
	if _, ok := proxyHealthByIndex[0]; ok {
		n -= 1
	}
	return n
}

// attemptCapRateFor is the cap for a node with this many proxies.
func attemptCapRateFor(proxies int) float64 {
	return max(float64(proxies)/attemptCapProxiesPerToken, budgetFloorRate)
}

// stormMinute reports whether a minute's attempts were almost all failures AND
// came from a large share of the pool. Canceled attempts are never recorded, so
// every recorded attempt that is not a connect counts as a failure, whatever its
// class.
func stormMinute(s govSnapshot, proxies int) (bool, float64) {
	if s.Attempts < attemptCapMinAttempts {
		return false, 0
	}
	if float64(s.Attempts) < attemptCapPoolShare*float64(proxies) {
		return false, float64(s.Attempts-s.OK) / float64(s.Attempts)
	}
	failShare := float64(s.Attempts-s.OK) / float64(s.Attempts)
	return attemptCapFailShare < failShare, failShare
}

// currentCap is the attempt cap now, 0 while off.
func (self *reconnectBudget) currentCap() float64 {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.attemptCap.rate
}

// updateCapLocked judges the attempt cap once per unix minute from the last
// minute's attempts, and releases it as soon as a recent window shows attempts
// succeeding again. It returns the transition lines to log. Caller holds mu.
func (self *reconnectBudget) updateCapLocked(recent, minute govSnapshot, proxies int) []string {
	c := &self.attemptCap
	if proxies < attemptCapMinProxies || minute.StartupGrace {
		c.storm = 0
		c.lastMinute = 0
		if 0 < c.rate {
			c.rate = 0
			return []string{"[govern] attempt cap off: node below the size it applies to, or inside the startup grace"}
		}
		return nil
	}

	var events []string
	release := func(why string) {
		if 0 < c.rate {
			c.rate = 0
			c.releasedAt = self.now()
			events = append(events, "[govern] attempt cap off: "+why)
		}
	}

	m := self.now().Unix() / 60
	if c.lastMinute != m {
		consecutive := c.lastMinute == m-1
		c.lastMinute = m
		storm, failShare := stormMinute(minute, proxies)
		switch {
		case !storm:
			c.storm = 0
			release(fmt.Sprintf("the last minute was not a failure storm (attempts=%d failed=%.0f%%)", minute.Attempts, 100*failShare))
		case consecutive:
			c.storm += 1
		default:
			c.storm = 1
		}
		if c.rate == 0 && attemptCapStormMinutes <= c.storm &&
			(c.releasedAt.IsZero() || attemptCapCooldown <= self.now().Sub(c.releasedAt)) {
			c.rate = attemptCapRateFor(proxies)
			events = append(events, fmt.Sprintf(
				"[govern] attempt cap on: more than %.0f%% of attempts failed in each of the last %d minutes (last minute attempts=%d failed=%.0f%%, any class), holding connects to %s for %d proxies until %.0f%% succeed",
				100*attemptCapFailShare, c.storm, minute.Attempts, 100*failShare, rateString(c.rate), proxies, 100*attemptCapReleaseShare))
		}
	}

	if 0 < c.rate {
		// follow the pool if it grows or shrinks while the cap is on
		c.rate = attemptCapRateFor(proxies)
		if attemptCapReleaseMinAttempts <= recent.Attempts &&
			attemptCapReleaseShare*float64(recent.Attempts) <= float64(recent.OK) {
			c.storm = 0
			release(fmt.Sprintf("attempts are succeeding again (recent attempts=%d connected=%d)", recent.Attempts, recent.OK))
		}
	}
	return events
}

// controlCap runs the attempt cap's control step and logs its transitions. The
// proxy count and both snapshots are read before the budget lock is taken.
func (self *reconnectBudget) controlCap(recent govSnapshot) {
	proxies := 0
	if self.attemptCap.proxyCount != nil {
		proxies = self.attemptCap.proxyCount()
	}
	if proxies < attemptCapMinProxies && self.currentCap() == 0 {
		// small nodes never pay for the minute snapshot
		return
	}
	minute := self.gov.snapshotWindow(govWindow)
	self.mu.Lock()
	events := self.updateCapLocked(recent, minute, proxies)
	emit := self.emit
	self.mu.Unlock()
	if emit != nil {
		for _, line := range events {
			emit(line)
		}
	}
}
