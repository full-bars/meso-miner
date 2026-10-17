package connect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every marker must appear in a non-test source file, so rewording an emit site
// without updating governEventMarkers fails here.
func TestGovernEventMarkersAreEmitted(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "govern_event.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(b)
	}
	for _, m := range governEventMarkers {
		if !strings.Contains(source.String(), m.marker) {
			t.Errorf("marker %q (%s) is not emitted by any source file: reworded?", m.marker, m.kind)
		}
	}
}

func TestClassifyGovernLineUsesTheRightModeSource(t *testing.T) {
	defer setGovernModeForTest(governShadow)()
	defer setDeadClientModeForTest(governEnforce)()

	// a real release under dead-client enforce is enforce, even with GOVERN=shadow
	kind, mode := ClassifyGovernLine("[govern] released dead client abc: silent 3m")
	if kind != "dead_client_release" || mode != "enforce" {
		t.Errorf("got %s/%s", kind, mode)
	}
	// the shadow line says so itself
	if _, mode := ClassifyGovernLine("[govern] dead client abc silent 3m; shadow, not released"); mode != "shadow" {
		t.Errorf("shadow line mode = %s", mode)
	}
	// everything else follows the governor mode
	if kind, mode := ClassifyGovernLine("[govern] budget cut from 200.0/s to 20.0/s"); kind != "budget_cut" || mode != "shadow" {
		t.Errorf("got %s/%s", kind, mode)
	}
	if kind, _ := ClassifyGovernLine("[t]mass disconnect: 40 transports closed"); kind != GovernKindObservation {
		t.Errorf("mass disconnect kind = %s", kind)
	}
	if kind, _ := ClassifyGovernLine("[govern] something new"); kind != "other" {
		t.Errorf("unknown kind = %s", kind)
	}
}

func TestEmitGovernLineHandsEventToSink(t *testing.T) {
	var got []GovernEvent
	SetGovernEventSink(func(e GovernEvent) { got = append(got, e) })
	defer SetGovernEventSink(nil)
	emitGovernLine("[govern] budget reopened to 200.0/s after being throttled for 5m")
	if len(got) != 1 || got[0].Kind != "budget_reopen" || !strings.HasPrefix(got[0].Line, "[govern]") {
		t.Errorf("got %+v", got)
	}
	SetGovernEventSink(nil)
	emitGovernLine("[govern] budget cut x") // no sink: must not panic
}
