//go:build windows

package urnettools

import (
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

// cmdStopWindows stops the provider on Windows by sending a shutdown command
// over the control socket, then waiting for the process to exit. Falls back
// to TerminateProcess if the graceful shutdown does not complete within 15s.
func cmdStopWindows(p Provider, force, dryRun bool) error {
	if p.StateDir == "" {
		return fmt.Errorf("provider %s has no resolvable state dir", providerLabel(p))
	}
	sockPath := filepath.Join(p.StateDir, "provider.sock")

	// Attempt a graceful shutdown via the control socket.
	fmt.Println("sending shutdown command...")
	resp, err := sendSocketRequest(sockPath, controlRequest{Cmd: "shutdown"})
	if err != nil {
		if isSocketUnavailable(err) {
			fmt.Printf("provider %s is not running (control socket unreachable)\n", providerLabel(p))
			return nil
		}
		fmt.Printf("warning: shutdown command failed: %v\n", err)
	} else if !resp.OK {
		fmt.Printf("warning: shutdown response: %s\n", resp.Error)
	}

	// Wait up to 15 seconds for the process to exit.
	if p.PID > 0 {
		deadline := time.After(15 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline:
				fmt.Println("provider did not exit within 15s, terminating...")
				if err := terminateProcess(p.PID); err != nil {
					return fmt.Errorf("terminate process %d: %w", p.PID, err)
				}
				fmt.Printf("forcefully terminated %s\n", providerLabel(p))
				return nil
			case <-ticker.C:
				if !pidIsAlive(p.PID) {
					fmt.Printf("stopped %s\n", providerLabel(p))
					return nil
				}
			}
		}
	}

	// If we had no PID, just report success — the socket went away.
	fmt.Printf("stopped %s\n", providerLabel(p))
	return nil
}

// terminateProcess kills a Windows process by PID using TerminateProcess.
func terminateProcess(pid int) error {
	const processTerminate = 0x0001

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpen := kernel32.NewProc("OpenProcess")
	procTerminate := kernel32.NewProc("TerminateProcess")

	handle, _, _ := procOpen.Call(
		uintptr(processTerminate),
		0, // bInheritHandle = FALSE
		uintptr(pid),
	)
	if handle == 0 {
		return fmt.Errorf("OpenProcess failed for pid %d", pid)
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	ret, _, _ := procTerminate.Call(handle, 1)
	if ret == 0 {
		return fmt.Errorf("TerminateProcess failed for pid %d", pid)
	}
	return nil
}
