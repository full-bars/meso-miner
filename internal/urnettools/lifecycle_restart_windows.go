//go:build windows

package urnettools

import (
	"fmt"
	"path/filepath"
	"time"
)

// cmdRestartWindows restarts the provider on Windows. It first tries a
// zero-downtime HotSwap via the control socket. If that fails or the
// control socket is unreachable, it falls back to stop + start.
func cmdRestartWindows(p Provider, force, dryRun bool) error {
	if controlSocketReachable(p) {
		// Try HotSwap: send {cmd: "hotswap"} to the socket. This
		// triggers the in-process binary handoff (PR #613's Windows
		// hotswap support).
		sockPath := providerSocketPath(p)
		fmt.Println("attempting zero-downtime HotSwap...")
		resp, err := sendSocketRequest(sockPath, controlRequest{Cmd: "hotswap"})
		if err == nil && resp.OK {
			// HotSwap accepted — wait for the new provider to
			// come up (poll pidIsAlive + controlSocketReachable).
			deadline := time.After(30 * time.Second)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			// Give the handoff a moment to kick off.
			time.Sleep(time.Second)

			for {
				select {
				case <-deadline:
					fmt.Println("warning: HotSwap did not complete within 30s")
					return stopStartFallback(p)
				case <-ticker.C:
					if controlSocketReachable(p) {
						fmt.Printf("restarted %s (HotSwap)\n", providerLabel(p))
						return nil
					}
				}
			}
		}
		// HotSwap failed or socket returned an error — fall through.
		if err != nil {
			fmt.Printf("HotSwap failed: %v — falling back to restart\n", err)
		} else {
			fmt.Printf("HotSwap rejected: %s — falling back to restart\n", resp.Error)
		}
	}

	return stopStartFallback(p)
}

// stopStartFallback stops the provider and starts it again.
func stopStartFallback(p Provider) error {
	fmt.Printf("stopping %s...\n", providerLabel(p))
	if err := cmdStopWindows(p, false, false); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	fmt.Printf("starting %s...\n", providerLabel(p))
	if err := cmdStartWindows(p, false, false); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Printf("restarted %s\n", providerLabel(p))
	return nil
}

// providerSocketPath returns the control socket path for a provider.
func providerSocketPath(p Provider) string {
	return filepath.Join(p.StateDir, "provider.sock")
}

// restartProviderWindows restarts the provider during an update. Same logic
// as cmdRestartWindows but without the dryRun gate — the update path already
// confirmed the restart is needed.
func restartProviderWindows(p Provider) error {
	return cmdRestartWindows(p, false, false)
}
