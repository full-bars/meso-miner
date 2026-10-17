package connect

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DMCA flow-table metrics. All of it is off the per-packet inspect path: the
// flow counters move on flow creation and eviction (once per flow), the
// verdict counters move once when a flow's terminal verdict is published, and
// the scan counters move once per scan tick. The hot touch path adds nothing.

var (
	// dmcaFlowsTracked is the current tracked-flow count summed over every
	// detector and shard (created minus removed).
	dmcaFlowsTracked atomic.Int64
	// dmcaCapacityEvictionsTotal counts evictWithLock removals (the MaxFlows
	// bound).
	dmcaCapacityEvictionsTotal atomic.Int64
	// dmcaIdleReclaimedTotal counts evictIdle removals (the FlowTtl scan).
	dmcaIdleReclaimedTotal atomic.Int64
	// dmcaIdleScanRunsTotal counts scan passes; dmcaIdleScanLastNanos is the
	// last pass's duration (a gauge).
	dmcaIdleScanRunsTotal atomic.Int64
	dmcaIdleScanLastNanos atomic.Int64

	// dmcaVerdictsTotal is [dmcaVerdict][protocol bucket]; the terminal
	// verdict a flow publishes, once per flow.
	dmcaVerdictsTotal [4][3]atomic.Int64
	// dmcaResumedAfterReclaim / dmcaResumedDropped are per protocol bucket:
	// a flow created for a path that was idle-reclaimed inside the window,
	// and how many of those then published a drop verdict.
	dmcaResumedAfterReclaimTotal [3]atomic.Int64
	dmcaResumedDroppedTotal      [3]atomic.Int64
)

// dmcaEstimatedBytesPerFlow is the PR's measured per-flow size; the exported
// bytes gauge is an ESTIMATE, named as one.
const dmcaEstimatedBytesPerFlow = 335

// dmcaRecentReclaimSize bounds the recently-reclaimed ring: 4096 entries
// (about 80 KiB), newest-first over a rolling index. Beyond that many newer
// reclaims, an older entry is gone, so the effective "recent" window is the
// sooner of dmcaRecentReclaimWindow and the newest dmcaRecentReclaimSize
// reclaims.
const dmcaRecentReclaimSize = 4096

// dmcaRecentReclaimWindow is how long after an idle reclaim a re-created flow
// still counts as resumed-after-reclaim.
const dmcaRecentReclaimWindow = 15 * time.Minute

type dmcaRecentReclaim struct {
	hash       uint64
	atUnixNano int64
	proto      uint8
}

var (
	// dmcaRecentReclaimMu guards the ring: the scan goroutine writes it while
	// flow creation on any shard reads it, and the shard locks do not cover
	// both. Writes are one per reclaimed flow and reads are one per new flow,
	// so a plain RWMutex is cheap here and keeps the ring race free.
	dmcaRecentReclaimMu  sync.RWMutex
	dmcaRecentReclaims   [dmcaRecentReclaimSize]dmcaRecentReclaim
	dmcaRecentReclaimSeq atomic.Uint64
)

var dmcaProtocolBuckets = [3]string{"tcp", "udp", "other"}

var dmcaVerdictNames = [4]string{"inspecting", "allow", "encrypted", "bittorrent"}

func dmcaProtocolBucket(protocol IpProtocol) int {
	switch protocol {
	case IpProtocolTcp:
		return 0
	case IpProtocolUdp:
		return 1
	default:
		return 2
	}
}

// dmcaPathHash is an fnv-1a over the path; only the hash is retained.
func dmcaPathHash(key Ip6Path) uint64 {
	const offset64 = uint64(14695981039346656037)
	const prime64 = uint64(1099511628211)
	hash := offset64
	for _, b := range key.SourceIp {
		hash = (hash ^ uint64(b)) * prime64
	}
	for _, b := range key.DestinationIp {
		hash = (hash ^ uint64(b)) * prime64
	}
	hash = (hash ^ uint64(uint32(key.SourcePort))) * prime64
	hash = (hash ^ uint64(uint32(key.DestinationPort))) * prime64
	hash = (hash ^ uint64(key.Protocol)) * prime64
	return hash
}

// noteDmcaRecentReclaim records one idle reclaim in the bounded ring.
func noteDmcaRecentReclaim(key Ip6Path, now time.Time) {
	entry := dmcaRecentReclaim{
		hash:       dmcaPathHash(key),
		atUnixNano: now.UnixNano(),
		proto:      uint8(dmcaProtocolBucket(key.Protocol)),
	}
	seq := dmcaRecentReclaimSeq.Add(1)
	dmcaRecentReclaimMu.Lock()
	dmcaRecentReclaims[(seq-1)%dmcaRecentReclaimSize] = entry
	dmcaRecentReclaimMu.Unlock()
}

