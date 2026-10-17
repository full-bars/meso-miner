package connect

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// fork: the icmp echo path (upstream e05ecee0b) refers to three symbols that
// upstream keeps in its own ip.go. Our ip.go does not carry that refactor, so
// they are declared here instead. Nothing else of ours changes for this file.
const IpProtocolIcmp IpProtocol = 3

type receivePacketsBatchFunction func(source TransferPath, provideMode protocol.ProvideMode, ipPath *IpPath, packets [][]byte) (batched bool)

// fork: the nat builds one icmp buffer pair per shard and hands the pair one
// shared flow tracker, so the configured caps are nat-wide instead of per
// shard and family (see icmpFlowTracker). The upstream constructors stay for
// standalone buffers, which enforce the caps per buffer.
func newIcmp4BufferShared(
	ctx context.Context,
	receiveCallback ReceivePacketFunction,
	icmpBufferSettings *IcmpBufferSettings,
	flowTracker *icmpFlowTracker,
) *Icmp4Buffer {
	return &Icmp4Buffer{
		IcmpBuffer: *newIcmpBuffer[BufferId4](ctx, receiveCallback, icmpBufferSettings, flowTracker),
	}
}

func newIcmp6BufferShared(
	ctx context.Context,
	receiveCallback ReceivePacketFunction,
	icmpBufferSettings *IcmpBufferSettings,
	flowTracker *icmpFlowTracker,
) *Icmp6Buffer {
	return &Icmp6Buffer{
		IcmpBuffer: *newIcmpBuffer[BufferId6](ctx, receiveCallback, icmpBufferSettings, flowTracker),
	}
}

// CancelSource cancels every flow of one source client and returns how many.
// It only cancels: each sequence's own exit path removes it from the maps
// (see the udp and tcp counterparts). A nat registers this with its source
// cancel list so one client's echo flows are released with its udp and tcp.
func (self *IcmpBuffer[BufferId]) CancelSource(sourceId Id) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for source, sequences := range self.sourceSequences {
		if source.SourceId != sourceId {
			continue
		}
		for _, sequence := range sequences {
			sequence.Cancel()
			count += 1
		}
	}
	return count
}

const lruEvictionSampleSize = 32

// fork: the egress backend factory, overridable by tests to exercise open
// and write failures without a real socket (the egress contract itself, see
// icmpEgress)
//
// It is read by every flow's goroutine and replaced by tests, so it sits behind
// an atomic pointer: a flow started by one test can still be running when the
// next test installs its own factory.
type icmpEgressFactoryFunc func(ctx context.Context, ipVersion int, destinationIp net.IP, settings *IcmpBufferSettings) (icmpEgress, error)

var icmpEgressFactory atomic.Pointer[icmpEgressFactoryFunc]

func init() {
	setIcmpEgressFactory(newIcmpEgress)
}

// currentIcmpEgressFactory is the factory a new flow opens its egress with.
func currentIcmpEgressFactory() icmpEgressFactoryFunc {
	return *icmpEgressFactory.Load()
}

// setIcmpEgressFactory installs a factory and returns the previous one.
func setIcmpEgressFactory(factory icmpEgressFactoryFunc) icmpEgressFactoryFunc {
	var previous icmpEgressFactoryFunc
	if old := icmpEgressFactory.Swap(&factory); old != nil {
		previous = *old
	}
	return previous
}

// applyLruMapLimit bounds eviction work independently of table cardinality.
// Go map iteration starts at a pseudo-random bucket, so choosing the oldest of
// the first fixed-size sample is an approximate LRU without collecting and
// sorting the entire flow table under its dispatch lock. limitCallback must
// remove an accepted resource from resources; it owns any cancellation.
func applyLruMapLimit[K comparable, R UserLimited](
	resources map[K]R,
	limit int,
	limitCallback func(K, R) bool,
) {
	for limit < len(resources) {
		var oldestKey K
		var oldest R
		var oldestTime time.Time
		found := false
		sampled := 0
		for key, resource := range resources {
			activityTime := resource.LastActivityTime()
			if !found || activityTime.Before(oldestTime) {
				oldestKey = key
				oldest = resource
				oldestTime = activityTime
				found = true
			}
			sampled += 1
			if lruEvictionSampleSize <= sampled {
				break
			}
		}
		if !found || !limitCallback(oldestKey, oldest) {
			return
		}
	}
}
