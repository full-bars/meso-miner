package connect

import (
	"context"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// a scripted egress: WriteEcho returns the scripted error for each call and
// records the payload lengths; ReadEcho blocks until Close.
type icmpScriptedEgress struct {
	mutex   sync.Mutex
	errors  []error
	writes  []int
	done    chan struct{}
	writeCh chan int
}

func newIcmpScriptedEgress(errors ...error) *icmpScriptedEgress {
	return &icmpScriptedEgress{
		errors:  errors,
		done:    make(chan struct{}),
		writeCh: make(chan int, 16),
	}
}

func (self *icmpScriptedEgress) WriteEcho(deadline time.Time, ttl int, sequenceNumber uint16, payload []byte) error {
	self.mutex.Lock()
	index := len(self.writes)
	self.writes = append(self.writes, len(payload))
	err := error(nil)
	if index < len(self.errors) {
		err = self.errors[index]
	}
	self.mutex.Unlock()
	self.writeCh <- index
	return err
}

func (self *icmpScriptedEgress) ReadEcho(deadline time.Time) (uint16, []byte, error) {
	<-self.done
	return 0, nil, net.ErrClosed
}

func (self *icmpScriptedEgress) Close() {
	close(self.done)
}

func (self *icmpScriptedEgress) writeCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.writes)
}

// test harness: buffer whose flows open a scripted egress
func newScriptedIcmpTestBuffer(t *testing.T, mtu int, scripted func() *icmpScriptedEgress) (*Icmp4Buffer, func() int, func() *icmpScriptedEgress, func()) {
	t.Helper()
	settings := DefaultIcmpBufferSettingsWithBufferSize(4)
	settings.Log = NewNoopLogger()
	if 0 < mtu {
		settings.Mtu = mtu
	}
	ctx, cancel := context.WithCancel(context.Background())

	mutex := sync.Mutex{}
	opens := 0
	current := scripted()
	restore := setIcmpEgressFactory(func(ctx context.Context, ipVersion int, destinationIp net.IP, s *IcmpBufferSettings) (icmpEgress, error) {
		mutex.Lock()
		defer mutex.Unlock()
		opens += 1
		current = scripted()
		return current, nil
	})
	buffer := NewIcmp4Buffer(ctx, nil, settings)
	openCount := func() int {
		mutex.Lock()
		defer mutex.Unlock()
		return opens
	}
	lastingEgress := func() *icmpScriptedEgress {
		mutex.Lock()
		defer mutex.Unlock()
		return current
	}
	cleanup := func() {
		buffer.mutex.Lock()
		for _, sequence := range buffer.sequences {
			sequence.Cancel()
		}
		buffer.mutex.Unlock()
		cancel()
		setIcmpEgressFactory(restore)
	}
	return buffer, openCount, lastingEgress, cleanup
}

func sendTestEcho(t *testing.T, buffer *Icmp4Buffer, identifier uint16, payload []byte) bool {
	t.Helper()
	packet := MessagePoolGet(2048)
	success, _ := buffer.send(
		TransferPath{},
		protocol.ProvideMode(0),
		&parsedIcmp{
			sourceIp:       net.ParseIP("10.0.0.9"),
			destinationIp:  net.ParseIP("203.0.113.7"),
			echoRequest:    true,
			identifier:     identifier,
			sequenceNumber: 1,
			ttl:            64,
			payload:        payload,
		},
		0,
		packet,
	)
	return success
}

func waitWrite(t *testing.T, egress *icmpScriptedEgress, want int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case index := <-egress.writeCh:
			if index == want {
				return
			}
		case <-deadline:
			t.Fatalf("write %d never reached the egress (got %d writes)", want, egress.writeCount())
		}
	}
}

