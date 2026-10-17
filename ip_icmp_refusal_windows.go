//go:build windows

package connect

import (
	"errors"
	"syscall"
)

// windows folds winsock errors onto their own codes (see ip_dial_errno), so
// the refused-socket class is matched natively as well: ERROR_ACCESS_DENIED,
// WSAEACCES and WSAEPROTONOSUPPORT.
const (
	errnoAccessDenied          syscall.Errno = 5
	errnoWsaAccessDenied       syscall.Errno = 10013
	errnoWsaProtocolNotSupport syscall.Errno = 10043
)

func icmpEgressRefusalErrno(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case errnoAccessDenied, errnoWsaAccessDenied, errnoWsaProtocolNotSupport:
		return true
	}
	return false
}

// windows: WSAEBADF and WSAENOTSOCK leave the socket unusable
const (
	errnoWsaBadFile syscall.Errno = 10009
	errnoWsaNotSock syscall.Errno = 10038
)

func icmpEgressUnusableErrno(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errnoWsaBadFile || errno == errnoWsaNotSock
}
