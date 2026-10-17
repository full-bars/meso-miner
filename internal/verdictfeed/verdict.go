// Copied from the verdict-service repository (commit 0abb8b3, package verdict). Standard library
// only, so it can be vendored here without importing the provider or connect. Keep in sync.

// Package verdict defines the signed, advisory backend-health verdict and the
// checks a consumer (the provider, later) must apply before trusting it.
//
// The wire format is an Envelope whose Payload is the exact JSON text that was
// signed, so no canonicalization is needed: verify the bytes, then decode them.
package verdictfeed

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type State string

const (
	StateOK       State = "ok"
	StateDegraded State = "degraded"
	StateOutage   State = "outage"
)

// Rank orders states by severity (ok < degraded < outage). Unknown states rank
// as -1 so consumers can treat them as "no information".
func (s State) Rank() int {
	switch s {
	case StateOK:
		return 0
	case StateDegraded:
		return 1
	case StateOutage:
		return 2
	}
	return -1
}

// Verdict is advisory. It must never be the sole reason a provider throttles.
type Verdict struct {
	State      State    `json:"state"`
	Confidence float64  `json:"confidence"` // 0..1
	Reasons    []string `json:"reasons"`
	// Coverage is the share of signals that were readable (0..1) and Groups the
	// number of independent signal groups that contributed; a consumer should
	// distrust a low-coverage "ok" most of all.
	Coverage   float64 `json:"coverage"`
	Groups     int     `json:"groups"`
	IssuedAt   int64   `json:"issued_at"`   // unix seconds
	ValidUntil int64   `json:"valid_until"` // unix seconds
	Seq        uint64  `json:"seq"`         // strictly increasing per key
	KeyID      string  `json:"key_id"`
}

type Envelope struct {
	Payload string `json:"payload"` // JSON text of Verdict, exactly as signed
	Sig     string `json:"sig"`     // base64 ed25519 signature over Payload bytes
}

func KeyID(pub ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(pub[:6])
}

func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func Sign(priv ed25519.PrivateKey, v Verdict) (Envelope, error) {
	if v.State.Rank() < 0 {
		return Envelope{}, fmt.Errorf("unknown state %q", v.State)
	}
	v.KeyID = KeyID(priv.Public().(ed25519.PublicKey))
	b, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	sig := ed25519.Sign(priv, b)
	return Envelope{Payload: string(b), Sig: base64.StdEncoding.EncodeToString(sig)}, nil
}

var (
	ErrBadSignature = errors.New("verdict: bad signature")
	ErrBadPayload   = errors.New("verdict: bad payload")
	ErrStale        = errors.New("verdict: expired")
	ErrFromFuture   = errors.New("verdict: issued in the future")
	ErrReplay       = errors.New("verdict: seq not newer than last accepted")
	ErrTooLong      = errors.New("verdict: validity window too long")
)

// MaxValidity bounds how long a single verdict may claim to be valid, so a
// captured-and-replayed or mis-issued verdict cannot linger.
const MaxValidity = 10 * time.Minute

// MaxClockSkew is the tolerated difference between issuer and consumer clocks.
const MaxClockSkew = 2 * time.Minute

// Verify checks the signature against pub and then the freshness and replay
// rules. lastSeq is the highest seq the caller has already accepted (0 if
// none). On any error the verdict must be ignored (fail open to local-only).
func Verify(pub ed25519.PublicKey, env Envelope, now time.Time, lastSeq uint64) (Verdict, error) {
	sig, err := base64.StdEncoding.DecodeString(env.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize || len(pub) != ed25519.PublicKeySize {
		return Verdict{}, ErrBadSignature
	}
	if !ed25519.Verify(pub, []byte(env.Payload), sig) {
		return Verdict{}, ErrBadSignature
	}
	var v Verdict
	if err := json.Unmarshal([]byte(env.Payload), &v); err != nil || v.State.Rank() < 0 {
		return Verdict{}, ErrBadPayload
	}
	if v.KeyID != KeyID(pub) {
		return Verdict{}, ErrBadPayload
	}
	issued, until := time.Unix(v.IssuedAt, 0), time.Unix(v.ValidUntil, 0)
	if until.Sub(issued) > MaxValidity || !until.After(issued) {
		return Verdict{}, ErrTooLong
	}
	if issued.After(now.Add(MaxClockSkew)) {
		return Verdict{}, ErrFromFuture
	}
	if !now.Before(until) {
		return Verdict{}, ErrStale
	}
	if v.Seq <= lastSeq {
		return Verdict{}, ErrReplay
	}
	return v, nil
}
