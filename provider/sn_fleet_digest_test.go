// sn_fleet_digest_test.go — golden vectors for the fleet binding digest.
//
// `provider bind-head` must produce byte-identical canonical binding digests to
// sn's own fleet sign flow, because both sides are checked by the same server
// parser and signature checks. The provider does not reimplement the digest: it
// calls the sn protocol package's FleetManifest.Binding / FleetBinding.Digest.
// These vectors pin the exact bytes anyway, so a future sn pin bump that changed
// the digest construction would fail here instead of silently producing
// bindings the coordinator rejects.
//
// The expected bytes were computed by running sn's own code (not this provider)
// against the fixture below, at sn upstream 3159527ac and re-checked at the pin
// this branch carries, 23d0bf9d9 — the two agree, so the pin bump changes
// nothing. The fixture mirrors sn's miner/fleet_sign_test.go inputs: derived
// throwaway seeds and made-up ids, no real key material anywhere.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/protocol"
)

// fleetVectorSeed builds an obviously-fake 32-byte seed: start, start+1, ...
func fleetVectorSeed(start byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = start + byte(i)
	}
	return seed
}

func fleetVectorFixture(t *testing.T) (*protocol.FleetManifest, ed25519.PrivateKey, []byte) {
	t.Helper()

	clientSeed := fleetVectorSeed(0x01)
	hotkeySeed := fleetVectorSeed(0x41)
	otherSeed := fleetVectorSeed(0x81)

	clientKey := ed25519.NewKeyFromSeed(clientSeed)
	otherKey := ed25519.NewKeyFromSeed(otherSeed)

	hotkey, err := (sr25519.Scheme{}).FromSeed(hotkeySeed)
	if err != nil {
		t.Fatal(err)
	}
	var hotkeyPublic [32]byte
	copy(hotkeyPublic[:], hotkey.Public())

	clientID := [16]byte{0x01, 0x9b, 0x2e, 0x10, 0x5a, 0x7c, 0x4d, 0x3e, 0x8f, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd}
	otherID := [16]byte{0x01, 0x9b, 0x2e, 0x10, 0x5a, 0x7c, 0x4d, 0x3e, 0x8f, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xce}

	manifest := &protocol.FleetManifest{
		Schema:     protocol.FleetManifestSchema,
		ChainID:    945,
		Netuid:     25,
		Hotkey:     hotkeyPublic,
		Generation: 3,
	}
	for i := range manifest.Coordinator {
		manifest.Coordinator[i] = 0xc0 + byte(i)
	}
	for i := range manifest.FleetID {
		manifest.FleetID[i] = 0xf0 ^ byte(i)
	}
	manifest.Members = []protocol.FleetMember{
		{ClientID: clientID, ClientKey: [32]byte(clientKey.Public().(ed25519.PublicKey))},
		{ClientID: otherID, ClientKey: [32]byte(otherKey.Public().(ed25519.PublicKey))},
	}
	return manifest, clientKey, hotkeySeed
}

func fleetVectorHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFleetBindingDigestMatchesSn pins the digest and both signatures that sn's
// own code produces for the fixture.
func TestFleetBindingDigestMatchesSn(t *testing.T) {
	manifest, clientKey, hotkeySeed := fleetVectorFixture(t)
	member, err := snFleetMember(manifest, manifest.Members[0].ClientID)
	if err != nil {
		t.Fatal(err)
	}

	binding, clientSig, hotkeySig, err := snFleetBindingAndSign(
		manifest, member, 100, 200, clientKey, hotkeySeed,
	)
	if err != nil {
		t.Fatal(err)
	}

	digest, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "19685134cd9fc731d0b5c3741e04a96b88743164ef26b9fb419386121290da23"
	if got := hex.EncodeToString(digest[:]); got != wantDigest {
		t.Fatalf("binding digest drifted from sn:\n got %s\nwant %s", got, wantDigest)
	}

	const wantClientSig = "8a3e5b990f1a280f0eef9455c81dfbfd251a5ac5a9bc021770cf0a4dee1b4aafab04762d6ca8b587c1e04642d46cff87844b89aefcaddf26700fdbc849efbc0a"
	if !bytes.Equal(clientSig, fleetVectorHex(t, wantClientSig)) {
		t.Fatalf("client signature drifted from sn:\n got %x\nwant %s", clientSig, wantClientSig)
	}

	// The hotkey signature deliberately has NO byte vector. sr25519 (Schnorrkel)
	// signs with a randomized nonce: signing the same digest twice yields
	// different bytes in both crv4 and subkey, and each implementation verifies
	// the other's output. The coordinator verifies this signature rather than
	// comparing bytes, so what must hold is that it verifies against the
	// manifest hotkey, not that it equals any fixed string.
	hotkey, err := (sr25519.Scheme{}).FromSeed(hotkeySeed)
	if err != nil {
		t.Fatal(err)
	}
	if !hotkey.Verify(digest[:], hotkeySig) {
		t.Fatal("hotkey signature does not verify against the manifest hotkey")
	}
	if len(hotkeySig) != 64 {
		t.Fatalf("hotkey signature is %d bytes, want 64", len(hotkeySig))
	}
}

// TestFleetBindingDigestVariesWithInputs guards the other direction: the digest
// must move when the epochs or the member change, or the vectors above would
// pass against a digest that ignores its input.
func TestFleetBindingDigestVariesWithInputs(t *testing.T) {
	manifest, _, _ := fleetVectorFixture(t)
	member := manifest.Members[0]

	base, err := manifest.Binding(member, 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}

	laterBinding, err := manifest.Binding(member, 200, 300)
	if err != nil {
		t.Fatal(err)
	}
	laterDigest, err := laterBinding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const wantLater = "63918c042b1a891dff99e6c46a49283691ea62451f8d7aa9fba38b72207763ab"
	if got := hex.EncodeToString(laterDigest[:]); got != wantLater {
		t.Fatalf("epoch-shifted digest drifted from sn:\n got %s\nwant %s", got, wantLater)
	}
	if laterDigest == baseDigest {
		t.Fatal("digest ignores the validity epochs")
	}

	otherBinding, err := manifest.Binding(manifest.Members[1], 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	otherDigest, err := otherBinding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const wantOther = "9a5bf6322a270745a8ec91fd65537131dfcda66ff70dc626edb573a63f62a147"
	if got := hex.EncodeToString(otherDigest[:]); got != wantOther {
		t.Fatalf("second-member digest drifted from sn:\n got %s\nwant %s", got, wantOther)
	}
}

// TestFleetBindingRefusesForeignHotkey checks the guard our side adds before
// signing: a hotkey seed that is not the manifest's hotkey is refused rather
// than producing a binding the coordinator would reject.
func TestFleetBindingRefusesForeignHotkey(t *testing.T) {
	manifest, clientKey, _ := fleetVectorFixture(t)
	member := manifest.Members[0]

	if _, _, _, err := snFleetBindingAndSign(
		manifest, member, 100, 200, clientKey, fleetVectorSeed(0x81),
	); err == nil {
		t.Fatal("a hotkey seed that is not the manifest hotkey was accepted")
	}
}
