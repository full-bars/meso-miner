package connect

import (
	"errors"
	"syscall"
)

// isDialRefused reports whether a dial failed because the destination refused
// the connection. On windows Go does not fold the winsock error
// (WSAECONNREFUSED) onto syscall.ECONNREFUSED, which there is a placeholder
// value, so the platform file adds the native code.
func isDialRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || isPlatformDialRefused(err)
}

// isDialKernelTimeout reports whether the kernel's own connect timeout fired
// (ETIMEDOUT, or WSAETIMEDOUT on windows).
func isDialKernelTimeout(err error) bool {
	return errors.Is(err, syscall.ETIMEDOUT) || isPlatformDialKernelTimeout(err)
}
