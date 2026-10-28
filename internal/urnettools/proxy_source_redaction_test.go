package urnettools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var proxySourceRedactionCases = []struct{ input, want string }{
	{"https://user:pass@api.example.com/lists/TOKEN?key=TOKEN", "https://api.example.com"},
	{"https://user:pass@api.example.com/lists/TOKEN?key=TOKEN#TOKEN", "https://api.example.com"},
	{"user:pass@proxy:1080?key=a@TOKEN", "proxy:1080"},
	{"alice:secret@proxy:bad", "proxy:bad"},
	{"https://api.example.com/lists/TOKEN", "https://api.example.com"},
	{"user:pass@?key=TOKEN", "<unparsed source>"},
	{"user:pass", "<unparsed source>"},
	{"proxy:1080", "proxy:1080"},
	{"user:pass@proxy:1080:TOKEN", "<unparsed source>"},
}

func sourceFixture(t *testing.T, input string) Provider {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(struct {
		Sources []string `json:"sources"`
	}{[]string{input}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy_url.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return Provider{StateDir: dir}
}

func checkSourceOutput(t *testing.T, out string, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("missing sanitized source %q in %q", want, out)
	}
	for _, secret := range []string{"user:pass", "alice", "secret", "TOKEN", "?key=", "/lists/"} {
		if strings.Contains(out, secret) {
			t.Errorf("source output leaked %q in %q", secret, out)
		}
	}
}

func TestDashboardProxySourcesRedaction(t *testing.T) {
	oldProcesses, oldStopped := discoverProcessesFn, discoverStoppedFn
	t.Cleanup(func() { discoverProcessesFn, discoverStoppedFn = oldProcesses, oldStopped })
	for _, tc := range proxySourceRedactionCases {
		t.Run(tc.input, func(t *testing.T) {
			p := sourceFixture(t, tc.input)
			discoverProcessesFn = func() []Provider { return []Provider{p} }
			discoverStoppedFn = func([]Provider) []Provider { return nil }
			var err error
			out := captureStdout(t, func() { err = cmdDashboard(nil) })
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "Proxy Sources") {
				t.Fatalf("missing dashboard block: %q", out)
			}
			checkSourceOutput(t, out, tc.want)
		})
	}
}

func TestPlainStatusProxySourcesRedaction(t *testing.T) {
	for _, tc := range proxySourceRedactionCases {
		t.Run(tc.input, func(t *testing.T) {
			p := sourceFixture(t, tc.input)
			out := captureStdout(t, func() { printProxyStatus(p) })
			if !strings.Contains(out, "URL sources:") {
				t.Fatalf("missing status source row: %q", out)
			}
			checkSourceOutput(t, out, tc.want)
		})
	}
}
