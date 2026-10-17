package connect

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// Go leaves the winsock codes distinct from the portable syscall constants, so
// the classifier must recognise the native codes itself. These are the values
// a real windows dial returns.
func TestClassifyDialFailureWindowsNativeCodes(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", syscall.Errno(10061))}
	wrapped := fmt.Errorf("dial tcp 203.0.113.9:443: %w", refused)
	timedOut := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", syscall.Errno(10060))}

	for _, version := range []int{4, 6} {
		path := dialFailureTestPath(IpProtocolTcp, version)
		for _, err := range []error{refused, wrapped} {
			action, packet := classifyDialFailure(path, err, 0)
			AssertEqual(t, action, dialFailureRst)
			AssertEqual(t, packet == nil, true)
		}
		action, packet := classifyDialFailure(path, timedOut, 0)
		AssertEqual(t, action, dialFailureNone)
		AssertEqual(t, packet == nil, true)
	}
}
