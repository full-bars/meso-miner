package main

import (
	"fmt"
	"strings"

	"github.com/urnetwork/connect"
)

// writePeerKeyCrosscheckMetrics exposes the process-wide peer public-key
// cross-check counters. The check compares the contract-delivered peer key
// against the key fetched from the platform's key API and is log-only today;
// a growing mismatch count on a host points at the platform serving
// inconsistent or substituted keys, which the log line alone cannot rate.
func writePeerKeyCrosscheckMetrics(b *strings.Builder) {
	counts := connect.PeerKeyCrossCheck()
	fmt.Fprintf(b, "# HELP urnet_peer_key_crosscheck_total Out-of-band peer public-key cross-check results: the contract-delivered peer key against the key fetched from the platform. A non-zero mismatch means the platform served inconsistent or substituted keys; the check is log-only today and the contract value is still trusted.\n# TYPE urnet_peer_key_crosscheck_total counter\n")
	fmt.Fprintf(b, "urnet_peer_key_crosscheck_total{result=\"ok\"} %d\n", counts.Ok)
	fmt.Fprintf(b, "urnet_peer_key_crosscheck_total{result=\"mismatch\"} %d\n", counts.Mismatch)
	fmt.Fprintf(b, "urnet_peer_key_crosscheck_total{result=\"fetch_failed\"} %d\n", counts.FetchFailed)
	fmt.Fprintf(b, "urnet_peer_key_crosscheck_total{result=\"wrong_length\"} %d\n", counts.WrongLength)
	fmt.Fprintf(b, "urnet_peer_key_crosscheck_total{result=\"no_key\"} %d\n", counts.NoKey)
}
