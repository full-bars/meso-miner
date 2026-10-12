package connect

import (
	"context"
	mathrand "math/rand"
	"time"
)

// pulseSpread is how far the hourly pulse spreads the wakeup of transports that
// are backing off. The pulse exists so a stalled transport retries without
// waiting out a long backoff, but it fires for every transport in the same
// instant, so thousands of proxies reconnected together every hour. Each now
// waits a random slice of this spread first. Variable so tests can shorten it.
var pulseSpread = 30 * time.Second

// pulseJitter picks the wait. Replaceable so tests are exact.
var pulseJitter = func(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(mathrand.Int63n(int64(max)))
}

// waitPulseJitter sleeps a random part of pulseSpread. It returns false if ctx
// ended first, in which case the caller should stop.
func waitPulseJitter(ctx context.Context) bool {
	wait := pulseJitter(pulseSpread)
	if wait <= 0 {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
