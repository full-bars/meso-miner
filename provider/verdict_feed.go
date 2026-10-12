package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/internal/verdictfeed"
)

// The verdict feed is a signed, advisory backend-health reading published by the
// fleet operator (https://urstatus.fullbars.xyz/verdict.json). In this release it
// is OBSERVATION ONLY: with URNETWORK_VERDICT_FEED=shadow the provider polls and
// verifies it and shows what it says next to its own verdict on the [govern]
// lines (feed=ok|degraded|outage feed_conf= feed_cov=). Nothing acts on it. Any
// failure (unreachable, expired, bad signature, replay) reads as feed=none, i.e.
// exactly as if the feed did not exist.
//
//	URNETWORK_VERDICT_FEED      off (default) | shadow ("advisory" is accepted but behaves as shadow)
//	URNETWORK_VERDICT_FEED_URL  override the feed URL
//	URNETWORK_VERDICT_FEED_KEYS comma-separated base64 ed25519 public keys (adds to the built-in key, for rotation)
const (
	verdictFeedDefaultURL = "https://urstatus.fullbars.xyz/verdict.json"
	// key id OluS2ZH6, the verdict service's signing key
	verdictFeedPubKey = "OluS2ZH6RlsYtcvpuEnyPotbY/NZfQfXK0+U0Vxsv1c="
)

func startVerdictFeed(ctx context.Context) *verdictfeed.Client {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("URNETWORK_VERDICT_FEED")))
	switch mode {
	case "", "off", "0", "false", "no":
		return nil
	case "shadow", "advisory":
	default:
		tlog("[verdict] unknown URNETWORK_VERDICT_FEED=%q, the feed stays off\n", mode)
		return nil
	}
	if mode == "advisory" {
		tlog("[verdict] URNETWORK_VERDICT_FEED=advisory: nothing in this release acts on the feed, running as shadow\n")
	}
	pub, err := base64.StdEncoding.DecodeString(verdictFeedPubKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		tlog("[verdict] built-in public key is invalid, the feed stays off\n")
		return nil
	}
	var extra []ed25519.PublicKey
	for _, k := range strings.Split(os.Getenv("URNETWORK_VERDICT_FEED_KEYS"), ",") {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) != ed25519.PublicKeySize {
			tlog("[verdict] ignoring a malformed key in URNETWORK_VERDICT_FEED_KEYS\n")
			continue
		}
		extra = append(extra, ed25519.PublicKey(b))
	}
	url := verdictFeedDefaultURL
	if u := strings.TrimSpace(os.Getenv("URNETWORK_VERDICT_FEED_URL")); u != "" {
		url = u
	}
	statePath := ""
	if home, err := os.UserHomeDir(); err == nil {
		statePath = filepath.Join(home, ".urnetwork", "verdict_seq")
	}
	client := &verdictfeed.Client{
		URL:       url,
		Pub:       ed25519.PublicKey(pub),
		Pubs:      extra,
		Mode:      verdictfeed.ModeShadow,
		Interval:  60 * time.Second,
		StatePath: statePath,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		Logf:      newVerdictFeedLogger(),
	}
	go client.Run(ctx)
	connect.SetGovernFeed(func() connect.FeedAdvice {
		v, ok := client.Latest()
		if !ok {
			return connect.FeedAdvice{}
		}
		return connect.FeedAdvice{Valid: true, State: string(v.State), Confidence: v.Confidence, Coverage: v.Coverage}
	})
	tlog("[verdict] feed shadow mode on (observing only, never acts) url=%s\n", url)
	return client
}

// newVerdictFeedLogger keeps the feed quiet: one line per state change, and
// failures at most once every 10 minutes. The client logs every poll.
func newVerdictFeedLogger() func(string, ...any) {
	var mu sync.Mutex
	lastState := ""
	var lastFail time.Time
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(format, "accepted state=") {
			state := ""
			if 0 < len(args) {
				state = fmt.Sprint(args[0])
			}
			if state == lastState {
				return
			}
			lastState = state
			tlog("[verdict] "+strings.TrimPrefix(format, "verdict: ")+"\n", args...)
			return
		}
		if time.Since(lastFail) < 10*time.Minute {
			return
		}
		lastFail = time.Now()
		tlog("[verdict] "+strings.TrimPrefix(format, "verdict: ")+"\n", args...)
	}
}
