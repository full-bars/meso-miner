package connect

import "testing"

func TestProxyScoutScore_RanksByEarningHistory(t *testing.T) {
	ResetProxyHealthForTesting()
	defer SetProxyScoutScorer(nil)

	RegisterProxy(1, "10.0.0.1:1080", "10.0.0.1:1080") // earned a lot
	markProxyUp(1)
	RegisterProxyBandwidth(1).BillableTx.Store(5_000_000)

	RegisterProxy(2, "10.0.0.2:1080", "10.0.0.2:1080") // connected, earned little
	markProxyUp(2)
	RegisterProxyBandwidth(2).BillableTx.Store(1_000)

	RegisterProxy(3, "10.0.0.3:1080", "10.0.0.3:1080") // never connected

	best, mid, none := proxyScoutScore(1), proxyScoutScore(2), proxyScoutScore(3)
	if !(best > mid && mid > none) {
		t.Fatalf("scores must rank earning history: best=%d mid=%d none=%d", best, mid, none)
	}
	if none != 0 {
		t.Fatalf("a proxy that never connected has no record, got %d", none)
	}
	if proxyScoutScore(404) != 0 {
		t.Fatal("an unknown proxy scores zero")
	}
}

func TestProxyScoutScore_ProviderCanSupplyItsOwnRecord(t *testing.T) {
	defer SetProxyScoutScorer(nil)
	SetProxyScoutScorer(func(index int) uint64 { return uint64(index) * 10 })
	if got := proxyScoutScore(7); got != 70 {
		t.Fatalf("the provider's scorer should win, got %d", got)
	}
	SetProxyScoutScorer(nil)
	ResetProxyHealthForTesting()
	if got := proxyScoutScore(7); got != 0 {
		t.Fatalf("clearing the scorer restores the default, got %d", got)
	}
}
