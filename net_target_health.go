package connect

import (
	"context"
	"math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/urnetwork/glog"
)

// A SOCKS5 proxy dial resolves the target hostname here and sends CONNECT to an
// IPv4 literal. The platform hosts publish several A records, and individual
// addresses fail intermittently: on 2026-10-11 the addresses of the API host
// repeatedly accepted or refused TCP and then never completed TLS, the failing
// set changed within minutes and differed by source network (an address that
// answered from one vantage stalled from another, and vice versa a half hour
// later). Taking the first answer and caching it for dnsCacheTTL for every proxy
// on the node meant one unlucky resolution failed every attempt on the node at
// once. These helpers keep a small per-node health table (a node is one source
// network, which is the right grain) so an address that repeatedly stalls is
// skipped for a few minutes, and pin one address per connect attempt so a failure
// is attributed to the address that was actually dialled.

const (
	// addrFailThreshold failures without an intervening success mark an address
	// bad. Several attempts share an address, so one unlucky proxy cannot do it.
	addrFailThreshold = 3
	// addrFailWindow bounds how old the oldest counted failure may be.
	addrFailWindow = 90 * time.Second
	// addrBadFor is how long a marked address is skipped; a success clears it
	// earlier and the table forgets idle entries.
	addrBadFor = 3 * time.Minute
	// addrHealthMax bounds the table (hostnames are client-controlled).
	addrHealthMax = 2048
	// addrGoodFor is how long an address that last answered stays a fallback
	// candidate. DNS may return a single record per lookup, so when that one is
	// marked bad the only way to a working address is one remembered from before.
	addrGoodFor = 30 * time.Minute
	// addrGoodPerHost bounds the remembered-good set per hostname.
	addrGoodPerHost = 16
	// addrFailMinProxies: the failures that mark an address bad must come from
	// at least this many different proxies. A free proxy that accepts the CONNECT
	// and then stalls is routine; one proxy's stalls say nothing about the address.
	addrFailMinProxies = 3
	// addrSiblingFresh: and another address of the same host must have answered
	// within this long. If nothing answers anywhere the proxies, or the platform,
	// are the problem, and skipping addresses would only concentrate the load.
	addrSiblingFresh = 2 * time.Minute
)

// targetHealthEnabled is the kill switch: URNETWORK_TARGET_HEALTH=off restores
// the previous behaviour (the first resolved address, no health table).
var targetHealthEnabled = os.Getenv("URNETWORK_TARGET_HEALTH") != "off"

type addrState struct {
	// proxies are the distinct proxies behind the counted failures
	proxies   map[int]struct{}
	fails     int
	firstFail time.Time
	badUntil  time.Time
	touched   time.Time
}

var addrHealth struct {
	mu   sync.Mutex
	m    map[string]*addrState           // "host|ip"
	good map[string]map[string]time.Time // host -> ip -> last time it answered
}

func addrKey(host, ip string) string { return host + "|" + ip }

// addrIsBad reports whether ip is currently skipped for host.
func addrIsBad(host, ip string, now time.Time) bool {
	addrHealth.mu.Lock()
	defer addrHealth.mu.Unlock()
	s := addrHealth.m[addrKey(host, ip)]
	return s != nil && now.Before(s.badUntil)
}

// noteAddrOutcome records the result of an attempt that used ip for host.
// ok clears the address; a failure counts toward marking it bad.
func noteAddrOutcome(host, ip string, proxyId int, ok bool, now time.Time) {
	if !targetHealthEnabled {
		return
	}
	addrHealth.mu.Lock()
	defer addrHealth.mu.Unlock()
	if addrHealth.m == nil {
		addrHealth.m = map[string]*addrState{}
	}
	k := addrKey(host, ip)
	s := addrHealth.m[k]
	if ok {
		if s != nil && now.Before(s.badUntil) {
			glog.Infof("[net][target] %s %s answered again, clearing", host, ip)
		}
		delete(addrHealth.m, k)
		rememberGoodLocked(host, ip, now)
		return
	}
	if s == nil {
		if len(addrHealth.m) >= addrHealthMax {
			pruneAddrHealthLocked(now)
			if len(addrHealth.m) >= addrHealthMax {
				return
			}
		}
		s = &addrState{}
		addrHealth.m[k] = s
	}
	s.touched = now
	if s.fails == 0 || addrFailWindow < now.Sub(s.firstFail) {
		s.fails, s.firstFail = 0, now
		s.proxies = map[int]struct{}{}
	}
	s.fails++
	if len(s.proxies) < 8 {
		s.proxies[proxyId] = struct{}{}
	}
	if addrFailThreshold <= s.fails && addrFailMinProxies <= len(s.proxies) &&
		siblingAnsweredLocked(host, ip, now) && !now.Before(s.badUntil) {
		s.badUntil = now.Add(addrBadFor)
		glog.Infof("[net][target] %s %s stalled %d times in %s, skipping it for %s", host, ip, s.fails, now.Sub(s.firstFail).Round(time.Second), addrBadFor)
	}
}

// siblingAnsweredLocked reports whether another address of host answered
// recently: the evidence that this address, not everything, is what is failing.
func siblingAnsweredLocked(host, ip string, now time.Time) bool {
	for other, t := range addrHealth.good[host] {
		if other != ip && now.Sub(t) <= addrSiblingFresh {
			return true
		}
	}
	return false
}

