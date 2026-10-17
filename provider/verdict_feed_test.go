package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/urnetwork/connect/internal/verdictfeed"
)

func TestVerdictFeedDefaultsToOff(t *testing.T) {
	t.Setenv("URNETWORK_VERDICT_FEED", "")
	if c := startVerdictFeed(context.Background()); c != nil {
		t.Fatal("the feed must be off unless asked for")
	}
}

func TestVerdictFeedIgnoresUnknownMode(t *testing.T) {
	t.Setenv("URNETWORK_VERDICT_FEED", "bogus")
	if c := startVerdictFeed(context.Background()); c != nil {
		t.Fatal("an unknown mode must leave the feed off")
	}
}

func TestVerdictFeedBuiltInKeyIsAValidEd25519Key(t *testing.T) {
	b, err := base64.StdEncoding.DecodeString(verdictFeedPubKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		t.Fatalf("a malformed built-in key would silently disable the feed: %v len=%d", err, len(b))
	}
}

// The environment wiring end to end: shadow mode, a URL override and an extra
// rotation key accept a verdict signed by that key, and shadow mode still never
// gives actionable advice.
func TestVerdictFeedShadowEndToEnd(t *testing.T) {
	pub, priv, _ := verdictfeed.GenerateKey()
	now := time.Now()
	env, _ := verdictfeed.Sign(priv, verdictfeed.Verdict{State: verdictfeed.StateDegraded, Confidence: 0.8, Coverage: 0.9,
		IssuedAt: now.Unix(), ValidUntil: now.Add(2 * time.Minute).Unix(), Seq: uint64(now.UnixMilli())})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(env) }))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("URNETWORK_VERDICT_FEED", "shadow")
	t.Setenv("URNETWORK_VERDICT_FEED_URL", srv.URL)
	t.Setenv("URNETWORK_VERDICT_FEED_KEYS", base64.StdEncoding.EncodeToString(pub))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startVerdictFeed(ctx)
	if c == nil {
		t.Fatal("shadow mode did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if v, ok := c.Latest(); ok {
			if v.State != verdictfeed.StateDegraded || v.Coverage != 0.9 {
				t.Fatalf("unexpected verdict: %+v", v)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the feed was never accepted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c.Advice().Valid {
		t.Fatal("shadow mode must never give actionable advice")
	}
}
