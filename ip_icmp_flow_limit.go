package connect

import "sync"

// icmpFlowTracker shares the icmp flow caps across every per-shard,
// per-family icmp buffer of one local user nat (see ICMP.md).
//
// The configured caps are nat-wide ceilings: the fd, goroutine and memory
// intent. Each shard builds its own buffer pair, so per-buffer enforcement
// multiplied them by the shard and family count — a client could open about
// 2048 sockets against a configured 64 per source and 128 overall. One
// tracker per nat enforces the caps across all of its buffers. Standalone
// buffers (nil tracker) keep the per-buffer enforcement.
type icmpFlowTracker struct {
	mutex     sync.Mutex
	total     int
	perSource map[TransferPath]int
	order     map[*IcmpSequence]uint64
	nextOrder uint64
}

func newIcmpFlowTracker() *icmpFlowTracker {
	return &icmpFlowTracker{
		perSource: map[TransferPath]int{},
		order:     map[*IcmpSequence]uint64{},
	}
}

// register admits a new flow, first evicting this nat's least recently active
// flows (across every buffer sharing this tracker) until the caps admit it.
// Evictions are canceled after the tracker lock is released, so a victim's
// own exit path can settle its counts. A caller that holds no limit always
// admits.
func (self *icmpFlowTracker) register(sequence *IcmpSequence, settings *IcmpBufferSettings) {
	self.mutex.Lock()
	victims := self.evictFor(sequence.source, settings)
	self.perSource[sequence.source] += 1
	self.total += 1
	self.nextOrder += 1
	self.order[sequence] = self.nextOrder
	// registered flows release through their own Cancel/Close
	sequence.flowTracker = self
	self.mutex.Unlock()

	for _, victim := range victims {
		victim.Cancel()
	}
}

// evictFor frees room for one new flow of `source`: oldest flows of that
// source until the per-source cap admits, then oldest flows anywhere until
// the global cap admits. Caller holds the tracker mutex.
func (self *icmpFlowTracker) evictFor(source TransferPath, settings *IcmpBufferSettings) []*IcmpSequence {
	var victims []*IcmpSequence
	evicted := map[*IcmpSequence]struct{}{}

	// least recently active first, matching the standalone buffers; the
	// registration order breaks activity-time ties so eviction is
	// deterministic (coarse platform clocks can tie)
	pick := func(match func(*IcmpSequence) bool) *IcmpSequence {
		var oldest *IcmpSequence
		var oldestTime int64
		for sequence := range self.order {
			if _, ok := evicted[sequence]; ok {
				continue
			}
			if match != nil && !match(sequence) {
				continue
			}
			at := sequence.LastActivityTime().UnixNano()
			if oldest == nil || at < oldestTime || (at == oldestTime && self.order[sequence] < self.order[oldest]) {
				oldest = sequence
				oldestTime = at
			}
		}
		return oldest
	}
	drop := func(sequence *IcmpSequence) {
		evicted[sequence] = struct{}{}
		delete(self.order, sequence)
		self.perSource[sequence.source] -= 1
		if 0 == self.perSource[sequence.source] {
			delete(self.perSource, sequence.source)
		}
		self.total -= 1
		victims = append(victims, sequence)
	}

	for 0 < settings.UserLimit && settings.UserLimit <= self.perSource[source] {
		victim := pick(func(sequence *IcmpSequence) bool { return sequence.source == source })
		if victim == nil {
			break
		}
		drop(victim)
	}
	for 0 < settings.GlobalLimit && settings.GlobalLimit <= self.total {
		victim := pick(nil)
		if victim == nil {
			break
		}
		drop(victim)
	}
	return victims
}

// release drops a closed flow from the shared counts. Idempotent: a flow can
// be canceled by its own exit path, by a source cancel or by an eviction, in
// any order, and only the first release owns the decrement.
func (self *icmpFlowTracker) release(sequence *IcmpSequence) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.order[sequence]; !ok {
		return
	}
	delete(self.order, sequence)
	self.perSource[sequence.source] -= 1
	if 0 == self.perSource[sequence.source] {
		delete(self.perSource, sequence.source)
	}
	self.total -= 1
}
