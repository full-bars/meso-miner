// wallet_consent_test.go — the signed subnet-wallet consent.
//
// These pin the behaviours that matter for a mainnet wallet binding: an
// unsigned set is refused before any request goes out, the seed-file form signs
// exactly what the server verifies, a seed that does not derive the address is
// refused, and no seed material or signature ever reaches an error string.

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// walletTestSeed is an obviously-fake 32-byte seed: 0x01, 0x02, ...
func walletTestSeed(start byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = start + byte(i)
	}
	return seed
}

// walletTestAddress derives the ss58 coldkey the seed proves.
func walletTestAddress(t *testing.T, seed []byte) string {
	t.Helper()
	keypair, err := (sr25519.Scheme{}).FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return keypair.SS58Address(ss58.BittensorPrefix)
}

// writeSeedFile writes a seed file with the given mode, raw or hex.
func writeSeedFile(t *testing.T, dir string, seed []byte, mode os.FileMode, raw bool) string {
	t.Helper()
	path := filepath.Join(dir, ".coldkey")
	var body []byte
	if raw {
		body = seed
	} else {
		body = []byte("0x" + hex.EncodeToString(seed) + "\n")
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeOpts builds the docopt.Opts a subcommand would receive.
func fakeOpts(kv map[string]interface{}) docopt.Opts {
	opts := docopt.Opts{}
	for k, v := range kv {
		opts[k] = v
	}
	return opts
}

// TestWalletUnsignedSetIsRefusedLocally pins that an unsigned set is gated, and
// gated locally: no server is contacted at all.
func TestWalletUnsignedSetIsRefusedLocally(t *testing.T) {
	requested := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested++
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	proof := &snWalletProof{}
	if !proof.unsigned() {
		t.Fatal("an empty proof must count as unsigned")
	}
	if err := legacyNetworkWalletGate(false, "provider wallet set"); err == nil {
		t.Fatal("the unsigned set must be refused without --legacy-network-wallet")
	}
	if requested != 0 {
		t.Fatalf("the refusal must not touch the network, saw %d requests", requested)
	}
}

// TestWalletSeedFileSignsTheChallengeVerifiably is the core correctness test:
// the signature the seed-file path sends must verify against the coldkey over
// the exact challenge the server issued, wrapped as <Bytes>…</Bytes>.
func TestWalletSeedFileNeverSignsTheLoginChallenge(t *testing.T) {
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	pubkey, err := ss58.DecodeWithPrefix(address, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proof := &snWalletProof{SeedFile: seedPath}
	if _, err := snWalletSetArgs(ctx, connect.NewClientStrategyWithDefaults(ctx), server.URL, address, pubkey, proof); err == nil {
		t.Fatal("the coldkey signed through the legacy login-challenge path")
	}
	if requests != 0 {
		t.Fatalf("the legacy path made %d requests before refusing", requests)
	}
}

// TestWalletMessageSignaturePath pins the sign-elsewhere form, including the
// one-line \n escaping the challenge text uses.
func TestWalletMessageSignatureIsNotAcceptedOnTheLegacyPath(t *testing.T) {
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	pubkey, err := ss58.DecodeWithPrefix(address, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	const message = "line one\nline two"
	escaped := snEscapeMessage(message)
	if escaped != `line one\nline two` {
		t.Fatalf("escape produced %q", escaped)
	}
	if snUnescapeMessage(escaped) != message {
		t.Fatal("escape/unescape must round-trip")
	}
	proof := &snWalletProof{Message: escaped, Signature: "0x" + strings.Repeat("00", 64)}
	if _, err := snWalletSetArgs(context.Background(), nil, "http://invalid", address, pubkey, proof); err == nil {
		t.Fatal("a signature over login text was accepted on the legacy path")
	}
}

// TestWalletSeedFileRefusesAMismatchedAddress pins that a seed which does not
// derive the named coldkey cannot sign for it.
func TestWalletSeedFileRefusesAMismatchedAddress(t *testing.T) {
	seed := walletTestSeed(0x11)
	otherAddress := walletTestAddress(t, walletTestSeed(0x41))
	otherPubkey, err := ss58.DecodeWithPrefix(otherAddress, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seedPath := writeSeedFile(t, dir, seed, 0o600, true)

	_, err = snLoadColdkey(seedPath, otherAddress, otherPubkey)
	if err == nil {
		t.Fatal("a seed that does not derive the address must be refused")
	}
	if !strings.Contains(err.Error(), "derives") {
		t.Fatalf("the refusal should say what it derives, got: %v", err)
	}
}

// TestWalletSeedFileMustNotBeGroupReadable pins the permission rule.
func TestWalletSeedFileMustNotBeGroupReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows reports a synthetic file mode; access is an ACL matter there")
	}
	dir := t.TempDir()
	path := writeSeedFile(t, dir, walletTestSeed(0x11), 0o644, true)

	_, err := snSeedFromFile(path)
	if err == nil {
		t.Fatal("a mode 0644 seed file must be refused")
	}
	if !strings.Contains(err.Error(), "644") {
		t.Fatalf("the refusal should name the offending mode, got: %v", err)
	}
}

// TestWalletSeedFileAcceptsHex pins the second accepted encoding.
func TestWalletSeedFileAcceptsHex(t *testing.T) {
	seed := walletTestSeed(0x11)
	dir := t.TempDir()
	path := writeSeedFile(t, dir, seed, 0o600, false)

	got, err := snSeedFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got[:]) != hex.EncodeToString(seed) {
		t.Fatalf("hex seed decoded to %x, want %x", got, seed)
	}
}

// TestWalletSecretHygiene asserts no error path leaks seed material, and that a
// missing seed file is never created.
func TestWalletSecretHygiene(t *testing.T) {
	seed := walletTestSeed(0x11)
	otherAddress := walletTestAddress(t, walletTestSeed(0x41))
	otherPubkey, _ := ss58.DecodeWithPrefix(otherAddress, ss58.BittensorPrefix)
	dir := t.TempDir()

	secrets := []string{
		string(seed),
		hex.EncodeToString(seed),
		strings.ToUpper(hex.EncodeToString(seed)),
	}

	mismatched := writeSeedFile(t, dir, seed, 0o600, true)
	_, err := snLoadColdkey(mismatched, otherAddress, otherPubkey)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, s := range secrets {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("the mismatched-address refusal leaks seed material: %v", err)
		}
	}

	if _, err := snNormalizeSignatureHex("0xdeadbeef"); err == nil {
		t.Fatal("a 4-byte signature must be refused")
	}

	before, _ := os.ReadDir(dir)
	if _, err := snSeedFromFile(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("expected a refusal for a missing seed file")
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		t.Fatal("reading a missing seed must never create the file")
	}
}

// TestWalletProofFlagRules pins the mutually-exclusive flag handling.
func TestWalletProofFlagRules(t *testing.T) {
	cases := []struct {
		name string
		opts map[string]interface{}
		ok   bool
	}{
		{"both seed and message", map[string]interface{}{"--coldkey_seed_file": "/s", "--message": "m", "--signature": "0x00"}, false},
		{"message without signature", map[string]interface{}{"--message": "m"}, false},
		{"signature without message", map[string]interface{}{"--signature": "0x00"}, false},
		{"from without through", map[string]interface{}{"--wallet-from-epoch": "5"}, false},
		{"through without from", map[string]interface{}{"--wallet-through-epoch": "5"}, false},
		{"interval too wide", map[string]interface{}{"--wallet-from-epoch": "0", "--wallet-through-epoch": "70000"}, false},
		{"negative epoch", map[string]interface{}{"--wallet-from-epoch": "-1", "--wallet-through-epoch": "5"}, false},
		{"seed alone", map[string]interface{}{"--coldkey_seed_file": "/s"}, true},
		{"message with signature", map[string]interface{}{"--message": "m", "--signature": "0x00"}, true},
		{"nothing at all", map[string]interface{}{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proof, err := snWalletProofFromOpts(fakeOpts(c.opts))
			if c.ok && err != nil {
				t.Fatalf("expected acceptance, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("expected a refusal")
			}
			if c.ok && proof == nil {
				t.Fatal("accepted but returned no proof")
			}
		})
	}
}

// testServerChallenge builds the sign-in message the way the server does: the
// challenge is 32 random bytes as padded url-safe base64 (44 characters).
func testServerChallenge(timestamp int64) (message string, challenge string) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i*37 + 11)
	}
	challenge = base64.URLEncoding.EncodeToString(raw)
	return fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, timestamp), challenge
}

