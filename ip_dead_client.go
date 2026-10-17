package connect

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The dead-client sweeper.
//
// Nothing tells a provider that a client it is answering has gone away. Returns
// to that client are retried and held until their own timers expire, a new
// sequence opens for the next packet, and the client's NAT flows (a TCP window
// that is never read, a UDP flow that nobody drains) stay up. This sweeper reads
// the per-destination ack yield (transfer_dest_yield.go) and, when a destination
// we are still writing to has acknowledged nothing for deadClientPolicy.Silence
// while the rest of the fleet is acknowledging, releases it: its send sequences
// are cancelled, which fails the bytes held for it, and its NAT flows are
// cancelled. A client that was only slow and comes back simply opens fresh
// flows.
//
// One sweeper serves the whole process. A provider runs one client per proxy, and
// each sees only its own few clients, so the fleet is judged across all of them
// together: a platform outage shows as collapse process-wide, not as many small
// fleets each too small to judge.
//
// URNETWORK_DEAD_CLIENT decides what happens: shadow (the default) counts and
// logs what it would release, enforce releases, off does nothing.

const deadClientSweepInterval = 15 * time.Second

// deadClientShadowLogEvery limits how often one destination is named in shadow.
const deadClientShadowLogEvery = 5 * time.Minute

var deadClientModeValue atomic.Int32

func init() {
	deadClientModeValue.Store(int32(parseDeadClientMode(os.Getenv("URNETWORK_DEAD_CLIENT"))))
}

// parseDeadClientMode reads URNETWORK_DEAD_CLIENT. Unset, empty and unrecognised
// values mean shadow: a new release rule observes before it acts. Only an
// explicit enforce releases and only an explicit off stops observing.
func parseDeadClientMode(raw string) governMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enforce", "on", "1", "true", "yes":
		return governEnforce
	case "off", "0", "false", "no":
		return governOff
	}
	return governShadow
}

func currentDeadClientMode() governMode { return governMode(deadClientModeValue.Load()) }

func setDeadClientModeForTest(mode governMode) func() {
	prev := deadClientModeValue.Swap(int32(mode))
	return func() { deadClientModeValue.Store(prev) }
}

var deadClientCounters struct {
	released          atomic.Int64
	releasedFlows     atomic.Int64
	releasedSequences atomic.Int64
	wouldRelease      atomic.Int64
	sweeps            atomic.Int64
	silent            atomic.Int64
	fleetCollapsed    atomic.Int64
	active            atomic.Int64
	acking            atomic.Int64
	records           atomic.Int64
	byReasonHealthy   atomic.Int64
	byReasonHard      atomic.Int64
	byReasonSmall     atomic.Int64
}

// DeadClientStats is a snapshot of the dead-client counters.
type DeadClientStats struct {
	// Released is destinations released, with the NAT flows and send sequences
	// cancelled for them. WouldRelease is the same count in shadow mode.
	Released          int64
	ReleasedFlows     int64
	ReleasedSequences int64
	WouldRelease      int64
	Sweeps            int64
	// Silent is the destinations still being written to that were past the
	// silence window at the latest sweep, released or held. FleetCollapsed is 1
	// when that sweep judged the platform, not the clients, at fault.
	Silent         int64
	FleetCollapsed int64
	// Active and Acking are the fleet sample the latest sweep judged, and
	// Records is how many destination records the process holds.
	Active  int64
	Acking  int64
	Records int64
	// ByReason counts releases and would-be releases by why they were allowed.
	ByReasonHealthy int64
	ByReasonHard    int64
	ByReasonSmall   int64
}

func DeadClientStatsNow() DeadClientStats {
	return DeadClientStats{
		Released:          deadClientCounters.released.Load(),
		ReleasedFlows:     deadClientCounters.releasedFlows.Load(),
		ReleasedSequences: deadClientCounters.releasedSequences.Load(),
		WouldRelease:      deadClientCounters.wouldRelease.Load(),
		Sweeps:            deadClientCounters.sweeps.Load(),
		Silent:            deadClientCounters.silent.Load(),
		FleetCollapsed:    deadClientCounters.fleetCollapsed.Load(),
		Active:            deadClientCounters.active.Load(),
		Acking:            deadClientCounters.acking.Load(),
		Records:           deadClientCounters.records.Load(),
		ByReasonHealthy:   deadClientCounters.byReasonHealthy.Load(),
		ByReasonHard:      deadClientCounters.byReasonHard.Load(),
		ByReasonSmall:     deadClientCounters.byReasonSmall.Load(),
	}
}

// the providers one process-wide sweeper serves
var deadClientProviders struct {
	mutex     sync.Mutex
	providers map[*RemoteUserNatProvider]bool
	once      sync.Once
}

func registerDeadClientProvider(provider *RemoteUserNatProvider) {
	deadClientProviders.mutex.Lock()
	if deadClientProviders.providers == nil {
		deadClientProviders.providers = map[*RemoteUserNatProvider]bool{}
	}
	deadClientProviders.providers[provider] = true
	deadClientProviders.mutex.Unlock()

	deadClientProviders.once.Do(func() {
		go HandleError(newDeadClientSweeper(registeredDeadClientProviders).run)
	})
	go func() {
		<-provider.client.ctx.Done()
		unregisterDeadClientProvider(provider)
	}()
}

