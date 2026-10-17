package connect

import (
	"net"
	"testing"
)

type fakeBufferedPacketConn struct {
	net.PacketConn
	readBuffer int
}

func (self *fakeBufferedPacketConn) SetReadBuffer(bytes int) error {
	self.readBuffer = bytes
	return nil
}

func TestCountedPacketConnCountsBothDirections(t *testing.T) {
	a, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	bw := &ProxyBandwidth{}
	counted := &countedPacketConn{PacketConn: a, bw: bw}
	if _, err := counted.WriteTo([]byte("hello"), b.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	n, from, err := b.ReadFrom(buffer)
	if err != nil || n != 5 {
		t.Fatalf("read n=%d err=%v", n, err)
	}
	if _, err := b.WriteTo([]byte("reply!!"), from); err != nil {
		t.Fatal(err)
	}
	if n, _, err := counted.ReadFrom(buffer); err != nil || n != 7 {
		t.Fatalf("counted read n=%d err=%v", n, err)
	}
	if bw.TotalTx.Load() != 5 || bw.TotalRx.Load() != 7 {
		t.Fatalf("tx=%d rx=%d, want 5 and 7", bw.TotalTx.Load(), bw.TotalRx.Load())
	}
}

func TestCountedPacketConnForwardsBufferSizes(t *testing.T) {
	inner := &fakeBufferedPacketConn{}
	counted := &countedPacketConn{PacketConn: inner, bw: &ProxyBandwidth{}}
	if err := counted.SetReadBuffer(1234); err != nil || inner.readBuffer != 1234 {
		t.Fatalf("read buffer not forwarded (%d, %v)", inner.readBuffer, err)
	}
	// an endpoint without the setter is not an error
	plain := &countedPacketConn{PacketConn: struct{ net.PacketConn }{}, bw: &ProxyBandwidth{}}
	if err := plain.SetWriteBuffer(1); err != nil {
		t.Fatal(err)
	}
}

func TestCountInjectedH3ConnWithoutRecordIsIdentity(t *testing.T) {
	a, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	transport := &PlatformTransport{}
	if got := transport.countInjectedH3Conn(a); got != a {
		t.Fatalf("wrapped with no bandwidth record")
	}
	if transport.countInjectedH3Conn(nil) != nil {
		t.Fatalf("nil must stay nil")
	}
}