// dmcaRecentlyReclaimed reports whether the path was idle-reclaimed inside
// the window, and its protocol bucket. Flow creation is rare, so the bounded
// ring scan is cheap there.
func dmcaRecentlyReclaimed(key Ip6Path, now time.Time) (bool, int) {
	hash := dmcaPathHash(key)
	cutoff := now.Add(-dmcaRecentReclaimWindow).UnixNano()
	dmcaRecentReclaimMu.RLock()
	defer dmcaRecentReclaimMu.RUnlock()
	for i := range dmcaRecentReclaims {
		entry := dmcaRecentReclaims[i]
		if entry.hash == hash && cutoff <= entry.atUnixNano {
			return true, int(entry.proto)
		}
	}
	return false, 0
}

func noteDmcaVerdict(v dmcaVerdict, protocol IpProtocol) {
	if v < 0 || int(v) >= len(dmcaVerdictsTotal) {
		return
	}
	dmcaVerdictsTotal[int(v)][dmcaProtocolBucket(protocol)].Add(1)
}

// writeDmcaMetrics renders the detector metrics (called by the stream metrics
// render).
func writeDmcaMetrics(b *strings.Builder) {
	flows := dmcaFlowsTracked.Load()
	b.WriteString("# HELP urnet_dmca_flows Current tracked dmca flow-table entries, summed over all detectors and shards.\n")
	b.WriteString("# TYPE urnet_dmca_flows gauge\n")
	fmt.Fprintf(b, "urnet_dmca_flows %d\n", flows)
	b.WriteString("# HELP urnet_dmca_flows_estimated_bytes Estimated memory held by the tracked dmca flows at the measured 335 bytes per flow (an estimate, not a measurement).\n")
	b.WriteString("# TYPE urnet_dmca_flows_estimated_bytes gauge\n")
	fmt.Fprintf(b, "urnet_dmca_flows_estimated_bytes %d\n", flows*dmcaEstimatedBytesPerFlow)
	b.WriteString("# HELP urnet_dmca_flows_capacity_evictions_total Flow-table entries dropped by the MaxFlows capacity bound (evictWithLock).\n")
	b.WriteString("# TYPE urnet_dmca_flows_capacity_evictions_total counter\n")
	fmt.Fprintf(b, "urnet_dmca_flows_capacity_evictions_total %d\n", dmcaCapacityEvictionsTotal.Load())
	b.WriteString("# HELP urnet_dmca_flows_idle_reclaimed_total Flow-table entries reclaimed after FlowTtl with no activity in either direction (evictIdle).\n")
	b.WriteString("# TYPE urnet_dmca_flows_idle_reclaimed_total counter\n")
	fmt.Fprintf(b, "urnet_dmca_flows_idle_reclaimed_total %d\n", dmcaIdleReclaimedTotal.Load())
	b.WriteString("# HELP urnet_dmca_idle_scan_runs_total FlowTtl scan passes run across all detectors.\n")
	b.WriteString("# TYPE urnet_dmca_idle_scan_runs_total counter\n")
	fmt.Fprintf(b, "urnet_dmca_idle_scan_runs_total %d\n", dmcaIdleScanRunsTotal.Load())
	b.WriteString("# HELP urnet_dmca_idle_scan_seconds Duration of the last FlowTtl scan pass; a slow pass holds shard locks, so this is the signal to watch.\n")
	b.WriteString("# TYPE urnet_dmca_idle_scan_seconds gauge\n")
	fmt.Fprintf(b, "urnet_dmca_idle_scan_seconds %g\n", float64(dmcaIdleScanLastNanos.Load())/float64(time.Second))
	b.WriteString("# HELP urnet_dmca_verdicts_total Terminal dmca verdicts a flow published, by reason and protocol.\n")
	b.WriteString("# TYPE urnet_dmca_verdicts_total counter\n")
	for v := range dmcaVerdictsTotal {
		for protocol := range dmcaVerdictsTotal[v] {
			fmt.Fprintf(b, "urnet_dmca_verdicts_total{reason=%q,protocol=%q} %d\n",
				dmcaVerdictNames[v], dmcaProtocolBuckets[protocol], dmcaVerdictsTotal[v][protocol].Load())
		}
	}
	b.WriteString("# HELP urnet_dmca_resumed_after_reclaim_total Flows created for a path that was idle-reclaimed inside the recent window (the PR's stated exposure: a resumed silent flow re-inspected from scratch).\n")
	b.WriteString("# TYPE urnet_dmca_resumed_after_reclaim_total counter\n")
	for protocol := range dmcaResumedAfterReclaimTotal {
		fmt.Fprintf(b, "urnet_dmca_resumed_after_reclaim_total{protocol=%q} %d\n",
			dmcaProtocolBuckets[protocol], dmcaResumedAfterReclaimTotal[protocol].Load())
	}
	b.WriteString("# HELP urnet_dmca_resumed_dropped_total Of the resumed-after-reclaim flows, how many then published a drop verdict.\n")
	b.WriteString("# TYPE urnet_dmca_resumed_dropped_total counter\n")
	for protocol := range dmcaResumedDroppedTotal {
		fmt.Fprintf(b, "urnet_dmca_resumed_dropped_total{protocol=%q} %d\n",
			dmcaProtocolBuckets[protocol], dmcaResumedDroppedTotal[protocol].Load())
	}
}
