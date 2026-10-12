package connect

import "sync/atomic"

// The platform speed-tests each new transport: it sends a control frame, then
// floods the websocket with 512 KiB (twice), and the provider echoes every byte
// back so the server can time the link. The result feeds provider ranking.
//
// That is about 2 MiB each way per connect on a metered proxy for zero billable
// payload, small per connection but it scales with connects, so a mass reconnect
// is a bandwidth event (1,100 proxies measured about 1.2 GiB each way). The connect
// attempt budget already bounds that by bounding connects.
//
// Do NOT drop or ration the echo. It was tried: a test that never comes back is
// retried by the server, and a provider that dropped its echoes got ten times as
// many tests (21k in 11 minutes against 2.3k) and ten times the inbound bytes.
// These counters exist so the cost is visible.

var speedEchoCounters struct {
	tests  atomic.Int64
	echoed atomic.Int64
}

// SpeedEchoStats is a snapshot of the process-wide speed test counters.
type SpeedEchoStats struct {
	Tests       int64
	EchoedBytes int64
}

func SpeedEchoStatsNow() SpeedEchoStats {
	return SpeedEchoStats{
		Tests:       speedEchoCounters.tests.Load(),
		EchoedBytes: speedEchoCounters.echoed.Load(),
	}
}

func recordSpeedTestStart()      { speedEchoCounters.tests.Add(1) }
func recordSpeedEchoBytes(n int) { speedEchoCounters.echoed.Add(int64(n)) }
