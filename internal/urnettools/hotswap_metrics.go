package urnettools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// hotswapDeclineReason maps a hotswap preflight/trigger error to a
// short metric label. The labels are intentionally terse because they
// become Prometheus label values.
func hotswapDeclineReason(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrHotSwapNotSupported):
		return "version_old"
	case errors.Is(err, ErrHotSwapUnitNotNotify):
		return "unit_not_notify"
	case strings.Contains(err.Error(), "query systemd unit type"):
		return "unit_query_error"
	case strings.Contains(err.Error(), "not yet supported on Windows"):
		return "windows"
	case strings.Contains(err.Error(), "no valid PID"):
		return "trigger_pid"
	case strings.Contains(err.Error(), "find process"):
		return "trigger_find"
	case strings.Contains(err.Error(), "send SIGUSR2"):
		return "trigger_signal"
	default:
		return "other"
	}
}

// --- Hotswap decline counters (written to provider state dir) ---

var (
	hotswapDeclineMu sync.Mutex
)

// hotswapDeclineCounts is the on-disk format for hotswap decline
// counters, persisted in <stateDir>/.hotswap_declines.json. The
// provider reads this file and exposes the values as Prometheus
// urnet_hotswap_declines_total{reason="..."} counters.
type hotswapDeclineCounts struct {
	Counts map[string]int64 `json:"counts"`
}

// recordHotswapDecline atomically increments a reason counter in the
// provider's state directory. Safe to call from the CLI update path.
// Errors are logged but never returned — metric bookkeeping must not
// abort an update.
func recordHotswapDecline(stateDir string, reason string) {
	if stateDir == "" || reason == "" {
		return
	}
	hotswapDeclineMu.Lock()
	defer hotswapDeclineMu.Unlock()

	path := filepath.Join(stateDir, ".hotswap_declines.json")

	// Read existing counts (best-effort).
	var dc hotswapDeclineCounts
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &dc)
	}
	if dc.Counts == nil {
		dc.Counts = make(map[string]int64)
	}
	dc.Counts[reason]++

	// Atomic write: write to temp, rename.
	tmp := path + ".tmp"
	data, err := json.Marshal(dc)
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// recordHotswapSuccess increments the hotswap_success counter in the
// provider's state directory. Called by the CLI when SIGUSR2 was sent
// successfully (the provider still needs to complete the handoff).
func recordHotswapSuccess(stateDir string) {
	if stateDir == "" {
		return
	}
	hotswapDeclineMu.Lock()
	defer hotswapDeclineMu.Unlock()

	path := filepath.Join(stateDir, ".hotswap_declines.json")

	var dc hotswapDeclineCounts
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &dc)
	}
	if dc.Counts == nil {
		dc.Counts = make(map[string]int64)
	}
	dc.Counts["success"]++

	tmp := path + ".tmp"
	data, err := json.Marshal(dc)
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// readHotswapDeclines reads the decline counters from the provider's
// state directory. Returns nil when the file does not exist. Used by
// the provider's Prometheus metrics endpoint.
func readHotswapDeclines(stateDir string) map[string]int64 {
	if stateDir == "" {
		return nil
	}
	path := filepath.Join(stateDir, ".hotswap_declines.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var dc hotswapDeclineCounts
	if err := json.Unmarshal(data, &dc); err != nil || dc.Counts == nil {
		return nil
	}
	return dc.Counts
}
