package connect

import (
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// The provider-ingress fragment gate charges each retained fragment's own
// bytes, and the per-source quota keeps one client's stream of opening
// fragments from evicting every other client's half-built datagrams.

// feedFragmentFrom is feedFragment with an explicit source, for the
// multi-source cases.
func feedFragmentFrom(r fragmentReassembler, source TransferPath, fragment []byte, now time.Time) ipFragmentProcessResult {
	return r.processResultAt(source, TransferKey{}, protocol.ProvideMode_Network, fragment, now, false)
}

func setFragmentPerSourceMaxRetainedBytes(t *testing.T, r fragmentReassembler, maxRetainedBytes int) {
	t.Helper()
	switch reassembler := r.(type) {
	case *ipv4FragmentReassembler:
		reassembler.maxRetainedBytesPerSource = maxRetainedBytes
	case *ipv6FragmentReassembler:
		reassembler.maxRetainedBytesPerSource = maxRetainedBytes
	default:
		t.Fatalf("unknown reassembler %T", r)
	}
}

// eleven half-built datagrams from eleven distinct sources fit the provider's
// 16 KiB budget when the fragment's own bytes are charged (with the old
// capacity charging, only about seven did), and every one of them completes
// when its remaining fragment arrives.
func TestFragmentGateBudgetChargesFragmentLength(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		setFragmentMaxRetainedBytes(r, providerIngressFragmentMaxRetainedBytes)
		base := time.Unix(0, 0)

		// eleven firsts leave headroom for one completing second inside the
		// budget while the other datagrams are still half-built
		const halves = 11
		type halfBuilt struct {
			source TransferPath
			second []byte
		}
		built := make([]halfBuilt, 0, halves)
		expectedRetained := 0
		for i := 0; i < halves; i++ {
			fragments, _ := fragmentSet(t, version, 2400, 1280)
			if len(fragments) != 2 {
				t.Fatalf("v%d: expected a two-fragment set, got %d", version, len(fragments))
			}
			source := TransferPath{SourceId: NewId()}
			result := feedFragmentFrom(r, source, fragments[0], base.Add(time.Duration(i)*time.Millisecond))
			if result.packet != nil {
				t.Fatalf("v%d: a first fragment must not complete a datagram", version)
			}
			expectedRetained += len(fragments[0])
			built = append(built, halfBuilt{source: source, second: fragments[1]})
		}
		if expectedRetained > providerIngressFragmentMaxRetainedBytes {
			t.Fatalf("v%d: test premise: %d first-fragment bytes exceed the %d byte budget", version, expectedRetained, providerIngressFragmentMaxRetainedBytes)
		}
		if got := retainedByteCount(r); got != expectedRetained {
			t.Fatalf("v%d: retained bytes must charge the fragment length: got %d, want %d", version, got, expectedRetained)
		}
		if got := datagramCount(r); got != halves {
			t.Fatalf("v%d: all %d half-built datagrams must stay under the budget, got %d", version, halves, got)
		}

		completed := 0
		for i, half := range built {
			result := feedFragmentFrom(r, half.source, half.second, base.Add(time.Duration(100+i)*time.Millisecond))
			if result.packet != nil {
				completed += 1
				MessagePoolReturn(result.packet)
			}
		}
		if completed != halves {
			t.Fatalf("v%d: %d of %d datagrams completed, want all", version, completed, halves)
		}
	}
}

// a source streaming opening fragments past its own quota sheds its own
// oldest datagrams; the well-behaved source's half-built datagram survives
// and completes.
func TestFragmentGatePerSourceQuotaProtectsOtherSources(t *testing.T) {
	for _, version := range []int{4, 6} {
		gate := &ipFragmentGate{
			maxRetainedBytes:          providerIngressFragmentMaxRetainedBytes,
			maxRetainedBytesPerSource: providerIngressFragmentMaxRetainedBytesPerSource,
		}
		defer gate.close()

		good := TransferPath{SourceId: NewId()}
		hostile := TransferPath{SourceId: NewId()}

		goodFragments, goodPayload := fragmentSet(t, version, 2400, 1280)
		if len(goodFragments) != 2 {
			t.Fatalf("v%d: expected a two-fragment set, got %d", version, len(goodFragments))
		}
		result := gate.processOwned(good, TransferKey{}, protocol.ProvideMode_Network, MessagePoolCopy(goodFragments[0]))
		returnIpFragmentProcessResult(result)

		// the hostile source opens datagram after datagram, enough to exceed
		// the whole shared budget if nothing pruned it by source
		hostileOpened := 0
		for i := 0; i < 15; i++ {
			fragments, _ := fragmentSet(t, version, 2400, 1280)
			copied := MessagePoolCopy(fragments[0])
			result := gate.processOwned(hostile, TransferKey{}, protocol.ProvideMode_Network, copied)
			returnIpFragmentProcessResult(result)
			returnFragments(fragments[1])
			hostileOpened += 1
		}
		if hostileOpened != 15 {
			t.Fatal("test premise: the hostile source opened no datagrams")
		}

		// the good source's datagram is still there: its second fragment
		// completes it
		result = gate.processOwned(good, TransferKey{}, protocol.ProvideMode_Network, MessagePoolCopy(goodFragments[1]))
		if result.packet == nil {
			t.Fatalf("v%d: the hostile source's stream evicted the well-behaved source's half-built datagram", version)
		}
		assertReassembledPayload(t, result.packet, goodPayload)
		// the gate keeps the original fragment owners on completion
		returnFragments(result.fragments...)

		// and the hostile source stayed within its own share
		var hostileRetained int
		if version == 4 {
			hostileRetained = gate.reassembler4.retainedBytesForSource(hostile)
		} else {
			hostileRetained = gate.reassembler6.retainedBytesForSource(hostile)
		}
		if providerIngressFragmentMaxRetainedBytesPerSource < hostileRetained {
			t.Fatalf("v%d: the hostile source retained %d bytes, over its %d byte quota", version, hostileRetained, providerIngressFragmentMaxRetainedBytesPerSource)
		}
		returnFragments(goodFragments[0])
	}
}
