package connect

import (
	mathrand "math/rand"
	"sync/atomic"
	"time"
)

// defaultFlapStableAfter is how long a session must live before it counts as
// real service rather than a flap. A healthy session lasts minutes to days, so
// this is far below any legitimate lifetime and far above the seconds a
// session lasts when the backend accepts the websocket and then cannot keep it.
const defaultFlapStableAfter = 60 * time.Second

// flapBackoffCeiling caps the extra wait after sessions that keep dying young.
// It matches the ceiling of the failed-connect backoff, so a flapping proxy is
// never slower to retry than a proxy that cannot connect at all.
const flapBackoffCeiling = 60 * time.Second

// flapBackoffFloor is the first wait when the reconnect timeout is unset, so a
// zero setting cannot make the guard a no-op tight loop.
const flapBackoffFloor = time.Second

// flapBackoffJitter spreads each wait by +-20%, so proxies that began flapping
// together do not reconnect in lockstep waves.
const flapBackoffJitter = 0.2

// transportFlapBackoffs counts how many times the guard held a reconnect back
// because the previous session died young. It is the process-wide signal for
// "sessions are being lost faster than they are worth", exposed for the
// [health] heartbeat so a flap storm is visible instead of being a silent loop.
var transportFlapBackoffs atomic.Int64

func TransportFlapBackoffs() int64 { return transportFlapBackoffs.Load() }

// flapGuard turns "the connect succeeded" into "the connect succeeded and the
// session lasted". Without it a session that dies seconds after the handshake
// counts as a full success: the failed-connect backoff resets and the shared
// backend-failure streak clears, so the next connect starts immediately and a
// slow backend produces an endless connect/die loop per proxy. With thousands
// of proxies that loop is the traffic flood, because each iteration is several
// SOCKS and TLS handshakes.
//
// The wait escalates only across consecutive young sessions. One session that
// lives stableAfter or longer clears it, so a healthy proxy that reconnects
// after a long session (a deploy, a hot restart, a network change) sees no
// added delay at all.
//
// Not safe for concurrent use: one guard belongs to one connect loop.
type flapGuard struct {
	stableAfter time.Duration
	base        time.Duration
	// jitter is replaceable so tests can assert exact waits
	jitter func(time.Duration) time.Duration

	streak time.Duration
	// everStable is set once a session has lived stableAfter or longer: from then
	// on this proxy's early deaths are evidence about the platform, not about it
	everStable bool
}

// newFlapGuard returns a guard that treats sessions shorter than stableAfter as
// flaps. A negative stableAfter disables it; zero selects the default.
func newFlapGuard(stableAfter time.Duration, base time.Duration) *flapGuard {
	if stableAfter == 0 {
		stableAfter = defaultFlapStableAfter
	}
	if base <= 0 {
		base = flapBackoffFloor
	}
	return &flapGuard{
		stableAfter: stableAfter,
		base:        min(base, flapBackoffCeiling),
		jitter:      jitterFlapWait,
	}
}

// isYoung reports whether a session of this length counts as a flap. It does
// not advance the streak.
func (self *flapGuard) isYoung(lived time.Duration) bool {
	return 0 <= self.stableAfter && lived < self.stableAfter
}

// sessionEnded records a finished session and returns the extra wait before the
// next connect. Zero means reconnect right away, as before the guard.
func (self *flapGuard) sessionEnded(lived time.Duration) time.Duration {
	if self.stableAfter < 0 {
		return 0
	}
	if self.stableAfter <= lived {
		self.streak = 0
		self.everStable = true
		return 0
	}
	if self.streak == 0 {
		self.streak = self.base
	} else {
		self.streak = min(self.streak*2, flapBackoffCeiling)
	}
	transportFlapBackoffs.Add(1)
	return self.jitter(self.streak)
}

func jitterFlapWait(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return d + time.Duration((mathrand.Float64()*2-1)*flapBackoffJitter*float64(d))
}
