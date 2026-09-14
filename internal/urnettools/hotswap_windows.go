//go:build windows

package urnettools

import (
	"errors"

	"golang.org/x/sys/windows"
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
//
// ERROR_ACCESS_DENIED from OpenProcess means the process exists but the
// caller lacks SYNCHRONIZE rights (e.g. urnet-tools running as a standard
// user while the provider is elevated or a service). In that case we
// conservatively report the process as alive to avoid false rollbacks.
func pidIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return true // Process exists but caller lacks SYNCHRONIZE rights.
		}
		return false
	}
	defer windows.CloseHandle(h)

	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == 0x00000102 // WAIT_TIMEOUT
}