func unregisterDeadClientProvider(provider *RemoteUserNatProvider) {
	deadClientProviders.mutex.Lock()
	defer deadClientProviders.mutex.Unlock()
	delete(deadClientProviders.providers, provider)
}

func registeredDeadClientProviders() []*RemoteUserNatProvider {
	deadClientProviders.mutex.Lock()
	defer deadClientProviders.mutex.Unlock()
	providers := make([]*RemoteUserNatProvider, 0, len(deadClientProviders.providers))
	for provider := range deadClientProviders.providers {
		providers = append(providers, provider)
	}
	return providers
}

type deadClientSweeper struct {
	providers func() []*RemoteUserNatProvider
	policy    deadClientPolicy
	now       func() time.Time
	emit      func(string)

	mutex       sync.Mutex
	shadowLogAt map[Id]time.Time
}

func newDeadClientSweeper(providers func() []*RemoteUserNatProvider) *deadClientSweeper {
	return &deadClientSweeper{
		providers:   providers,
		policy:      defaultDeadClientPolicy(),
		now:         time.Now,
		emit:        emitGovernLine,
		shadowLogAt: map[Id]time.Time{},
	}
}

func (self *deadClientSweeper) run() {
	ticker := time.NewTicker(deadClientSweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		self.sweep()
	}
}

// sweep reads every provider's yields once, judges them together, and acts on
// the decision. It reports how many destinations it released.
func (self *deadClientSweeper) sweep() int {
	mode := currentDeadClientMode()
	deadClientCounters.sweeps.Add(1)
	now := self.now()

	providers := self.providers()
	var views []destinationYieldView
	owners := map[*SendBuffer]*RemoteUserNatProvider{}
	for _, provider := range providers {
		sendBuffer := provider.client.sendBuffer
		owners[sendBuffer] = provider
		for _, view := range sendBuffer.DestinationYields(now) {
			view.owner = sendBuffer
			views = append(views, view)
		}
	}
	decision := decideDeadClients(views, self.policy)

	deadClientCounters.records.Store(int64(len(views)))
	deadClientCounters.silent.Store(int64(decision.Silent))
	deadClientCounters.active.Store(int64(decision.Active))
	deadClientCounters.acking.Store(int64(decision.Acking))
	if decision.FleetCollapsed {
		deadClientCounters.fleetCollapsed.Store(1)
	} else {
		deadClientCounters.fleetCollapsed.Store(0)
	}
	if mode == governOff {
		return 0
	}

	released := 0
	for _, release := range decision.Release {
		view := release.View
		provider := owners[view.owner]
		if provider == nil {
			continue
		}
		switch release.Reason {
		case deadClientReasonHealthy:
			deadClientCounters.byReasonHealthy.Add(1)
		case deadClientReasonHard:
			deadClientCounters.byReasonHard.Add(1)
		case deadClientReasonSmall:
			deadClientCounters.byReasonSmall.Add(1)
		}
		if mode == governShadow {
			// counted once per silence: forget it so the next sweep does not count
			// the same silence again. A destination still silent after being
			// written to again starts a new silence and is counted again, which is
			// exactly when enforce would have released it a second time.
			deadClientCounters.wouldRelease.Add(1)
			transferMoreCounters.releasedHeldBytes.Add(provider.client.sendBuffer.HeldBytes(view.DestinationId))
			provider.client.sendBuffer.MarkDestinationReleased(view.DestinationId, now, true)
			if self.shouldLogShadow(view.DestinationId, now) {
				self.emit(fmt.Sprintf(
					"[govern] dead client %s silent %s (sent %.1f KiB, acked %.1f KiB; %d of %d active clients acking; %s); shadow, not released",
					view.DestinationId, view.SilentFor.Round(time.Second),
					float64(view.SentBytes)/1024, float64(view.AckedBytes)/1024, decision.Acking, decision.Active, release.Reason,
				))
			}
			continue
		}
		heldBytes := provider.client.sendBuffer.HeldBytes(view.DestinationId)
		sequences := provider.client.sendBuffer.CancelDestination(view.DestinationId)
		flows := provider.localUserNat.CancelSource(view.DestinationId)
		provider.client.sendBuffer.MarkDestinationReleased(view.DestinationId, now, false)
		transferMoreCounters.releasedHeldBytes.Add(heldBytes)
		deadClientCounters.released.Add(1)
		deadClientCounters.releasedFlows.Add(int64(flows))
		deadClientCounters.releasedSequences.Add(int64(sequences))
		released += 1
		self.emit(fmt.Sprintf(
			"[govern] released dead client %s: silent %s (sent %.1f KiB, acked %.1f KiB; %d of %d active clients acking; %s), cancelled %d send sequences and %d NAT flows",
			view.DestinationId, view.SilentFor.Round(time.Second),
			float64(view.SentBytes)/1024, float64(view.AckedBytes)/1024, decision.Acking, decision.Active, release.Reason,
			sequences, flows,
		))
	}
	return released
}

func (self *deadClientSweeper) shouldLogShadow(destinationId Id, now time.Time) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if last, ok := self.shadowLogAt[destinationId]; ok && now.Sub(last) < deadClientShadowLogEvery {
		return false
	}
	self.shadowLogAt[destinationId] = now
	// forget old names so the map cannot grow without bound
	for id, at := range self.shadowLogAt {
		if 4*deadClientShadowLogEvery < now.Sub(at) {
			delete(self.shadowLogAt, id)
		}
	}
	return true
}
