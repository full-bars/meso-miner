package connect

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// deterministic edge coverage for the v4/v6 fragment reassemblers. the
// nat-level tests cover completion order and shard pinning; these pin the
// coded policy for duplicate and overlapping fragments, holes, the datagram,
// fragment and byte caps, and the injected-clock expiry. no network, no
// sleeps: every case drives processResultAt directly with an explicit now.

type fragmentReassembler interface {
	processResultAt(source TransferPath, transferKey TransferKey, provideMode protocol.ProvideMode, packet []byte, now time.Time, retainCompletedFragments bool) ipFragmentProcessResult
	close()
}

func datagramCount(r fragmentReassembler) int {
	switch v := r.(type) {
	case *ipv4FragmentReassembler:
		return len(v.datagrams)
	case *ipv6FragmentReassembler:
		return len(v.datagrams)
	}
	return -1
}

func retainedByteCount(r fragmentReassembler) int {
	switch v := r.(type) {
	case *ipv4FragmentReassembler:
		return v.retainedByteCount
	case *ipv6FragmentReassembler:
		return v.retainedByteCount
	}
	return -1
}

func setFragmentMaxRetainedBytes(r fragmentReassembler, maxRetainedBytes int) {
	switch v := r.(type) {
	case *ipv4FragmentReassembler:
		v.maxRetainedBytes = maxRetainedBytes
	case *ipv6FragmentReassembler:
		v.maxRetainedBytes = maxRetainedBytes
	}
}

func hasFragmentIdent(r fragmentReassembler, ident uint32) bool {
	switch v := r.(type) {
	case *ipv4FragmentReassembler:
		for key := range v.datagrams {
			if uint32(key.identification) == ident {
				return true
			}
		}
	case *ipv6FragmentReassembler:
		for key := range v.datagrams {
			if key.identification == ident {
				return true
			}
		}
	}
	return false
}

func datagramFragmentCount(r fragmentReassembler, ident uint32) int {
	switch v := r.(type) {
	case *ipv4FragmentReassembler:
		for key, datagram := range v.datagrams {
			if uint32(key.identification) == ident {
				return len(datagram.fragments)
			}
		}
	case *ipv6FragmentReassembler:
		for key, datagram := range v.datagrams {
			if key.identification == ident {
				return len(datagram.fragments)
			}
		}
	}
	return -1
}

func fragmentIdent(fragment []byte) uint32 {
	if fragment[0]>>4 == 4 {
		return uint32(binary.BigEndian.Uint16(fragment[4:6]))
	}
	return binary.BigEndian.Uint32(fragment[Ipv6HeaderSize+4 : Ipv6HeaderSize+8])
}

func newFragmentReassembler(version int) fragmentReassembler {
	if version == 4 {
		return newIpv4FragmentReassembler()
	}
	return newIpv6FragmentReassembler()
}

// fragmentSet builds one complete udp datagram of payloadByteCount bytes,
// fragments it at mtu, and returns the fragments plus the payload they carry.
// every call draws a fresh identification, so each set is its own datagram.
func fragmentSet(t *testing.T, version int, payloadByteCount int, mtu int) ([][]byte, []byte) {
	t.Helper()
	path := &IpPath{Version: version, Protocol: IpProtocolUdp, SourcePort: 42001, DestinationPort: 443}
	if version == 4 {
		path.SourceIp = net.ParseIP("10.0.0.9").To4()
		path.DestinationIp = net.ParseIP("203.0.113.7").To4()
	} else {
		path.SourceIp = net.ParseIP("fd00::9")
		path.DestinationIp = net.ParseIP("2001:db8::7")
	}
	payload := make([]byte, payloadByteCount)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}
	whole := ipOosUdpPacket(path, payload)
	var fragments [][]byte
	var err error
	if version == 4 {
		fragments, err = fragmentIpv4Packet(whole, mtu)
	} else {
		fragments, err = fragmentIpv6Packet(whole, mtu)
	}
	if err != nil || len(fragments) < 2 {
		t.Fatalf("v%d: fragmenting %d bytes at mtu %d: %d fragments, err=%v", version, payloadByteCount, mtu, len(fragments), err)
	}
	return fragments, payload
}

