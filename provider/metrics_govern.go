package main

import (
	"fmt"
	"strings"

	"github.com/urnetwork/connect"
)

// governHealthLine is the per-heartbeat governor line next to
// [health][proxies]: whether the backend reads as degraded, what the shared
// connect budget is doing about it, and how many connects died young.
func governHealthLine(s connect.GovernorStatus) string {
	return fmt.Sprintf(
		"❤️ [health][govern] mode=%s verdict=%s rate=%.1f/s waiting=%d attempts=%d connected=%d died_young=%d would_hold=%d flap_backoffs=%d",
		s.Mode, s.Verdict, s.BudgetRate, s.Waiting, s.Attempts, s.Connected, s.DiedYoung, s.WouldHold, s.FlapBackoffs,
	)
}

// writeGovernMetrics exposes the reconnect governor. The window gauges cover the
// governor's last 60 seconds.
func writeGovernMetrics(b *strings.Builder) {
	s := connect.GovernorStatusNow()
	mode := map[string]int{"off": 0, "shadow": 1, "enforce": 2}[s.Mode]
	degraded := 0
	if s.Degraded {
		degraded = 1
	}
	gauge := func(name, help string, value any) {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, value)
	}
	gauge("urnet_govern_mode", "Reconnect governor mode: 0 off, 1 shadow (observe only), 2 enforce.", mode)
	gauge("urnet_govern_degraded", "1 when the governor reads the platform backend as degraded.", degraded)
	gauge("urnet_govern_budget_rate", "Platform connect attempts per second the budget admits (200 is open).", fmt.Sprintf("%.1f", s.BudgetRate))
	gauge("urnet_govern_waiting", "Proxies waiting their turn under the connect budget.", s.Waiting)
	gauge("urnet_govern_attempts", "Platform connect attempts in the last 60s.", s.Attempts)
	gauge("urnet_govern_died_young", "Connects in the last 60s whose session died within a minute.", s.DiedYoung)
	fmt.Fprintf(b, "# HELP urnet_govern_flap_backoffs_total Reconnects held back because the previous session died young.\n# TYPE urnet_govern_flap_backoffs_total counter\nurnet_govern_flap_backoffs_total %d\n", s.FlapBackoffs)
}
