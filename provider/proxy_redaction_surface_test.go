package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyFileCredentialLogRedaction(t *testing.T) {
	cases := []struct{ input, want string }{
		{"alice:secret@proxy:bad", `proxy "proxy:bad (user=unparsed, password=unparsed)" missing credentials`},
		{"socks5://alice:secret@proxy:bad", `proxy "socks5://proxy:bad (user=unparsed, password=unparsed)" missing credentials`},
		{"https://user:pass@api.example.com/lists/TOKEN?key=TOKEN", `proxy "https://api.example.com (user=unparsed, password=unparsed)" missing credentials`},
		{"proxy:1080:alice:", `proxy "proxy:1080 (user=present, password=absent)" missing credentials`},
		{"proxy:1080::secret", `proxy "proxy:1080 (user=absent, password=present)" missing credentials`},
		{"alice:secret", `proxy "<unparsed address> (user=absent, password=absent)" missing credentials`},
		{"[2001:db8::1]:1080:alice:", `proxy "[2001:db8::1]:1080 (user=present, password=absent)" missing credentials`},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proxies")
			if err := os.WriteFile(path, []byte(tc.input+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			out := captureTlog(t, func() {
				settings, err := readProxySettingsFromFile(path)
				if err != nil {
					t.Error(err)
				}
				if len(settings) != 0 {
					t.Errorf("malformed source accepted: %d", len(settings))
				}
			})
			if !strings.Contains(out, tc.want) || strings.Contains(out, "alice") || strings.Contains(out, "secret") {
				t.Errorf("unsafe diagnostic: %q, want %q", out, tc.want)
			}
		})
	}
}