func fragmentPayloadLen(fragment []byte) int {
	if fragment[0]>>4 == 4 {
		headerByteCount := int(fragment[0]&0x0f) * 4
		return int(binary.BigEndian.Uint16(fragment[2:4])) - headerByteCount
	}
	return int(binary.BigEndian.Uint16(fragment[4:6])) - ipv6FragmentHeaderSize
}

func copyFragment(fragment []byte) []byte {
	duplicate := MessagePoolGet(len(fragment))
	copy(duplicate, fragment)
	return duplicate
}

func returnFragments(fragments ...[]byte) {
	for _, fragment := range fragments {
		MessagePoolReturn(fragment)
	}
}

// retagIpv4Fragment rewrites only the fragment offset (in bytes) and the
// more-fragments flag of a v4 fragment copy.
func retagIpv4Fragment(fragment []byte, offsetByteCount int, moreFragments bool) []byte {
	retagged := copyFragment(fragment)
	second := binary.BigEndian.Uint16(retagged[6:8]) & 0xe000
	second |= uint16(offsetByteCount / 8)
	if moreFragments {
		second |= 0x2000
	}
	binary.BigEndian.PutUint16(retagged[6:8], second)
	return retagged
}

// retagIpv6Fragment rewrites only the fragment offset (as this package's
// builders encode it: raw bytes) and the more-fragments flag of a v6 copy.
func retagIpv6Fragment(fragment []byte, offsetByteCount int, moreFragments bool) []byte {
	retagged := copyFragment(fragment)
	value := uint16(offsetByteCount)
	if moreFragments {
		value |= 0x1
	}
	binary.BigEndian.PutUint16(retagged[Ipv6HeaderSize+2:Ipv6HeaderSize+4], value)
	return retagged
}

func feedFragment(r fragmentReassembler, fragment []byte, now time.Time) ipFragmentProcessResult {
	return r.processResultAt(TransferPath{}, TransferKey{}, protocol.ProvideMode_Network, fragment, now, false)
}

func assertReassembledPayload(t *testing.T, packet []byte, want []byte) {
	t.Helper()
	if packet == nil {
		t.Fatal("the fragments did not reassemble")
	}
	defer MessagePoolReturn(packet)
	_, payload, err := ParseIpPathWithPayload(packet)
	if err != nil {
		t.Fatalf("reassembled packet does not parse: %v", err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("reassembled payload is %d bytes, want %d", len(payload), len(want))
	}
}

func TestFragmentExactDuplicateIsHarmless(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, payload := fragmentSet(t, version, 2600, 544)
		now := time.Unix(0, 0)
		ident := fragmentIdent(fragments[0])
		feedFragment(r, fragments[0], now)
		feedFragment(r, fragments[1], now)
		// an exact retransmission is accepted and changes nothing
		result := feedFragment(r, copyFragment(fragments[1]), now)
		if !result.accepted || result.packet != nil {
			t.Fatalf("v%d: an exact duplicate was not accepted in place", version)
		}
		if datagramCount(r) != 1 || datagramFragmentCount(r, ident) != 2 {
			t.Fatalf("v%d: the duplicate changed the retained state", version)
		}
		var whole []byte
		for _, fragment := range fragments[2:] {
			whole = feedFragment(r, fragment, now).packet
		}
		assertReassembledPayload(t, whole, payload)
	}
}

func TestFragmentConflictingDuplicatePoisonsDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, _ := fragmentSet(t, version, 2600, 544)
		now := time.Unix(0, 0)
		ident := fragmentIdent(fragments[0])
		feedFragment(r, fragments[0], now)
		feedFragment(r, fragments[1], now)
		// the same range with one changed payload byte poisons the datagram
		conflict := copyFragment(fragments[1])
		conflict[len(conflict)-1] ^= 1
		result := feedFragment(r, conflict, now)
		if result.accepted {
			t.Fatalf("v%d: a conflicting duplicate was accepted", version)
		}
		if datagramCount(r) != 0 || retainedByteCount(r) != 0 {
			t.Fatalf("v%d: the poisoned datagram was not released", version)
		}
		// a late fragment of the poisoned datagram starts from nothing
		result = feedFragment(r, copyFragment(fragments[0]), now)
		if !result.accepted || datagramCount(r) != 1 || datagramFragmentCount(r, ident) != 1 {
			t.Fatalf("v%d: the poisoned datagram's state survived", version)
		}
		// and a complete fresh set still reassembles
		fresh, payload := fragmentSet(t, version, 2600, 544)
		var whole []byte
		for _, fragment := range fresh {
			whole = feedFragment(r, fragment, now).packet
		}
		assertReassembledPayload(t, whole, payload)
	}
}

func TestFragmentPartialOverlapPoisonsDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, _ := fragmentSet(t, version, 2600, 544)
		now := time.Unix(0, 0)
		feedFragment(r, fragments[0], now)
		overlappingOffset := fragmentPayloadLen(fragments[0]) - 8
		var overlapping []byte
		if version == 4 {
			overlapping = retagIpv4Fragment(fragments[1], overlappingOffset, true)
		} else {
			overlapping = retagIpv6Fragment(fragments[1], overlappingOffset, true)
		}
		returnFragments(fragments[1:]...)
		result := feedFragment(r, overlapping, now)
		if result.accepted || datagramCount(r) != 0 {
			t.Fatalf("v%d: a partially overlapping fragment did not poison the datagram", version)
		}
	}
}

func TestFragmentHoleCompletesAfterLateFragment(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, payload := fragmentSet(t, version, 2600, 544)
		if len(fragments) < 5 {
			t.Fatalf("v%d: expected at least 5 fragments, got %d", version, len(fragments))
		}
		now := time.Unix(0, 0)
		ident := fragmentIdent(fragments[0])
		// every fragment but the one at index 2 lands first, the final one
		// included: a hole before the end completes nothing
		for index, fragment := range fragments {
			if index == 2 {
				continue
			}
			if result := feedFragment(r, fragment, now); result.packet != nil {
				t.Fatalf("v%d: completed with a hole at fragment 2", version)
			}
		}
		if datagramCount(r) != 1 || datagramFragmentCount(r, ident) != len(fragments)-1 {
			t.Fatalf("v%d: the holed datagram is not retained", version)
		}
		// the late hole filler completes the datagram
		whole := feedFragment(r, fragments[2], now).packet
		assertReassembledPayload(t, whole, payload)
	}
}

func TestFragmentMissingFinalNeverCompletes(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, _ := fragmentSet(t, version, 2600, 544)
		now := time.Unix(0, 0)
		for _, fragment := range fragments[:len(fragments)-1] {
			if result := feedFragment(r, fragment, now); result.packet != nil {
				t.Fatalf("v%d: completed without the final fragment", version)
			}
		}
		if datagramCount(r) != 1 {
			t.Fatalf("v%d: the missing-final datagram is not retained", version)
		}
		returnFragments(fragments[len(fragments)-1])
	}
}

func TestFragmentPayloadZeroIsDroppedWithoutDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, _ := fragmentSet(t, version, 2600, 544)
		stripped := copyFragment(fragments[0])
		if version == 4 {
			// total length equal to the header: payloadByteCount is zero
			headerByteCount := int(stripped[0]&0x0f) * 4
			binary.BigEndian.PutUint16(stripped[2:4], uint16(headerByteCount))
		} else {
			// declared payload length of the fragment header alone
			binary.BigEndian.PutUint16(stripped[4:6], uint16(ipv6FragmentHeaderSize))
		}
		result := feedFragment(r, stripped, time.Unix(0, 0))
		returnFragments(fragments[0], fragments[1])
		if result.accepted || datagramCount(r) != 0 {
			t.Fatalf("v%d: a zero payload fragment was not dropped", version)
		}
	}
}

