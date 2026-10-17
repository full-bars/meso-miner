package main

import (
	"strings"
	"testing"

	"github.com/urnetwork/connect/internal/promtext"
)

// The cross-check block must be a lint-clean single counter family with the
// five result series, or a scrape that carries it would be rejected whole.
func TestPeerKeyCrosscheckMetricsFormat(t *testing.T) {
	var b strings.Builder
	writePeerKeyCrosscheckMetrics(&b)
	out := b.String()

	if !strings.Contains(out, "# HELP urnet_peer_key_crosscheck_total ") {
		t.Fatalf("missing HELP line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE urnet_peer_key_crosscheck_total counter") {
		t.Fatalf("missing TYPE line:\n%s", out)
	}
	for _, result := range []string{"ok", "mismatch", "fetch_failed", "wrong_length", "no_key"} {
		series := "urnet_peer_key_crosscheck_total{result=\"" + result + "\"}"
		if !strings.Contains(out, series) {
			t.Errorf("missing series %s:\n%s", series, out)
		}
	}
	for _, err := range promtext.Lint(out) {
		t.Error(err)
	}
}
