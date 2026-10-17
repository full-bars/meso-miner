package connect

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

// The helpers must see the portable constants through any wrapping on every
// platform, and must not call an unrelated error a refusal or a timeout.
func TestDialErrnoHelpers(t *testing.T) {
	AssertEqual(t, isDialRefused(syscall.ECONNREFUSED), true)
	AssertEqual(t, isDialRefused(fmt.Errorf("dial: %w", syscall.ECONNREFUSED)), true)
	AssertEqual(t, isDialRefused(syscall.ENETUNREACH), false)
	AssertEqual(t, isDialRefused(errors.New("refused")), false)
	AssertEqual(t, isDialRefused(nil), false)

	AssertEqual(t, isDialKernelTimeout(syscall.ETIMEDOUT), true)
	AssertEqual(t, isDialKernelTimeout(fmt.Errorf("dial: %w", syscall.ETIMEDOUT)), true)
	AssertEqual(t, isDialKernelTimeout(syscall.ECONNREFUSED), false)
	AssertEqual(t, isDialKernelTimeout(nil), false)
}