func TestFragmentDatagramCapEvictsOldest(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		base := time.Unix(0, 0)
		idents := make([]uint32, 0, 16)
		for i := 0; i < 16; i++ {
			fragments, _ := fragmentSet(t, version, 600, 544)
			feedFragment(r, fragments[0], base.Add(time.Duration(i)*time.Millisecond))
			returnFragments(fragments[1])
			idents = append(idents, fragmentIdent(fragments[0]))
		}
		if datagramCount(r) != 16 {
			t.Fatalf("v%d: expected 16 retained datagrams, got %d", version, datagramCount(r))
		}
		// the seventeenth datagram evicts the oldest
		fragments, _ := fragmentSet(t, version, 600, 544)
		feedFragment(r, fragments[0], base.Add(200*time.Millisecond))
		returnFragments(fragments[1])
		if datagramCount(r) != 16 {
			t.Fatalf("v%d: the datagram cap drifted to %d", version, datagramCount(r))
		}
		if hasFragmentIdent(r, idents[0]) {
			t.Fatalf("v%d: the oldest datagram survived eviction", version)
		}
		if !hasFragmentIdent(r, idents[1]) || !hasFragmentIdent(r, fragmentIdent(fragments[0])) {
			t.Fatalf("v%d: eviction took the wrong datagram", version)
		}
	}
}

func TestFragmentCountCapDropsTheDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		// lift the byte cap so the fragment count cap is what bites: pool
		// buffer cost makes tiny fragments heavy against the default cap
		setFragmentMaxRetainedBytes(r, 1<<20)
		mtu := 56
		if version == 4 {
			mtu = 28
		}
		// 520 payload plus the 8 byte udp header at an 8 byte payload per
		// fragment: 66 fragments, two past the 64 fragment cap
		fragments, _ := fragmentSet(t, version, 520, mtu)
		if len(fragments) != 66 {
			t.Fatalf("v%d: expected 66 fragments, got %d", version, len(fragments))
		}
		now := time.Unix(0, 0)
		ident := fragmentIdent(fragments[0])
		for index, fragment := range fragments[:64] {
			if result := feedFragment(r, fragment, now); !result.accepted || result.packet != nil {
				t.Fatalf("v%d: fragment %d under the cap was not retained (accepted=%v, completed=%v)", version, index, result.accepted, result.packet != nil)
			}
		}
		if datagramFragmentCount(r, ident) != 64 {
			t.Fatalf("v%d: expected 64 retained fragments, got %d", version, datagramFragmentCount(r, ident))
		}
		// the 65th fragment exceeds the fragment cap: the whole datagram drops
		result := feedFragment(r, fragments[64], now)
		if result.accepted || result.packet != nil {
			t.Fatalf("v%d: the fragment cap did not drop", version)
		}
		returnFragments(fragments[65])
		if datagramCount(r) != 0 || retainedByteCount(r) != 0 {
			t.Fatalf("v%d: the capped datagram was not released (count %d, bytes %d)", version, datagramCount(r), retainedByteCount(r))
		}
	}
}

func TestFragmentExpiryUsesFirstFragmentTime(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		base := time.Unix(0, 0)
		setA, _ := fragmentSet(t, version, 2600, 544)
		identA := fragmentIdent(setA[0])
		feedFragment(r, setA[0], base)
		// a duplicate near the timeout refreshes activity but not the lifetime
		if result := feedFragment(r, copyFragment(setA[0]), base.Add(14*time.Second)); !result.accepted {
			t.Fatalf("v%d: the refresh duplicate was rejected", version)
		}
		if !hasFragmentIdent(r, identA) {
			t.Fatalf("v%d: the datagram expired before its timeout", version)
		}
		// at the timeout the lifetime, measured from the first fragment, ends
		setB, _ := fragmentSet(t, version, 600, 544)
		feedFragment(r, setB[0], base.Add(15*time.Second))
		returnFragments(setB[1])
		if hasFragmentIdent(r, identA) {
			t.Fatalf("v%d: a refreshed datagram survived the first-fragment timeout", version)
		}
		if datagramCount(r) != 1 {
			t.Fatalf("v%d: expected only the new datagram, got %d", version, datagramCount(r))
		}
		// a late fragment of the expired datagram starts a fresh, empty one
		result := feedFragment(r, setA[1], base.Add(15*time.Second+time.Millisecond))
		if !result.accepted || datagramCount(r) != 2 || datagramFragmentCount(r, identA) != 1 {
			t.Fatalf("v%d: the expired datagram's partial state survived", version)
		}
	}
}

