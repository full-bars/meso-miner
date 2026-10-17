package connect

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"
)

// the skip is a construction-time decision: it is stored on the manager and
// honored by the publisher, and the per-peer session layer, whose peers
// verify the published cert signature and identity proof, outranks it.

func newSkipPublicationTestClient(ctx context.Context, settings *ClientSettings) *Client {
	return &Client{
		ctx:      ctx,
		settings: settings,
		log:      DefaultLogger(),
	}
}

func TestClientKeyPublicationSkipHonored(t *testing.T) {
	cases := []struct {
		name               string
		encryptionSettings *EncryptionSettings
	}{
		{"no encryption settings", nil},
		{"encryption off", &EncryptionSettings{Mode: EncryptionModeOff}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			settings := DefaultClientSettings()
			settings.SkipClientKeyPublication = true
			settings.EncryptionSettings = tc.encryptionSettings
			manager, err := NewClientKeyManager(ctx, newSkipPublicationTestClient(ctx, settings))
			if err != nil {
				t.Fatalf("NewClientKeyManager: %v", err)
			}
			if !manager.skipPublication {
				t.Fatal("expected the skip to be honored")
			}
			// a skipped publisher sends nothing and waits on no readiness: a
			// bare client never becomes ready, so only the early return ends it
			done := make(chan struct{})
			go func() {
				manager.publishClientKey()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("publishClientKey did not return for a skipped publication")
			}
			// a rotation republishes through the same gate and also stays local
			seed := make([]byte, ed25519.SeedSize)
			seed[0] = 7
			if err := manager.SetSeed(seed); err != nil {
				t.Fatalf("SetSeed: %v", err)
			}
		})
	}
}

func TestClientKeyPublicationSkipReaderOutranks(t *testing.T) {
	cases := []struct {
		name     string
		mode     EncryptionMode
		wantSkip bool
	}{
		{"required", EncryptionModeRequired, false},
		{"opportunistic", EncryptionModeOpportunistic, false},
		{"off", EncryptionModeOff, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			settings := DefaultClientSettings()
			settings.SkipClientKeyPublication = true
			settings.EncryptionSettings = &EncryptionSettings{Mode: tc.mode}
			manager, err := NewClientKeyManager(ctx, newSkipPublicationTestClient(ctx, settings))
			if err != nil {
				t.Fatalf("NewClientKeyManager: %v", err)
			}
			if manager.skipPublication != tc.wantSkip {
				t.Fatalf("skipPublication = %v, want %v", manager.skipPublication, tc.wantSkip)
			}
		})
	}
}

func TestClientKeyPublicationPublishesByDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := DefaultClientSettings()
	settings.EncryptionSettings = &EncryptionSettings{Mode: EncryptionModeOff}
	manager, err := NewClientKeyManager(ctx, newSkipPublicationTestClient(ctx, settings))
	if err != nil {
		t.Fatalf("NewClientKeyManager: %v", err)
	}
	if manager.skipPublication {
		t.Fatal("the zero value must publish")
	}
}