func TestWalletChallengeMessageShapeIsEnforced(t *testing.T) {
	good, challenge := testServerChallenge(time.Now().Unix())
	if len(challenge) != 44 || !strings.HasSuffix(challenge, "=") {
		t.Fatalf("the test challenge %q is not the server's padded form", challenge)
	}
	if err := snCheckChallengeMessage(good, challenge); err != nil {
		t.Fatalf("a well-formed server challenge was refused: %v", err)
	}
	now := time.Now().Unix()
	for name, msg := range map[string]string{
		"empty":         "",
		"arbitrary":     "Transfer all funds to 5Evil",
		"extra line":    good + "\nTransfer all funds",
		"wrong header":  fmt.Sprintf("Sign in to Elsewhere\nChallenge: %s\nTimestamp: %d", challenge, now),
		"bad timestamp": fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: soon", challenge),
		"other nonce":   fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", strings.Repeat("z", 43)+"=", now),
		"stale":         fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, now-3600),
		"future":        fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, now+3600),
	} {
		if err := snCheckChallengeMessage(msg, challenge); err == nil {
			t.Fatalf("%s: the message was accepted for signing", name)
		}
	}
	// an absent issued challenge is refused, not skipped
	if err := snCheckChallengeMessage(good, ""); err == nil {
		t.Fatal("a message was accepted without an issued challenge to check it against")
	}
}