func rememberGoodLocked(host, ip string, now time.Time) {
	if addrHealth.good == nil {
		addrHealth.good = map[string]map[string]time.Time{}
	}
	g := addrHealth.good[host]
	if g == nil {
		if len(addrHealth.good) >= addrHealthMax {
			return
		}
		g = map[string]time.Time{}
		addrHealth.good[host] = g
	}
	g[ip] = now
	for len(g) > addrGoodPerHost {
		oldest, oldestT := "", now
		for k, t := range g {
			if !t.After(oldestT) {
				oldest, oldestT = k, t
			}
		}
		delete(g, oldest)
	}
}

// knownGood returns addresses of host that answered within addrGoodFor and are
// not currently marked bad.
func knownGood(host string, now time.Time) []string {
	addrHealth.mu.Lock()
	defer addrHealth.mu.Unlock()
	var out []string
	for ip, t := range addrHealth.good[host] {
		if addrGoodFor < now.Sub(t) {
			delete(addrHealth.good[host], ip)
			continue
		}
		if st := addrHealth.m[addrKey(host, ip)]; st != nil && now.Before(st.badUntil) {
			continue
		}
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func pruneAddrHealthLocked(now time.Time) {
	for k, s := range addrHealth.m {
		if !now.Before(s.badUntil) && addrFailWindow < now.Sub(s.touched) {
			delete(addrHealth.m, k)
		}
	}
}

// chooseTargetAddr picks one address of host. It prefers addresses that are not
// marked bad and chooses at random among them so load spreads over the edge
// instead of always hitting the first record. If every address is marked it
// chooses among all: blocking would be worse than a bad guess. The choice is
// pinned for the lifetime of one connect attempt (taken from ctx) so all of that
// attempt's dials use, and report on, the same address.
func chooseTargetAddr(ctx context.Context, host string, ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	if !targetHealthEnabled {
		return ips[0]
	}
	stats := attemptStatsFrom(ctx)
	now := time.Now()
	if stats != nil {
		if pinned, ok := stats.pinnedTarget(host); ok {
			for _, ip := range ips {
				if ip == pinned {
					return pinned
				}
			}
			// a pin taken from the remembered-good set is still valid
			if !addrIsBad(host, pinned, now) {
				for _, ip := range knownGood(host, now) {
					if ip == pinned {
						return pinned
					}
				}
			}
		}
	}
	healthy := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !addrIsBad(host, ip, now) {
			healthy = append(healthy, ip)
		}
	}
	pool := healthy
	if len(pool) == 0 {
		// every fresh answer is marked bad: prefer an address that answered
		// recently over retrying one that is known to stall
		pool = knownGood(host, now)
	}
	if len(pool) == 0 {
		pool = ips
	}
	pick := pool[0]
	if 1 < len(pool) {
		pick = pool[rand.IntN(len(pool))]
	}
	if stats != nil {
		stats.pinTarget(host, pick)
	}
	return pick
}

// attemptTargets is the per-attempt record of which address each host resolved
// to. Hung off attemptStats.
type attemptTargets struct {
	mu        sync.Mutex
	pinned    map[string]string
	lastHost  string
	lastProxy int
}

func (self *attemptStats) pinnedTarget(host string) (string, bool) {
	self.targets.mu.Lock()
	defer self.targets.mu.Unlock()
	ip, ok := self.targets.pinned[host]
	return ip, ok
}

func (self *attemptStats) pinTarget(host string, ip string) {
	self.targets.mu.Lock()
	defer self.targets.mu.Unlock()
	if self.targets.pinned == nil {
		self.targets.pinned = map[string]string{}
	}
	self.targets.pinned[host] = ip
}

// noteDialedTarget marks host as the most recent target a dial got through the
// proxy to (SOCKS CONNECT succeeded). A later failure at the TLS or upgrade layer
// is then evidence about that address, not about the proxy.
func (self *attemptStats) noteDialedTarget(host string, proxyId int) {
	self.targets.mu.Lock()
	defer self.targets.mu.Unlock()
	self.targets.lastHost = host
	self.targets.lastProxy = proxyId
}

// recordTargetOutcome turns a finished attempt's class into address health.
// Only outcomes that are evidence about the address count: a stall after the
// proxy connected (no reply, or a failed TLS handshake) is a failure; a
// connected attempt clears. Proxy-level failures, rejections, upgrades and
// cancels say nothing about the address.
func (self *attemptStats) recordTargetOutcome(class connectClass) {
	if self == nil {
		return
	}
	self.targets.mu.Lock()
	last := self.targets.lastHost
	lastIp := self.targets.pinned[last]
	lastProxy := self.targets.lastProxy
	pinned := make(map[string]string, len(self.targets.pinned))
	for h, ip := range self.targets.pinned {
		pinned[h] = ip
	}
	self.targets.mu.Unlock()
	now := time.Now()
	switch class {
	case connectOK:
		for h, ip := range pinned {
			noteAddrOutcome(h, ip, 0, true, now)
		}
	case connectTimeoutNoReply, connectTlsFail:
		if last != "" && lastIp != "" {
			noteAddrOutcome(last, lastIp, lastProxy, false, now)
		}
	}
}
