package connect

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// icmpCaptureLogger records Errorf lines so a test can assert the refusal
// log names the real error.
type icmpCaptureLogger struct {
	mu     sync.Mutex
	errors []string
}

func (self *icmpCaptureLogger) Info(args ...any)                    {}
func (self *icmpCaptureLogger) Infof(format string, args ...any)    {}
func (self *icmpCaptureLogger) Warningf(format string, args ...any) {}
func (self *icmpCaptureLogger) Errorf(format string, args ...any) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.errors = append(self.errors, fmt.Sprintf(format, args...))
}
func (self *icmpCaptureLogger) V(level int32) Verbose { return icmpNoopVerbose{} }

type icmpNoopVerbose struct{}

func (icmpNoopVerbose) Enabled() bool                    { return false }
func (icmpNoopVerbose) Info(args ...any)                 {}
func (icmpNoopVerbose) Infof(format string, args ...any) {}

// only a refused family latches, and a refusal in one family must never
// disable the other; every other open error is a per-flow condition
func TestIcmpEgressRefusalLatchIsNarrowAndPerFamily(t *testing.T) {
	resetIcmpGate(t)
	reset := func() {
		icmpEchoFamilyRefused.Store(0)
		icmpEnvState.Store(icmpEnvUnread)
		os.Unsetenv("URNETWORK_ICMP")
	}
	reset()
	defer reset()

	newRefusal := func(network string, errno syscall.Errno) error {
		return &net.OpError{Op: "listen", Net: network, Err: errno}
	}

	// EACCES on ipv4: ipv4 latches, ipv6 must stay enabled
	logger := &icmpCaptureLogger{}
	eacces := newRefusal("udp4", syscall.EACCES)
	icmpEgressRefused(logger, 4, eacces)
	if icmpEchoParsingEnabled(4) {
		t.Fatal("EACCES on ipv4 must latch ipv4 off")
	}
	if !icmpEchoParsingEnabled(6) {
		t.Fatal("EACCES on ipv4 must not disable ipv6")
	}
	if len(logger.errors) != 1 {
		t.Fatalf("the refusal must log exactly once, got %d lines", len(logger.errors))
	}
	if !strings.Contains(logger.errors[0], eacces.Error()) {
		t.Fatalf("the refusal log must name the real error, got %q", logger.errors[0])
	}

	// EAFNOSUPPORT on udp6 (an ipv6-disabled host): no latch at all
	reset()
	logger = &icmpCaptureLogger{}
	icmpEgressRefused(logger, 6, newRefusal("udp6", syscall.EAFNOSUPPORT))
	if !icmpEchoParsingEnabled(4) || !icmpEchoParsingEnabled(6) {
		t.Fatal("EAFNOSUPPORT must not latch either family")
	}
	if len(logger.errors) != 0 {
		t.Fatalf("a per-flow error must not log a refusal, got %q", logger.errors)
	}

	// EMFILE (fd exhaustion): per-flow drop, no latch
	icmpEgressRefused(logger, 4, newRefusal("udp4", syscall.EMFILE))
	if !icmpEchoParsingEnabled(4) {
		t.Fatal("EMFILE must not latch")
	}

	// EPROTONOSUPPORT on ipv6: ipv6 latches, ipv4 must stay enabled
	reset()
	icmpEgressRefused(logger, 6, newRefusal("udp6", syscall.EPROTONOSUPPORT))
	if icmpEchoParsingEnabled(6) {
		t.Fatal("EPROTONOSUPPORT on ipv6 must latch ipv6 off")
	}
	if !icmpEchoParsingEnabled(4) {
		t.Fatal("EPROTONOSUPPORT on ipv6 must not disable ipv4")
	}

	// an unsupported platform (js/wasm) classifies as a refusal and latches
	reset()
	icmpEgressRefused(logger, 4, icmpEgressUnsupportedError(4))
	if icmpEchoParsingEnabled(4) {
		t.Fatal("the unsupported-platform error must latch")
	}

	// a second refusal of the same family must not log twice
	reset()
	logger = &icmpCaptureLogger{}
	icmpEgressRefused(logger, 4, newRefusal("udp4", syscall.EACCES))
	icmpEgressRefused(logger, 4, newRefusal("udp4", syscall.EPERM))
	if len(logger.errors) != 1 {
		t.Fatalf("the refusal latches once per family, got %d log lines", len(logger.errors))
	}
}
