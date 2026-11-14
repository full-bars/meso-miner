package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

type failingIPv6FamilyGenerator struct {
	testingEmptyMultiClientGenerator
	destination MultiHopId
}

func (g *failingIPv6FamilyGenerator) NextDestinationsWithIpFamily(_ int, _ []MultiHopId, _ string, family IpFamilyFilter) (map[MultiHopId]DestinationStats, error) {
	if family == IpFamilyFilterV6Capable {
		return nil, errors.New("IPv6 lookup failed")
	}
	return map[MultiHopId]DestinationStats{g.destination: {}}, nil
}

type failingIPv6CapabilityGenerator struct {
	failingIPv6FamilyGenerator
	known bool
}

func (g *failingIPv6CapabilityGenerator) SelectorMode() (bool, bool) { return false, g.known }
func (g *failingIPv6CapabilityGenerator) NextDestinationsWithCapabilities(_ int, _ []MultiHopId, _ string, family IpFamilyFilter, _ FindProvidersSelect) (map[MultiHopId]DestinationStats, bool, error) {
	result, err := g.NextDestinationsWithIpFamily(0, nil, "", family)
	return result, false, err
}

func TestFP2OptionalIPv6FailureKeepsIPv4(t *testing.T) {
	destination, err := NewMultiHopId(NewId())
	if err != nil {
		t.Fatal(err)
	}
	base := failingIPv6FamilyGenerator{destination: destination}
	generators := []MultiClientGenerator{&base, &failingIPv6CapabilityGenerator{failingIPv6FamilyGenerator: base, known: true}, &failingIPv6CapabilityGenerator{failingIPv6FamilyGenerator: base}}
	for _, generator := range generators {
		window := &multiClientWindow{ctx: context.Background(), generator: generator, settings: DefaultMultiClientSettings(), ipv6Shortfall: 1, windowType: WindowTypeQuality}
		got, err := window.discoverDestinations(nil, false, "quality")
		if err != nil || len(got) != 1 || got[0].destination != destination {
			t.Fatalf("%T: got %v, %v", generator, got, err)
		}
		if _, err := window.discoverDestinations(nil, true, "quality"); err == nil {
			t.Fatalf("%T: probe did not return error", generator)
		}
	}
}

func TestFP2ApiGeneratorPreservesFamily(t *testing.T) {
	id := NewId()
	api := walletMappingTestApi(t, "synthetic-token", func(w http.ResponseWriter, request *http.Request) {
		var args FindProviders2Args
		if err := json.NewDecoder(request.Body).Decode(&args); err != nil || args.IpFamily != IpFamilyFilterV6Capable {
			http.Error(w, "incorrect family filter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FindProviders2Result{Providers: []*FindProvidersProvider{{ClientId: id, IpFamily: IpFamilyDualstack, Tier: 2, EstimatedBytesPerSecond: 42}}})
	})
	generator := &ApiMultiClientGenerator{api: api}
	destinations, err := generator.NextDestinationsWithIpFamily(1, nil, "quality", IpFamilyFilterV6Capable)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := NewMultiHopId(id)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := destinations[destination]
	if !ok || stats.IpFamily != IpFamilyDualstack || stats.Capabilities.Ip != ProviderIpCapabilityDualstack || stats.Tier != 2 || stats.EstimatedBytesPerSecond != 42 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	channel := &multiClientChannel{args: &multiClientChannelArgs{DestinationStats: stats}}
	if !channel.supportsFlow(6, IpProtocolUdp) {
		t.Fatal("dualstack destination rejected IPv6")
	}
}

func TestFP2ZeroWindowAdvertisementRecordedOnce(t *testing.T) {
	r := NewPeerCapabilityRecorder(1)
	clock := time.Unix(100, 0)
	r.nowForTest = func() time.Time { return clock }
	id := NewId()
	s := &SendSequence{client: &Client{peerCapabilityRecorder: r}, destination: DestinationId(id)}
	s.observePeerCapabilityAck(&protocol.Ack{})
	clock = clock.Add(time.Second)
	zero := uint64(0)
	s.observePeerCapabilityAck(&protocol.Ack{ReceiveWindowByteCount: &zero})
	if !r.peerIdCapabilities[id].windowAdvertised {
		t.Fatal("zero window was not recorded")
	}
	observed := r.peerIdCapabilities[id].lastObservationTime
	clock = clock.Add(time.Second)
	s.observePeerCapabilityAck(&protocol.Ack{ReceiveWindowByteCount: &zero})
	if r.peerIdCapabilities[id].lastObservationTime != observed {
		t.Fatal("repeated zero was recorded")
	}
	one := uint64(1)
	s.observePeerCapabilityAck(&protocol.Ack{ReceiveWindowByteCount: &one})
	if r.peerIdCapabilities[id].lastObservationTime != clock {
		t.Fatal("increase was not recorded")
	}
}
