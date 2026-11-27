package connect

import (
	"net/http/httptest"
	"regexp"
	"testing"
)

// Both source policy families reach a real scrape with the label values the
// documentation names, and each series is present before any event, so a panel
// or an alert on it has data from the first scrape. The values are not asserted:
// the counters are process wide and other tests in this package move them.
func TestP2PSourceMetricsExposed(t *testing.T) {
	recorder := httptest.NewRecorder()
	PrometheusHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()

	for _, series := range []string{
		`urnet_p2p_forged_source_drops_total{kind="platform"}`,
		`urnet_p2p_forged_source_drops_total{kind="self"}`,
		`urnet_p2p_source_mismatch_total{mode="count",outcome="decoded"}`,
		`urnet_p2p_source_mismatch_total{mode="count",outcome="rejected"}`,
		`urnet_p2p_source_mismatch_total{mode="count",outcome="dropped"}`,
		`urnet_p2p_source_mismatch_total{mode="drop",outcome="dropped"}`,
	} {
		line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(series) + ` \d+$`)
		if !line.MatchString(body) {
			t.Errorf("scrape is missing the series %s", series)
		}
	}
}
