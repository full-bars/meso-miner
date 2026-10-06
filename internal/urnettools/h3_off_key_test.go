package urnettools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// `urnet-tools set h3 off` must be sent to the provider as the VALUE "off",
// not as the generic clear. The clear path drops the override and re-applies
// the provider's live default for the key, and for h3 that default follows
// URNETWORK_H3. So on a box started with URNETWORK_H3=on, `set h3 off` cleared
// the override, the provider re-read the environment, and the H3 gate came
// back on at the next start, the opposite of the kill switch the help text
// documents. The provider-side h3 test sets the persisted state directly and
// so never saw the CLI-side gap.
func TestH3OffIsSentAsAValueNotAClear(t *testing.T) {
	for _, key := range []string{"h3", "H3"} {
		c, ok := canonicalControlKey(key)
		if !ok || c != "h3" {
			t.Fatalf("canonicalControlKey(%q) = %q, %v; want h3", key, c, ok)
		}
		if treatsOffAsClear(c) {
			t.Errorf("%q maps to h3, which must NOT take the off-as-clear path: "+
				"clearing reverts to URNETWORK_H3, which may be on", key)
		}
	}
}

// The caller, not just the lookup helper: with the provider down, `set h3 off`
// must queue a "set h3=off" op rather than a "clear".
func TestSetH3OffQueuesAValueThroughTheCaller(t *testing.T) {
	dir := t.TempDir()
	if err := applySetOverride(Provider{StateDir: dir}, "h3", "off", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pending_overrides.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ops []pendingOp
	if err := json.Unmarshal(data, &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Op != "set" || ops[0].Key != "h3" || ops[0].Value != "off" {
		t.Fatalf("queued ops = %+v, want one set h3=off", ops)
	}
}
