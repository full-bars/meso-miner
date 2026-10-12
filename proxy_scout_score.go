package connect

import "sync/atomic"

// proxyScoutScorer, when set, replaces the default scout ranking. The provider
// sets it so scouts are ranked by what it knows and the connect package does
// not: earnings history and where each proxy came from (a paid or file proxy is
// far likelier to work than one scraped from a URL).
var proxyScoutScorer atomic.Pointer[func(index int) uint64]

// SetProxyScoutScorer installs the scout ranking. A nil fn restores the default.
func SetProxyScoutScorer(fn func(index int) uint64) {
	if fn == nil {
		proxyScoutScorer.Store(nil)
		return
	}
	proxyScoutScorer.Store(&fn)
}

// proxyScoutScore ranks a proxy as a recovery scout while the attempt budget is
// throttled: higher goes first. Unknown and never-connected proxies score zero.
func proxyScoutScore(index int) uint64 {
	if fn := proxyScoutScorer.Load(); fn != nil {
		return (*fn)(index)
	}
	return defaultProxyScoutScore(index)
}

// defaultProxyScoutScore is the proxy's lifetime billable traffic, with one
// extra point for having connected at all so any proxy with a record outranks
// one with none. Billable bytes are earnings, so the proxies that paid before
// the outage come back first.
func defaultProxyScoutScore(index int) uint64 {
	proxyHealthMu.Lock()
	defer proxyHealthMu.Unlock()
	h, ok := proxyHealthByIndex[index]
	if !ok || !h.everUp {
		return 0
	}
	score := uint64(1)
	if h.bw != nil {
		score += h.bw.BillableRx.Load() + h.bw.BillableTx.Load()
	}
	return score
}
