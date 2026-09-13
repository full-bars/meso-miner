//go:build windows

package urnettools

import (
	"errors"
	"syscall"
)

// triggerHotSwap on Windows is a stub until the named pipe adapter is connected.
func triggerHotSwap(p Provider) error {
	return errors.New("zero-downtime hotswap is not yet supported on Windows. Use `urnet-tools restart` to apply updates (there will be a brief restart gap)")
}

// pidIsAlive reports whether a process with the given PID is still running.
// Uses OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION | SYNCHRONIZE
// and WaitForSingleObject with a zero timeout to check without blocking.
func pidIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		processQueryLimitedInfo = 0x1000
		synchronize             = 0x00100000
		waitTimeout             = 0x00000102
	)
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpen := kernel32.NewProc("OpenProcess")
	procWait := kernel32.NewProc("WaitForSingleObject")
	procClose := kernel32.NewProc("CloseHandle")

	h, _, _ := procOpen.Call(
		uintptr(processQueryLimitedInfo|synchronize),
		0, // bInheritHandle = FALSE
		uintptr(pid),
	)
	if h == 0 {
		return false
	}
	defer procClose.Call(h)

	ret, _, _ := procWait.Call(h, 0) // 0 timeout = poll
	return ret == waitTimeout
}
