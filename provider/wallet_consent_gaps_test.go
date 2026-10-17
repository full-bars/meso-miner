// wallet_consent_gaps_test.go — the refusal edges around the seed file and the
// signature forms that the core suite does not pin: symlinks, non-regular
// files, refused encodings (including a base58 address pasted where a seed
// belongs), the signature normalization table, and the challenge window the
// operator is told about. Deterministic; no network.

package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

func TestWalletSeedFileRefusesASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs a privilege windows test hosts do not have")
	}
	dir := t.TempDir()
	real := writeSeedFile(t, dir, walletTestSeed(0x11), 0o600, true)
	link := filepath.Join(dir, ".coldkey-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	_, err := snSeedFromFile(link)
	if err == nil {
		t.Fatal("a symlinked seed file must be refused")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("the refusal should name the symlink, got: %v", err)
	}
}

func TestWalletSeedFileRefusesADirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := snSeedFromFile(dir)
	if err == nil {
		t.Fatal("a directory must be refused as a seed file")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("the refusal should say it is not a regular file, got: %v", err)
	}
}

// the accepted encodings are raw 32 bytes and 64 hex characters with an
// optional prefix; everything else, including a base58 address pasted where a
// seed belongs, is refused without echoing the contents.
func TestWalletSeedFileEncodingEdges(t *testing.T) {
	seed := walletTestSeed(0x11)
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// the uppercase prefix form is accepted
	upper := write("upper-hex", []byte("0X"+strings.ToUpper(hex.EncodeToString(seed))+"\n"))
	got, err := snSeedFromFile(upper)
	if err != nil {
		t.Fatalf("0X-prefixed hex must be accepted: %v", err)
	}
	if hex.EncodeToString(got[:]) != hex.EncodeToString(seed) {
		t.Fatalf("0X-prefixed hex decoded to %x", got)
	}

	address := walletTestAddress(t, seed)
	bad := map[string][]byte{
		"raw-too-short":  seed[:31],
		"hex-too-short":  []byte(hex.EncodeToString(seed[:31])),
		"hex-not-hex":    []byte(strings.Repeat("zz", 32)),
		"base58-address": []byte(address),
		"empty":          {},
	}
	for name, body := range bad {
		path := write(name, body)
		_, err := snSeedFromFile(path)
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if len(body) >= 8 && strings.Contains(err.Error(), string(body)) {
			t.Fatalf("%s: the refusal echoes the contents: %v", name, err)
		}
	}
}

func TestWalletSignatureNormalizationEdges(t *testing.T) {
	valid := strings.Repeat("ab", 64)
	for _, c := range []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"plain hex", valid, "0x" + valid, true},
		{"0x prefix", "0x" + valid, "0x" + valid, true},
		{"0X prefix", "0X" + valid, "0x" + valid, true},
		{"padded whitespace", "  " + valid + "\n", "0x" + valid, true},
		{"not hex", strings.Repeat("zz", 64), "", false},
		{"63 bytes", strings.Repeat("ab", 63), "", false},
		{"65 bytes", strings.Repeat("ab", 65), "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := snNormalizeSignatureHex(c.input)
			if c.ok && (err != nil || got != c.want) {
				t.Fatalf("want %q, got %q (err=%v)", c.want, got, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("expected a refusal, got %q", got)
			}
		})
	}
}

// the printed challenge must surface the signing window, the exact bytes as
// hex, and the submit command with the one-line message
func TestWalletChallengeTextSurfacesTheWindow(t *testing.T) {
	const message = "Sign in to URnetwork\nChallenge: abc_DEF-123\nTimestamp: 1760000000"
	text := snWalletChallengeText("5FakeAddress", &connect.AuthWalletChallengeResult{
		Challenge:       "abc_DEF-123",
		Timestamp:       1760000000,
		ExpiresIn:       300,
		MessageTemplate: message,
	})
	for _, want := range []string{
		message,
		"expires in 300 seconds",
		"0x" + hex.EncodeToString([]byte(message)),
		"provider wallet set 5FakeAddress --message=",
		`\n`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("challenge text is missing %q:\n%s", want, text)
		}
	}
}

// The open itself must not follow a symlink, so a path swapped for a link
// between the Lstat check and the open is refused rather than read.
func TestOpenSeedNoFollowRefusesASymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no O_NOFOLLOW on windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openSeedNoFollow(link); err == nil {
		f.Close()
		t.Fatal("openSeedNoFollow followed a symlink")
	}
	f, err := openSeedNoFollow(real)
	if err != nil {
		t.Fatalf("regular file refused: %v", err)
	}
	f.Close()
}
