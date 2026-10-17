// Copied from the verdict-service repository (commit 0abb8b3, package verdict). Standard library
// only, so it can be vendored here without importing the provider or connect. Keep in sync.

package verdictfeed

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mode controls whether a consumer may act on advice.
type Mode int

const (
	// ModeOff: do not poll at all.
	ModeOff Mode = iota
	// ModeShadow: poll, verify and log, but Advice() always reports no advice.
	ModeShadow
	// ModeAdvisory: Advice() reports the verified verdict. Callers may only use
	// it to nudge thresholds; they must never throttle on it alone.
	ModeAdvisory
)

// Advice is what a consumer sees. Valid=false means "no usable advice": the
// caller must behave exactly as if the feed did not exist (fail open).
type Advice struct {
	Valid   bool
	Verdict Verdict
}

// Client polls a signed verdict feed. It is safe for concurrent use and
// imports nothing from the provider, so wiring it in later is one call site.
type Client struct {
	URL string
	Pub ed25519.PublicKey
	// Pubs are additional accepted public keys, for rotation: publish with the
	// new key while consumers still pin the old one, then retire it.
	Pubs []ed25519.PublicKey
	// StatePath, when set, persists the highest accepted seq so a restart does
	// not re-accept a captured verdict that is still inside its validity window.
	StatePath string
	Mode      Mode
	Interval  time.Duration // base poll interval (default 60s)
	HTTP      *http.Client
	// Logf receives one line per poll outcome (shadow mode's whole purpose).
	Logf func(format string, args ...any)
	Now  func() time.Time

	mu sync.Mutex
	// lastSeq is the highest accepted seq per signing key (by KeyID), so a key
	// that starts counting from 1 again, or a rotation, cannot be locked out by
	// another key's higher counter. legacySeq is the single counter older
	// versions persisted; it is a floor for keys with no history of their own.
	lastSeq   map[string]uint64
	legacySeq uint64
	cur       Verdict
	have      bool
	loaded    bool
}

// Latest returns the most recent verified verdict that is still within its
// validity window, regardless of Mode. It exists so a consumer running in
// ModeShadow can log what the feed said next to its own decision without ever
// acting on it; Advice() stays the only accessor that is safe to act on.
func (c *Client) Latest() (Verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.have || !c.now().Before(time.Unix(c.cur.ValidUntil, 0)) {
		return Verdict{}, false
	}
	return c.cur, true
}

// loadSeqLocked reads the persisted seqs once. The file holds one "<key id> <seq>"
// line per key; a bare number is the older single-counter format.
func (c *Client) loadSeqLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.lastSeq == nil {
		c.lastSeq = map[string]uint64{}
	}
	if c.StatePath == "" {
		return
	}
	b, err := os.ReadFile(c.StatePath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		switch len(fields) {
		case 1:
			if n, err := strconv.ParseUint(fields[0], 10, 64); err == nil && n > c.legacySeq {
				c.legacySeq = n
			}
		case 2:
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil && n > c.lastSeq[fields[0]] {
				c.lastSeq[fields[0]] = n
			}
		}
	}
}

// seqForLocked is the highest seq accepted from the key with this id.
func (c *Client) seqForLocked(keyId string) uint64 {
	if n, ok := c.lastSeq[keyId]; ok {
		return n
	}
	return c.legacySeq
}

// saveSeqsLocked persists the per-key seqs best-effort (a read-only directory
// must never break polling).
func (c *Client) saveSeqsLocked() {
	if c.StatePath == "" {
		return
	}
	var b strings.Builder
	for keyId, seq := range c.lastSeq {
		b.WriteString(keyId + " " + strconv.FormatUint(seq, 10) + "\n")
	}
	tmp := c.StatePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		c.logf("verdict: cannot persist seq: %v", err)
		return
	}
	if err := os.Rename(tmp, c.StatePath); err != nil {
		c.logf("verdict: cannot persist seq: %v", err)
	}
}

// verifyAny accepts a verdict signed by any configured key.
func (c *Client) verifyAny(env Envelope) (Verdict, error) {
	keys := append([]ed25519.PublicKey{c.Pub}, c.Pubs...)
	var firstErr error
	for _, k := range keys {
		if len(k) != ed25519.PublicKeySize {
			continue
		}
		c.mu.Lock()
		last := c.seqForLocked(KeyID(k))
		c.mu.Unlock()
		v, err := Verify(k, env, c.now(), last)
		if err == nil {
			return v, nil
		}
		// a bad signature just means "not this key"; any other error is about
		// the verdict itself and the same for every key
		if err != ErrBadSignature && err != ErrBadPayload {
			return Verdict{}, err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = ErrBadSignature
	}
	return Verdict{}, firstErr
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) logf(f string, a ...any) {
	if c.Logf != nil {
		c.Logf(f, a...)
	}
}

// Advice returns the current verified verdict if it is still within its
// validity window, mode permits, and nothing has gone wrong; else Valid=false.
func (c *Client) Advice() Advice {
	if c.Mode != ModeAdvisory {
		return Advice{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.have || !c.now().Before(time.Unix(c.cur.ValidUntil, 0)) {
		return Advice{}
	}
	return Advice{Valid: true, Verdict: c.cur}
}

// PollOnce fetches and verifies one verdict. Any failure leaves the previously
// accepted verdict in place until its own valid_until passes.
func (c *Client) PollOnce(ctx context.Context) error {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.logf("verdict: fetch failed (failing open): %v", err)
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || resp.StatusCode != 200 {
		c.logf("verdict: bad response status=%d err=%v (failing open)", resp.StatusCode, err)
		if err == nil {
			err = fmt.Errorf("verdict: HTTP status %d", resp.StatusCode)
		}
		return err
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		c.logf("verdict: undecodable envelope (failing open): %v", err)
		return err
	}
	c.mu.Lock()
	c.loadSeqLocked()
	c.mu.Unlock()
	v, err := c.verifyAny(env)
	if err != nil {
		c.logf("verdict: rejected: %v", err)
		return err
	}
	c.mu.Lock()
	c.lastSeq[v.KeyID] = v.Seq
	c.cur, c.have = v, true
	c.saveSeqsLocked()
	c.mu.Unlock()
	c.logf("verdict: accepted state=%s conf=%.2f seq=%d mode=%d reasons=%v", v.State, v.Confidence, v.Seq, c.Mode, v.Reasons)
	return nil
}

// Run polls until ctx ends, with +/-20% jitter so a fleet never synchronizes.
func (c *Client) Run(ctx context.Context) {
	if c.Mode == ModeOff {
		return
	}
	base := c.Interval
	if base <= 0 {
		base = 60 * time.Second
	}
	for {
		_ = c.PollOnce(ctx)
		d := time.Duration(float64(base) * (0.8 + 0.4*rand.Float64()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}
