package connect

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
)

// Every branch of the cross-check must move exactly one counter, so the
// exported rate can be trusted (the check is log-only today).
func TestPeerKeyCrossCheckCountsEveryResult(t *testing.T) {
	sess, cleanup := newTestEncryptionSession(t, sequenceTlsRoleClient)
	defer cleanup()

	contractPub := ed25519.PublicKey(bytes.Repeat([]byte{7}, ed25519.PublicKeySize))
	otherPub := ed25519.PublicKey(bytes.Repeat([]byte{9}, ed25519.PublicKeySize))

	fields := func(counts PeerKeyCrossCheckCounts) map[string]int64 {
		return map[string]int64{
			"ok":           counts.Ok,
			"mismatch":     counts.Mismatch,
			"fetch_failed": counts.FetchFailed,
			"wrong_length": counts.WrongLength,
			"no_key":       counts.NoKey,
		}
	}
	cases := []struct {
		name  string
		fetch func(ctx context.Context) ([]byte, error)
	}{
		{"ok", func(context.Context) ([]byte, error) { return append([]byte(nil), contractPub...), nil }},
		{"mismatch", func(context.Context) ([]byte, error) { return append([]byte(nil), otherPub...), nil }},
		{"fetch_failed", func(context.Context) ([]byte, error) { return nil, errors.New("fetch refused") }},
		{"wrong_length", func(context.Context) ([]byte, error) { return []byte{1, 2, 3}, nil }},
		{"no_key", func(context.Context) ([]byte, error) { return []byte{}, nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess.peerClientPublicKeyFetcher = c.fetch
			before := PeerKeyCrossCheck()
			sess.crossCheckPeerClientPublicKey(contractPub)
			after := PeerKeyCrossCheck()

			if got := after.Started - before.Started; got != 1 {
				t.Fatalf("started moved %d, want 1", got)
			}
			beforeFields, afterFields := fields(before), fields(after)
			for name := range beforeFields {
				want := int64(0)
				if name == c.name {
					want = 1
				}
				if got := afterFields[name] - beforeFields[name]; got != want {
					t.Errorf("%s moved %d, want %d", name, got, want)
				}
			}
		})
	}
}
