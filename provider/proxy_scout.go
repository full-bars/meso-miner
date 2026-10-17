package main

import (
	"sync"
	"time"

	"github.com/urnetwork/connect"
)

// Recovery scouts. While the reconnect attempt budget is throttled only a few
// proxies may try to connect each second, so which ones matters. They are ranked
// by what the provider knows and the connect package does not: how much each
// proxy has earned, and where it came from.

// scoutSources maps a proxy identity key to its provenance ("file", "internal"
// or "url"). It is fed by tagProxySourceIfUnset, which both the startup launch
// and hot reload already call for every proxy.
var scoutSources sync.Map

func registerScoutSource(key, source string) {
	if source == "" {
		return
	}
	scoutSources.Store(key, source)
	// the connect package labels its transport churn counters by the same source
	connect.SetProxySource(key, source)
}

func scoutSourceOf(key string) string {
	v, _ := scoutSources.Load(key)
	s, _ := v.(string)
	return s
}

func resetScoutSourcesForTest() {
	scoutSources.Range(func(k, _ any) bool {
		scoutSources.Delete(k)
		return true
	})
}

// scoutFileBonus is the head start a paid, file-listed or operator-configured
// proxy has over one scraped from a URL, in earned-bytes terms. URL lists are
// far likelier to be full of dead proxies. It equals the promotion threshold on
// purpose: a URL proxy that has actually moved that much billable traffic
// ranks level with a cold file proxy, and past it ranks ahead, the same rule
// that orders proxies at launch.
const scoutFileBonus = uint64(earningsPromotionBytes)

// scoutScoreForIndex ranks the proxy at a registry index as a recovery scout:
// its decayed earnings, plus the head start for file-sourced proxies. Unknown
// proxies score zero.
func scoutScoreForIndex(index int) uint64 {
	key := connect.ProxyKeyByIndex(index)
	if key == "" {
		return 0
	}
	score := uint64(max(0, proxyEarningsScore(key, time.Now())))
	switch scoutSourceOf(key) {
	case "file", "internal":
		score += scoutFileBonus
	}
	return score
}

// installScoutScorer hands the ranking to the connect package.
func installScoutScorer() {
	connect.SetProxyScoutScorer(scoutScoreForIndex)
}
