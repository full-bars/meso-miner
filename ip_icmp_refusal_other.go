//go:build !windows && !js

package connect

import (
	"errors"
	"syscall"
)

// the errors that mean the platform refuses unprivileged icmp: a refused
// socket (EACCES/EPERM) or an unsupported protocol. Everything else an open
// can return is a per-flow condition, not a platform refusal.
func icmpEgressRefusalErrno(err error) bool {
	return errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EPROTONOSUPPORT)
}

// write or read errors that leave the socket unusable
func icmpEgressUnusableErrno(err error) bool {
	return errors.Is(err, syscall.EBADF) || errors.Is(err, syscall.ENOTSOCK)
}
