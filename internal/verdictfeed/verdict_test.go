// Copied from the verdict-service repository (commit 0abb8b3, package verdict). Standard library
// only, so it can be vendored here without importing the provider or connect. Keep in sync.

package verdictfeed

import (
	"testing"
	"time"
)

func TestRoundTripAndChecks(t *testing.T) {
	pub, priv, _ := GenerateKey()
	now := time.Unix(1_000_000, 0)
	mk := func(mut func(*Verdict)) Envelope {
		v := Verdict{State: StateDegraded, Confidence: .7, Reasons: []string{"x"},
			IssuedAt: now.Unix(), ValidUntil: now.Add(2 * time.Minute).Unix(), Seq: 5}
		if mut != nil {
			mut(&v)
		}
		e, err := Sign(priv, v)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	got, err := Verify(pub, mk(nil), now, 4)
	if err != nil || got.State != StateDegraded || got.Seq != 5 {
		t.Fatalf("good verdict rejected: %v %+v", err, got)
	}
	cases := []struct {
		name string
		env  Envelope
		now  time.Time
		last uint64
		want error
	}{
		{"replay equal seq", mk(nil), now, 5, ErrReplay},
		{"replay older seq", mk(nil), now, 9, ErrReplay},
		{"expired", mk(nil), now.Add(3 * time.Minute), 4, ErrStale},
		{"from future", mk(func(v *Verdict) {
			v.IssuedAt = now.Add(time.Hour).Unix()
			v.ValidUntil = now.Add(time.Hour + time.Minute).Unix()
		}), now, 4, ErrFromFuture},
		{"too long", mk(func(v *Verdict) { v.ValidUntil = now.Add(time.Hour).Unix() }), now, 4, ErrTooLong},
		{"zero window", mk(func(v *Verdict) { v.ValidUntil = v.IssuedAt }), now, 4, ErrTooLong},
	}
	for _, c := range cases {
		if _, err := Verify(pub, c.env, c.now, c.last); err != c.want {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
}

func TestTamperAndWrongKey(t *testing.T) {
	pub, priv, _ := GenerateKey()
	otherPub, _, _ := GenerateKey()
	now := time.Unix(1_000_000, 0)
	env, _ := Sign(priv, Verdict{State: StateOK, IssuedAt: now.Unix(), ValidUntil: now.Add(time.Minute).Unix(), Seq: 1})
	if _, err := Verify(otherPub, env, now, 0); err != ErrBadSignature {
		t.Errorf("wrong key: %v", err)
	}
	tampered := env
	tampered.Payload = `{"state":"outage"}` + env.Payload[len(`{"state":"ok"}`):]
	if _, err := Verify(pub, tampered, now, 0); err != ErrBadSignature {
		t.Errorf("tampered: %v", err)
	}
	bad := env
	bad.Sig = "!!notbase64"
	if _, err := Verify(pub, bad, now, 0); err != ErrBadSignature {
		t.Errorf("bad sig encoding: %v", err)
	}
	if _, err := Sign(priv, Verdict{State: "weird"}); err == nil {
		t.Error("signed unknown state")
	}
}