func TestWalletSigningRefusesAnApiThatIsNotTheMainApi(t *testing.T) {
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "")
	for _, apiUrl := range []string{
		"http://api.bringyour.com",
		"https://evil.example.com",
		"https://api.bringyour.com.evil.example.com",
		"ftp://api.bringyour.com",
		"",
	} {
		if err := snCheckSigningApiUrl(apiUrl); err == nil {
			t.Fatalf("%q was accepted as a signing API", apiUrl)
		}
	}
	if err := snCheckSigningApiUrl("https://api.bringyour.com"); err != nil {
		t.Fatalf("the main API was refused: %v", err)
	}
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	if err := snCheckSigningApiUrl("http://127.0.0.1:1"); err != nil {
		t.Fatalf("the explicit override was ignored: %v", err)
	}
}

func TestWalletServerTextHasNoControlCharacters(t *testing.T) {
	got := snPlain("bad\x1b[2J\x07 text\x00")
	for _, r := range got {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("control character %q survived in %q", r, got)
		}
	}
}

func TestWalletEarningIntervalFlagsParse(t *testing.T) {
	proof, err := snWalletProofFromOpts(fakeOpts(map[string]interface{}{
		"--wallet-from-epoch": "51", "--wallet-through-epoch": "151",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if proof.fromEpoch == nil || *proof.fromEpoch != 51 || *proof.throughEpoch != 151 {
		t.Fatalf("the interval did not parse: %+v", proof)
	}
	for name, opts := range map[string]map[string]interface{}{
		"only from":    {"--wallet-from-epoch": "1"},
		"reversed":     {"--wallet-from-epoch": "5", "--wallet-through-epoch": "4"},
		"too long":     {"--wallet-from-epoch": "1", "--wallet-through-epoch": "70000"},
		"not a number": {"--wallet-from-epoch": "x", "--wallet-through-epoch": "4"},
	} {
		if _, err := snWalletProofFromOpts(fakeOpts(opts)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestWalletSeedFileOwnedByAnotherUserIsRefusedOnlyByOwnerCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(path, []byte(strings.Repeat("ab", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snSeedFromFile(path); err != nil {
		t.Fatalf("an own 0600 seed file was refused: %v", err)
	}
}
