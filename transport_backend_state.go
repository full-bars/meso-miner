package connect

import (
	"sync"
	"sync/atomic"
	"time"
)

// Fork-only error throttles. The auth and write throttles, and the backend
// degraded state (isBackendDegraded, noteBackendFailure/Success), are owned by
// transport.go now (ported from upstream).
var (
	selectErrThrottle = newLogThrottle(time.Minute)
	flapErrThrottle   = newLogThrottle(time.Minute)
)

// activeProxyConnections counts proxy transports that are currently registered
// with the route manager (i.e. authenticated and live on the platform). It is
// incremented when a H1 transport registers (setRegistered) and decremented when
// it is torn down. ActiveProxyConnections() exposes it for the [health]
// heartbeat as proxies=N.
var activeProxyConnections int64

func ActiveProxyConnections() int64 {
	return atomic.LoadInt64(&activeProxyConnections)
}

// shouldLogSelectErr reports whether a `[net][s]select` error line may be
// emitted now, with the count of lines suppressed since the previous allowed
// one. The second value is only meaningful when the first is true, and reading
// it resets the counter.
func shouldLogSelectErr() (bool, int64) { return selectErrThrottle.Allow(time.Now()) }

// IsBackendDegraded is the exported form for use by the provider binary.
func IsBackendDegraded() bool {
	return isBackendDegraded()
}

// An identity can run several live H1 transports at once: the dual-stack group
// holds a v4 pin and a v6 pin. The health registry and the proxies=N count are
// per identity, not per transport, so an identity counts as up when its first
// transport registers and as down when its last one goes, and a pin dropping
// while its sibling is up is not a transport drop of the identity.
var (
	liveTransportsMu      sync.Mutex
	liveTransportsByProxy = map[int]int{}
)

// noteIdentityTransportUp is called when an H1 transport registers.
func (self *PlatformTransport) noteIdentityTransportUp() {
	idx, ok := self.proxyIndex()
	if !ok {
		idx = -1
	}
	liveTransportsMu.Lock()
	liveTransportsByProxy[idx] += 1
	first := liveTransportsByProxy[idx] == 1
	liveTransportsMu.Unlock()
	if !first {
		return
	}
	atomic.AddInt64(&activeProxyConnections, 1)
	if ok {
		markProxyUp(idx)
	}
}

// noteIdentityTransportDown is the matching call when that H1 transport ends.
func (self *PlatformTransport) noteIdentityTransportDown() {
	idx, ok := self.proxyIndex()
	if !ok {
		idx = -1
	}
	liveTransportsMu.Lock()
	liveTransportsByProxy[idx] -= 1
	last := liveTransportsByProxy[idx] <= 0
	if last {
		delete(liveTransportsByProxy, idx)
	}
	liveTransportsMu.Unlock()
	if !last {
		return
	}
	atomic.AddInt64(&activeProxyConnections, -1)
	if ok {
		markProxyDown(idx)
		RecordProxyTransportDrop(idx, nil)
	}
}
