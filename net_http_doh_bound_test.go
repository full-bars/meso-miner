package connect

import (
	"net/http"
	"testing"
)

// The resolver transport bounds its connections per origin, so dials that
// net/http keeps alive after a request is cancelled cannot pile up.
func TestDohTransportBoundsConnectionsPerHost(t *testing.T) {
	settings := DefaultDohSettings()
	settings.MaxConcurrentHttpRequests = 0
	tr := httpClientWithDialer(settings, nil).Transport.(*http.Transport)
	if tr.MaxConnsPerHost != defaultMaxConcurrentHttpRequests {
		t.Fatalf("default bound: want %d got %d", defaultMaxConcurrentHttpRequests, tr.MaxConnsPerHost)
	}
	settings.MaxConcurrentHttpRequests = 4
	tr = httpClientWithDialer(settings, nil).Transport.(*http.Transport)
	if tr.MaxConnsPerHost != 4 {
		t.Fatalf("an explicit limit must win, got %d", tr.MaxConnsPerHost)
	}
}
