package connect

import (
	"errors"
	"syscall"
)

const (
	wsaETimedOut    syscall.Errno = 10060
	wsaEConnRefused syscall.Errno = 10061
)

func isPlatformDialRefused(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaEConnRefused
}

func isPlatformDialKernelTimeout(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaETimedOut
}