func TestFragmentUnfragmentedPacketPassesThrough(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		path := &IpPath{Version: version, Protocol: IpProtocolUdp, SourcePort: 42001, DestinationPort: 443}
		if version == 4 {
			path.SourceIp = net.ParseIP("10.0.0.9").To4()
			path.DestinationIp = net.ParseIP("203.0.113.7").To4()
		} else {
			path.SourceIp = net.ParseIP("fd00::9")
			path.DestinationIp = net.ParseIP("2001:db8::7")
		}
		built := ipOosUdpPacket(path, make([]byte, 64))
		whole := copyFragment(built)
		MessagePoolReturn(built)
		defer MessagePoolReturn(whole)
		result := feedFragment(r, whole, time.Unix(0, 0))
		if !result.accepted || result.fragment || len(result.packet) != len(whole) {
			t.Fatalf("v%d: an unfragmented packet was not passed through", version)
		}
		if datagramCount(r) != 0 {
			t.Fatalf("v%d: an unfragmented packet created reassembly state", version)
		}
	}
}

// the retained byte cap charges the pool buffer's capacity, so a small
// fragment can cost kilobytes: a datagram that grows past the cap while it
// waits is released whole, and new datagrams evict the oldest to make room.
func TestFragmentRetainedBytesCapDropsGrownDatagram(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		fragments, _ := fragmentSet(t, version, 2600, 544)
		now := time.Unix(0, 0)
		if result := feedFragment(r, fragments[0], now); !result.accepted {
			t.Fatalf("v%d: the first fragment was not retained", version)
		}
		cost := retainedByteCount(r)
		// room for two fragments only: the third exceeds and drops the whole
		setFragmentMaxRetainedBytes(r, 3*cost-1)
		if result := feedFragment(r, fragments[1], now); !result.accepted {
			t.Fatalf("v%d: the second fragment was not retained", version)
		}
		result := feedFragment(r, fragments[2], now)
		if result.accepted || result.packet != nil {
			t.Fatalf("v%d: a datagram grown past the byte cap was not dropped", version)
		}
		returnFragments(fragments[3:]...)
		if datagramCount(r) != 0 || retainedByteCount(r) != 0 {
			t.Fatalf("v%d: the byte-capped datagram was not released", version)
		}
	}
}

// the nat budget must admit the real fragment patterns this line sees: a
// ~9 KiB reply (the measured 9,124-byte peer TLS flight) fragments into 9
// fragments at the tunnel mtu and must reassemble under
// natFragmentReassemblyMaxRetainedBytes.
func TestFragmentNatBudgetAdmitsARealisticReply(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		setFragmentMaxRetainedBytes(r, natFragmentReassemblyMaxRetainedBytes)
		fragments, payload := fragmentSet(t, version, 9124, 1100)
		now := time.Unix(0, 0)
		var whole []byte
		for _, fragment := range fragments {
			whole = feedFragment(r, fragment, now).packet
		}
		assertReassembledPayload(t, whole, payload)
	}
}

func TestFragmentRetainedBytesCapEvictsOldest(t *testing.T) {
	for _, version := range []int{4, 6} {
		r := newFragmentReassembler(version)
		defer r.close()
		// an explicit small budget so the BYTE-cap eviction path is what
		// runs: the fragments' own bytes are charged, so even a few
		// datagrams exceed it and the oldest gives way
		setFragmentMaxRetainedBytes(r, 10*1024)
		base := time.Unix(0, 0)
		idents := make([]uint32, 0, 20)
		for i := 0; i < 20; i++ {
			fragments, _ := fragmentSet(t, version, 600, 544)
			feedFragment(r, fragments[0], base.Add(time.Duration(i)*time.Millisecond))
			returnFragments(fragments[1])
			idents = append(idents, fragmentIdent(fragments[0]))
			if 10*1024 < retainedByteCount(r) {
				t.Fatalf("v%d: retained bytes exceed the cap (%d)", version, retainedByteCount(r))
			}
		}
		if datagramCount(r) >= 20 {
			t.Fatalf("v%d: no eviction happened under a %d byte cap", version, 10*1024)
		}
		if hasFragmentIdent(r, idents[0]) {
			t.Fatalf("v%d: the oldest datagram survived the byte-cap eviction", version)
		}
		if !hasFragmentIdent(r, idents[len(idents)-1]) {
			t.Fatalf("v%d: the newest datagram was evicted", version)
		}
	}
}
