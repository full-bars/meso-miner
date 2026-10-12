// Copied from the verdict-service repository (commit 0abb8b3, package verdict). Standard library
// only, so it can be vendored here without importing the provider or connect. Keep in sync.

package verdictfeed

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientShadowAdvisoryAndFailOpen(t *testing.T) {
	pub, priv, _ := GenerateKey()
	now := time.Unix(2_000_000, 0)
	seq := uint64(10)
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			http.Error(w, "boom", 500)
			return
		}
		env, _ := Sign(priv, Verdict{State: StateOutage, Confidence: .9, IssuedAt: now.Unix(), ValidUntil: now.Add(2 * time.Minute).Unix(), Seq: seq})
		json.NewEncoder(w).Encode(env)
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL, Pub: pub, Mode: ModeShadow, Now: func() time.Time { return now }}
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Advice().Valid {
		t.Fatal("shadow mode must never give advice")
	}

	c.Mode = ModeAdvisory
	seq = 11
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a := c.Advice(); !a.Valid || a.Verdict.State != StateOutage {
		t.Fatalf("advisory advice missing: %+v", a)
	}
	// Replay of an old seq is rejected and does not clobber state.
	seq = 11
	if err := c.PollOnce(context.Background()); err != ErrReplay {
		t.Fatalf("want replay reject, got %v", err)
	}
	// Feed down: keeps last verdict until valid_until, then fails open.
	healthy = false
	_ = c.PollOnce(context.Background())
	if !c.Advice().Valid {
		t.Fatal("should keep unexpired verdict across a transient outage")
	}
	now = now.Add(3 * time.Minute)
	if c.Advice().Valid {
		t.Fatal("expired verdict must fail open")
	}
}

func TestClientRejectsWrongKey(t *testing.T) {
	_, priv, _ := GenerateKey()
	otherPub, _, _ := GenerateKey()
	now := time.Unix(2_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env, _ := Sign(priv, Verdict{State: StateOutage, IssuedAt: now.Unix(), ValidUntil: now.Add(time.Minute).Unix(), Seq: 1})
		json.NewEncoder(w).Encode(env)
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Pub: otherPub, Mode: ModeAdvisory, Now: func() time.Time { return now }}
	if err := c.PollOnce(context.Background()); err != ErrBadSignature || c.Advice().Valid {
		t.Fatalf("forged verdict accepted: %v", err)
	}
}

func TestClientNon200IsAnError(t *testing.T) {
	pub, _, _ := GenerateKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer srv.Close()
	c := &Client{URL: srv.URL, Pub: pub, Mode: ModeAdvisory}
	if err := c.PollOnce(context.Background()); err == nil {
		t.Fatal("a 503 from the feed must surface as an error so `verdictd check` fails")
	}
}

func TestLatestWorksInShadowModeButAdviceDoesNot(t *testing.T) {
	pub, priv, _ := GenerateKey()
	now := time.Unix(2_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env, _ := Sign(priv, Verdict{State: StateDegraded, Confidence: .7, IssuedAt: now.Unix(), ValidUntil: now.Add(2 * time.Minute).Unix(), Seq: 5})
		json.NewEncoder(w).Encode(env)
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Pub: pub, Mode: ModeShadow, Now: func() time.Time { return now }}
	if _, ok := c.Latest(); ok {
		t.Fatal("nothing polled yet")
	}
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Advice().Valid {
		t.Fatal("shadow Advice must stay empty")
	}
	if v, ok := c.Latest(); !ok || v.State != StateDegraded {
		t.Fatalf("Latest in shadow mode: %+v %v", v, ok)
	}
	now = now.Add(3 * time.Minute)
	if _, ok := c.Latest(); ok {
		t.Fatal("expired verdict must not be returned")
	}
}

func TestPersistedSeqBlocksReplayAfterRestart(t *testing.T) {
	pub, priv, _ := GenerateKey()
	now := time.Unix(2_000_000, 0)
	env, _ := Sign(priv, Verdict{State: StateOutage, Confidence: .9, IssuedAt: now.Unix(), ValidUntil: now.Add(2 * time.Minute).Unix(), Seq: 77})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(env) }))
	defer srv.Close()
	path := t.TempDir() + "/seq"
	c1 := &Client{URL: srv.URL, Pub: pub, Mode: ModeAdvisory, StatePath: path, Now: func() time.Time { return now }}
	if err := c1.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// a restarted consumer must refuse the same, still-valid verdict
	c2 := &Client{URL: srv.URL, Pub: pub, Mode: ModeAdvisory, StatePath: path, Now: func() time.Time { return now }}
	if err := c2.PollOnce(context.Background()); err != ErrReplay {
		t.Fatalf("restart replay: want ErrReplay, got %v", err)
	}
	// an unwritable state path never breaks polling
	c3 := &Client{URL: srv.URL, Pub: pub, Mode: ModeAdvisory, StatePath: "/nonexistent-dir/seq", Now: func() time.Time { return now }}
	if err := c3.PollOnce(context.Background()); err != nil {
		t.Fatalf("unwritable state path broke polling: %v", err)
	}
}

func TestKeyRotationAcceptsEitherKey(t *testing.T) {
	oldPub, _, _ := GenerateKey()
	newPub, newPriv, _ := GenerateKey()
	now := time.Unix(2_000_000, 0)
	env, _ := Sign(newPriv, Verdict{State: StateOK, Confidence: 1, IssuedAt: now.Unix(), ValidUntil: now.Add(time.Minute).Unix(), Seq: 1})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(env) }))
	defer srv.Close()
	pinnedOldOnly := &Client{URL: srv.URL, Pub: oldPub, Mode: ModeAdvisory, Now: func() time.Time { return now }}
	if err := pinnedOldOnly.PollOnce(context.Background()); err != ErrBadSignature {
		t.Fatalf("a key the consumer does not know must be rejected, got %v", err)
	}
	rotating := &Client{URL: srv.URL, Pub: oldPub, Pubs: []ed25519.PublicKey{newPub}, Mode: ModeAdvisory, Now: func() time.Time { return now }}
	if err := rotating.PollOnce(context.Background()); err != nil {
		t.Fatalf("rotation key not accepted: %v", err)
	}
}