func waitOpen(t *testing.T, openCount func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for openCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("egress open %d never happened", want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// a message error (EMSGSIZE) drops only that packet: the flow keeps its
// socket, so the next echo is written without reopening
func TestIcmpFailedSendKeepsTheFlow(t *testing.T) {
	resetIcmpGate(t)
	buffer, openCount, lastingEgress, cleanup := newScriptedIcmpTestBuffer(t, 0, func() *icmpScriptedEgress {
		return newIcmpScriptedEgress(&net.OpError{Op: "write", Net: "udp4", Err: syscall.EMSGSIZE})
	})
	defer cleanup()

	if !sendTestEcho(t, buffer, 0x11, []byte("first")) {
		t.Skip("could not open a flow here")
	}
	waitOpen(t, openCount, 1)
	firstEgress := lastingEgress()
	waitWrite(t, firstEgress, 0)
	// let the write error be handled before the next send
	time.Sleep(30 * time.Millisecond)

	if !sendTestEcho(t, buffer, 0x11, []byte("second")) {
		t.Fatal("the flow must survive a failed send")
	}
	waitWrite(t, firstEgress, 1)

	if opens := openCount(); opens != 1 {
		t.Fatalf("a failed send must keep the flow; the egress was opened %d times", opens)
	}
	// A message error must never count as a platform refusal. The decision is a
	// pure function of the error, so it is asserted directly: the process-wide
	// latch is also set by other tests' flows that really try to open an
	// unprivileged socket (a CI runner refuses them), and their goroutines can
	// outlive those tests, so reading the global here would be racy.
	if icmpEgressRefusalError(&net.OpError{Op: "write", Net: "udp4", Err: syscall.EMSGSIZE}) {
		t.Fatal("a message error is classified as a platform refusal")
	}
	egress := lastingEgress()
	if egress.writeCount() != 2 {
		t.Fatalf("expected both echoes written on the one socket, got %d", egress.writeCount())
	}
}

// a socket that is gone (EBADF) closes the flow, and the next send opens a
// fresh one
func TestIcmpUnusableSocketClosesTheFlow(t *testing.T) {
	resetIcmpGate(t)
	buffer, openCount, lastingEgress, cleanup := newScriptedIcmpTestBuffer(t, 0, func() *icmpScriptedEgress {
		return newIcmpScriptedEgress(&net.OpError{Op: "write", Net: "udp4", Err: syscall.EBADF})
	})
	defer cleanup()

	if !sendTestEcho(t, buffer, 0x21, []byte("doomed")) {
		t.Skip("could not open a flow here")
	}
	waitOpen(t, openCount, 1)
	waitWrite(t, lastingEgress(), 0)

	// the flow must close: the buffer's flow table drains
	deadline := time.Now().Add(3 * time.Second)
	for {
		buffer.mutex.Lock()
		remaining := len(buffer.sequences)
		buffer.mutex.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("an EBADF send must close the flow; %d flows left", remaining)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// and the next send opens a fresh egress
	if !sendTestEcho(t, buffer, 0x21, []byte("retry")) {
		t.Fatal("the next send must open a fresh flow")
	}
	openDeadline := time.Now().Add(3 * time.Second)
	for openCount() < 2 {
		if time.Now().After(openDeadline) {
			t.Fatal("the retry must open a fresh socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitWrite(t, lastingEgress(), 0)
	if opens := openCount(); opens != 2 {
		t.Fatalf("the retry must open a fresh socket, got %d opens", opens)
	}
}

// an oversized echo drops before the write and keeps the flow
func TestIcmpOversizedEchoDropsAndKeepsFlow(t *testing.T) {
	resetIcmpGate(t)
	buffer, openCount, lastingEgress, cleanup := newScriptedIcmpTestBuffer(t, 100, func() *icmpScriptedEgress {
		return newIcmpScriptedEgress()
	})
	defer cleanup()

	oversized := make([]byte, 200)
	if !sendTestEcho(t, buffer, 0x31, oversized) {
		t.Skip("could not open a flow here")
	}
	waitOpen(t, openCount, 1)
	egress := lastingEgress()
	if !sendTestEcho(t, buffer, 0x31, []byte("ok")) {
		t.Fatal("the flow must survive an oversized echo")
	}
	waitWrite(t, egress, 0)

	egress.mutex.Lock()
	writes := append([]int(nil), egress.writes...)
	egress.mutex.Unlock()
	if len(writes) != 1 || writes[0] != len("ok") {
		t.Fatalf("the oversized echo must not reach the egress; writes %v", writes)
	}
	if opens := openCount(); opens != 1 {
		t.Fatalf("an oversized echo must keep the flow; the egress was opened %d times", opens)
	}
}
