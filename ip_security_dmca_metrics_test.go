package connect

import (
	"sync"
	"testing"
	"time"
)

func resetDmcaMetricsForTest() {
	dmcaFlowsTracked.Store(0)
	dmcaCapacityEvictionsTotal.Store(0)
	dmcaIdleReclaimedTotal.Store(0)
	dmcaIdleScanRunsTotal.Store(0)
	dmcaIdleScanLastNanos.Store(0)
	for v := range dmcaVerdictsTotal {
		for p := range dmcaVerdictsTotal[v] {
			dmcaVerdictsTotal[v][p].Store(0)
		}
	}
	for p := range dmcaResumedAfterReclaimTotal {
		dmcaResumedAfterReclaimTotal[p].Store(0)
	}
	for p := range dmcaResumedDroppedTotal {
		dmcaResumedDroppedTotal[p].Store(0)
	}
	for i := range dmcaRecentReclaims {
		dmcaRecentReclaims[i] = dmcaRecentReclaim{}
	}
}

func dmcaFlowStateForTest(t *testing.T, detector *dmcaDetector, path *IpPath) *dmcaFlowState {
	t.Helper()
	key := path.ToIp6Path()
	shard := detector.shards[dmcaShardIndex(key)]
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	st := shard.flows[key]
	if st == nil {
		t.Fatalf("flow %v is not tracked", key)
	}
	return st
}

func TestDmcaFlowGaugeTracksCreateEvictAndResume(t *testing.T) {
	resetDmcaMetricsForTest()
	detector := newIdleDmcaDetector(nil, 10*time.Minute)
	for port := 7000; port < 7005; port += 1 {
		detector.classify(dmcaIdleTestPath(port), nil)
	}
	if got := dmcaFlowsTracked.Load(); got != 5 {
		t.Fatalf("gauge after 5 creates = %d, want 5", got)
	}
	reclaimed := dmcaIdleTestPath(7000)
	ageFlow(t, detector, reclaimed.ToIp6Path(), 11*time.Minute)
	detector.evictIdle(time.Now())
	if got := dmcaFlowsTracked.Load(); got != 4 {
		t.Fatalf("gauge after the reclaim = %d, want 4", got)
	}
	if got := dmcaIdleReclaimedTotal.Load(); got != 1 {
		t.Fatalf("idle reclaims = %d, want 1", got)
	}

	// a re-created path inside the window counts as resumed, and its drop
	// verdict counts as resumed-and-dropped, by protocol
	detector.classify(reclaimed, nil)
	if got := dmcaResumedAfterReclaimTotal[1].Load(); got != 1 {
		t.Fatalf("resumed-after-reclaim{udp} = %d, want 1", got)
	}
	st := dmcaFlowStateForTest(t, detector, reclaimed)
	st.mu.Lock()
	st.setTerminal(dmcaDropEncrypted, SecurityPolicyReasonDropEncrypted)
	st.mu.Unlock()
	if got := dmcaResumedDroppedTotal[1].Load(); got != 1 {
		t.Fatalf("resumed-dropped{udp} = %d, want 1", got)
	}
	if got := dmcaVerdictsTotal[int(dmcaDropEncrypted)][1].Load(); got != 1 {
		t.Fatalf("verdicts{encrypted,udp} = %d, want 1", got)
	}
}

func TestDmcaCapacityEvictionCounts(t *testing.T) {
	resetDmcaMetricsForTest()
	settings := DefaultDmcaSecurityPolicySettings()
	settings.FlowTtl = 10 * time.Minute
	settings.MaxFlows = 4
	detector := newDmcaDetector(nil, settings, newWebStandardDetector(DefaultWebStandardSettings()))
	for port := 7100; port < 7150; port += 1 {
		detector.classify(dmcaIdleTestPath(port), nil)
	}
	if got := dmcaCapacityEvictionsTotal.Load(); got == 0 {
		t.Fatal("capacity evictions never counted under a tiny MaxFlows")
	}
}

