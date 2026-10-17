package connect

import (
	"context"
	"encoding/binary"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
)

// The QUIC version offer of every carrier (EXTENDER.md A13): what the first
// Initial carries per policy at each construction site, that a version 1 only
// peer is still reached through negotiation, that forcing version 2 refuses
// such a peer rather than falling back in the clear, and that a retry after
// a failed handshake leaves its local port behind. Every observation is made
// on the wire bytes of the client's own udp endpoint, which is where a filter
// reads them, or on the version the peer negotiated.

// The wire version numbers as the long header carries them (RFC 9000 §15,
// RFC 9369 §2), and the version field of a Version Negotiation packet
// (RFC 9000 §17.2.1).
const (
	quicWireVersion1           uint32 = 0x00000001
	quicWireVersion2           uint32 = 0x6b3343cf
	quicWireVersionNegotiation uint32 = 0x00000000
)

// The version field of a datagram whose first packet has a long header,
// which every Initial and Version Negotiation packet has; false for a short
// header, which carries no version.

func quicDatagramVersion(datagram []byte) (uint32, bool) {
	if len(datagram) < 5 || datagram[0]&0x80 == 0 {
		return 0, false
	}
	return binary.BigEndian.Uint32(datagram[1:5]), true
}

// A loopback udp endpoint that records the long-header version of every
// datagram it sends and receives, in order. When holdPort is set, the close
// the dial owes it is counted but the socket stays bound until the test ends,
// so the operating system cannot hand the next endpoint the same port and a
// repeated port is a dial's choice, never the allocator's.
type testQuicVersionPacketConn struct {
	net.PacketConn
	holdPort bool
	// whether the endpoint before this one had been closed when this one was
	// asked for
	previousReleased bool

	stateLock       sync.Mutex
	writtenVersions []uint32
	readVersions    []uint32
	closeCount      int
}

func newTestQuicVersionPacketConn(holdPort bool) (*testQuicVersionPacketConn, error) {
	packetConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &testQuicVersionPacketConn{
		PacketConn: packetConn,
		holdPort:   holdPort,
	}, nil
}

func (self *testQuicVersionPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if version, ok := quicDatagramVersion(b); ok {
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.writtenVersions = append(self.writtenVersions, version)
		}()
	}
	return self.PacketConn.WriteTo(b, addr)
}

func (self *testQuicVersionPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := self.PacketConn.ReadFrom(b)
	if err == nil {
		if version, ok := quicDatagramVersion(b[:n]); ok {
			func() {
				self.stateLock.Lock()
				defer self.stateLock.Unlock()
				self.readVersions = append(self.readVersions, version)
			}()
		}
	}
	return n, addr, err
}

func (self *testQuicVersionPacketConn) Close() error {
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.closeCount += 1
	}()
	if self.holdPort {
		return nil
	}
	return self.PacketConn.Close()
}

// The versions written so far, the first being what the first Initial
// carried.
func (self *testQuicVersionPacketConn) written() []uint32 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.writtenVersions)
}

func (self *testQuicVersionPacketConn) read() []uint32 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.readVersions)
}

func (self *testQuicVersionPacketConn) closes() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.closeCount
}

func (self *testQuicVersionPacketConn) port() int {
	return self.PacketConn.LocalAddr().(*net.UDPAddr).Port
}

// The endpoints one test's dials asked for, in order. A factory may run on a
// dial goroutine, so the list is locked and nothing here fails the test
// directly.
type testQuicVersionEndpoints struct {
	t        *testing.T
	holdPort bool

	stateLock sync.Mutex
	endpoints []*testQuicVersionPacketConn
}

// A PacketConnFactory that hands out one recording endpoint per call.
func (self *testQuicVersionEndpoints) factory(context.Context) (net.PacketConn, error) {
	endpoint, err := newTestQuicVersionPacketConn(self.holdPort)
	if err != nil {
		return nil, err
	}
	self.t.Cleanup(func() { endpoint.PacketConn.Close() })
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	endpoint.previousReleased = len(self.endpoints) == 0 ||
		self.endpoints[len(self.endpoints)-1].closes() == 1
	self.endpoints = append(self.endpoints, endpoint)
	return endpoint, nil
}

func (self *testQuicVersionEndpoints) all() []*testQuicVersionPacketConn {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.endpoints)
}

// The first endpoint a test's dials asked for, which is the one whose first
// datagram is the offer.
func (self *testQuicVersionEndpoints) first(t *testing.T) *testQuicVersionPacketConn {
	t.Helper()
	endpoints := self.all()
	if len(endpoints) == 0 {
		t.Fatal("no dial asked for an endpoint")
	}
	return endpoints[0]
}

// One in-process udp carrier endpoint: a quic listener on loopback whose h3
// handler answers every extender request with an empty response frame, which
// is all DialExtender reads before it hands the stream over. It accepts the
// versions it is given, records the version each accepted connection
// negotiated, and refuses the Initial of every source address refuse names,
// the way a filter that poisoned a 4-tuple does.
type testQuicCarrierServer struct {
	port     int
	versions chan quic.Version
}

func (self *testQuicCarrierServer) nextVersion(t *testing.T) quic.Version {
	t.Helper()
	select {
	case version := <-self.versions:
		return version
	case <-time.After(10 * time.Second):
		t.Fatal("the carrier server accepted no connection")
		return 0
	}
}

// One extender dial over the udp carrier to the in-process server, under the
// policy and over the endpoints given.
func TestQuicVersionPolicyVersions(t *testing.T) {
	for _, c := range []struct {
		policy   QuicVersionPolicy
		versions []quic.Version
	}{
		{policy: "", versions: []quic.Version{quic.Version2, quic.Version1}},
		{policy: QuicVersionPolicyPreferV2, versions: []quic.Version{quic.Version2, quic.Version1}},
		{policy: QuicVersionPolicyV1, versions: []quic.Version{quic.Version1}},
		{policy: QuicVersionPolicyV2, versions: []quic.Version{quic.Version2}},
		{policy: "v3", versions: []quic.Version{quic.Version2, quic.Version1}},
	} {
		if versions := c.policy.Versions(); !slices.Equal(versions, c.versions) {
			t.Errorf("%q offers %v, want %v", c.policy, versions, c.versions)
		}
	}
	// the constants are the wire numbers the filters key on
	if uint32(quic.Version1) != quicWireVersion1 || uint32(quic.Version2) != quicWireVersion2 {
		t.Fatalf("quic-go versions are %#08x and %#08x", uint32(quic.Version1), uint32(quic.Version2))
	}
}

// Every defaults function offers version 2 first, every config built from the
// defaults carries that offer, and the extender dial's config takes the
// offer of its connect settings rather than of the platform transport that
// sized its windows.
