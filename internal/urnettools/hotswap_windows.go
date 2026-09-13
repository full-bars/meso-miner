//go:build windows

package urnettools

import (
	"syscall"
)

// triggerHotSwap sends {cmd: "hotswap"} over the provider's control socket
// to initiate the in-process handoff. Windows has no SIGUSR2, so the
// control socket is the only trigger path.
func triggerHotSwap(p Provider) error {
	if err := hotSwapPreflight(p); err != nil {
		return err
	}
	return triggerHotSwapViaSocket(p)
}

// pidIsAlive reports whether pid still refers to a running process.
// Uses OpenProcess with SYNCHRONIZE access and WaitForSingleObject: if the
// wait returns WAIT_OBJECT_0, the process has exited; WAIT_TIMEOUT means it
// is still running.
func pidIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		processSynchronize = 0x00100000
		waitObject0        = 0x00000000 // Process has exited
		waitTimeout        = 0x00000102 // Process is still running
	)

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpen := kernel32.NewProc("OpenProcess")
	procWait := kernel32.NewProc("WaitForSingleObject")

	handle, _, _ := procOpen.Call(
		uintptr(processSynchronize),
		0, // bInheritHandle = FALSE
		uintptr(pid),
	)
	if handle == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	ret, _, _ := procWait.Call(
		handle,
		0, // dwMilliseconds = 0 (immediate check)
	)
	return ret == waitTimeout // Still running
}