func TestDmcaScanOnceCountsAndTimes(t *testing.T) {
	resetDmcaMetricsForTest()
	detector := newIdleDmcaDetector(nil, 10*time.Minute)
	detector.classify(dmcaIdleTestPath(7200), nil)
	detector.scanOnce()
	detector.scanOnce()
	if got := dmcaIdleScanRunsTotal.Load(); got != 2 {
		t.Fatalf("scan runs = %d, want 2", got)
	}
	if dmcaIdleScanLastNanos.Load() < 0 {
		t.Fatal("last scan duration must be >= 0")
	}
}

func TestDmcaVerdictCountersByProtocol(t *testing.T) {
	resetDmcaMetricsForTest()
	detector := newIdleDmcaDetector(nil, 10*time.Minute)
	udpPath := dmcaIdleTestPath(7300)
	tcpPath := dmcaIdleTestPath(7301)
	tcpPath.Protocol = IpProtocolTcp
	detector.classify(udpPath, nil)
	detector.classify(tcpPath, nil)
	for _, path := range []*IpPath{udpPath, tcpPath} {
		st := dmcaFlowStateForTest(t, detector, path)
		st.mu.Lock()
		st.setTerminal(dmcaAllow, SecurityPolicyReasonAllowPlaintext)
		st.mu.Unlock()
	}
	if got := dmcaVerdictsTotal[int(dmcaAllow)][1].Load(); got != 1 {
		t.Fatalf("verdicts{allow,udp} = %d, want 1", got)
	}
	if got := dmcaVerdictsTotal[int(dmcaAllow)][0].Load(); got != 1 {
		t.Fatalf("verdicts{allow,tcp} = %d, want 1", got)
	}
}

func TestDmcaRecentReclaimRingBound(t *testing.T) {
	resetDmcaMetricsForTest()
	now := time.Now()
	oldest := dmcaIdleTestPath(7400)
	noteDmcaRecentReclaim(oldest.ToIp6Path(), now)
	for i := 0; i < dmcaRecentReclaimSize+100; i += 1 {
		noteDmcaRecentReclaim(dmcaIdleTestPath(8000+i).ToIp6Path(), now)
	}
	if found, _ := dmcaRecentlyReclaimed(oldest.ToIp6Path(), now); found {
		t.Fatal("an entry older than the ring capacity must rotate out")
	}
	newest := dmcaIdleTestPath(8000 + dmcaRecentReclaimSize + 99)
	if found, protocol := dmcaRecentlyReclaimed(newest.ToIp6Path(), now); !found || protocol != 1 {
		t.Fatalf("the newest entry must be found: found=%v protocol=%d", found, protocol)
	}
	// outside the window it no longer counts
	later := now.Add(dmcaRecentReclaimWindow + time.Second)
	if found, _ := dmcaRecentlyReclaimed(newest.ToIp6Path(), later); found {
		t.Fatal("an entry outside the window must not count as recent")
	}
}

func TestDmcaMetricsConcurrent(t *testing.T) {
	resetDmcaMetricsForTest()
	detector := newIdleDmcaDetector(nil, 10*time.Minute)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < 4; worker += 1 {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for i := 0; i < 200; i += 1 {
				detector.classify(dmcaIdleTestPath(9000+worker*300+i), nil)
			}
		}(worker)
	}
	waitGroup.Wait()
	// conservation: every created flow is either still tracked or was
	// capacity-evicted (the default MaxFlows caps a shard under this load)
	created := int64(800)
	gauge := dmcaFlowsTracked.Load()
	evicted := dmcaCapacityEvictionsTotal.Load()
	reclaimed := dmcaIdleReclaimedTotal.Load()
	if gauge+evicted+reclaimed != created {
		t.Fatalf("flow accounting under concurrency: gauge %d + evicted %d + reclaimed %d = %d, want %d",
			gauge, evicted, reclaimed, gauge+evicted+reclaimed, created)
	}
}

// the scan goroutine writes the ring while flow creation reads it; under -race
// this fails if the ring is touched without its lock.
func TestDmcaRecentReclaimRingIsRaceFree(t *testing.T) {
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i += 1 {
			noteDmcaRecentReclaim(dmcaIdleTestPath(20000+i).ToIp6Path(), now)
		}
	}()
	for i := 0; i < 2000; i += 1 {
		dmcaRecentlyReclaimed(dmcaIdleTestPath(20000+i).ToIp6Path(), now)
	}
	<-done
}
