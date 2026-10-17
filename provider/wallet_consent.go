// wallet_consent.go — the signed subnet-wallet consent: `provider wallet set`
// with a coldkey proof, and `provider wallet challenge`.
//
// The main network refuses an unsigned wallet set, so `provider wallet set`
// must prove possession of the coldkey. There are two ways, and this file
// carries both:
//
//   - --coldkey_seed_file=<path>: the 32-byte sr25519 seed, raw or 64 hex
//     characters with an optional 0x prefix. The keypair is derived the way
//     subkey, polkadot-js and btcli derive it (crv4.KeypairFromSeed) and is
//     refused unless it derives <coldkey_ss58>. The seed never leaves the host
//     and is never logged. Use this only on a workstation, never on a fleet
//     node.
//   - --message=<text> --signature=<hex>: a challenge printed by
//     `provider wallet challenge <coldkey_ss58>` and signed elsewhere (btcli, a
//     hardware wallet, a browser extension). This is the recommended form for a
//     coldkey that lives off the host.
//
// The signing format is byte for byte what the server verifies: an sr25519
// signature in the "substrate" signing context over the UTF-8 challenge text
// (LF line endings, no trailing newline), wrapped in <Bytes>…</Bytes>. The
// wrapper is what a Polkadot extension's signRaw of type "bytes" signs, so the
// seed-file path signs that form too, and the server accepts either.
//
// Ported from sn's miner/sn_wallet.go. The command names, flag names, signing
// format, refusal wording and the challenge text all match upstream so an
// operator moving between the two finds the same thing. What differs is the
// plumbing: upstream drives this through the sdk's Api, and this line has no
// sdk, so the two HTTP calls go through connect.BringYourApi like every other
// sn call the provider already makes.

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// snWalletChallengePurpose is the purpose sent with the challenge request. The
// ur.io wallet bridge sends "connect" when the signature proves a subnet claim
// wallet, and the server does not pin it, but sending the same value keeps the
// request identical to the app's.
const snWalletChallengePurpose = connect.PurposeConnect

// snWalletProofHint is appended to a refusal of an unsigned wallet set.
const snWalletProofHint = `the main network requires the coldkey's signature over the wallet challenge. Either:
  provider wallet set <coldkey_ss58> --coldkey_seed_file=<path>            (signs the challenge here)
or sign it elsewhere:
  provider wallet challenge <coldkey_ss58>                                 (prints the message to sign)
  provider wallet set <coldkey_ss58> --message=<text> --signature=<hex>`

// snWalletProof is how a wallet set proves possession of the coldkey. At most
// one of the two forms is set; both empty means an unsigned set.
type snWalletProof struct {
	// SeedFile holds the coldkey's 32-byte sr25519 seed.
	SeedFile string
	// Message is the exact challenge text, or its one-line form with a literal
	// \n per line break, and Signature the coldkey's signature over it, hex
	// with an optional 0x prefix.
	Message   string
	Signature string
	// the earning interval, when the operator asked for one
	fromEpoch    *int64
	throughEpoch *int64
}

func (self *snWalletProof) unsigned() bool {
	return self == nil || (self.SeedFile == "" && self.Message == "" && self.Signature == "")
}

// snWalletProofFromOpts reads --coldkey_seed_file, --message and --signature.
func snWalletProofFromOpts(opts docopt.Opts) (*snWalletProof, error) {
	proof := &snWalletProof{}
	if seedFile, err := opts.String("--coldkey_seed_file"); err == nil {
		proof.SeedFile = strings.TrimSpace(seedFile)
	}
	if message, err := opts.String("--message"); err == nil {
		proof.Message = message
	}
	if signature, err := opts.String("--signature"); err == nil {
		proof.Signature = strings.TrimSpace(signature)
	}
	// The earning interval is carried inside the signed network consent
	// statement (wallet_network_consent.go).
	from, _ := opts.String("--wallet-from-epoch")
	through, _ := opts.String("--wallet-through-epoch")
	if (from == "") != (through == "") {
		return nil, errors.New("--wallet-from-epoch and --wallet-through-epoch go together")
	}
	if from != "" {
		first, err := strconv.ParseInt(from, 10, 64)
		if err != nil || first < 0 {
			return nil, errors.New("invalid wallet earning epoch")
		}
		last, err := strconv.ParseInt(through, 10, 64)
		if err != nil || last < first || last-first > 65535 {
			return nil, errors.New("wallet earning interval must contain at most 65536 epochs")
		}
		proof.fromEpoch, proof.throughEpoch = &first, &last
	}
	if proof.SeedFile != "" && (proof.Message != "" || proof.Signature != "") {
		return nil, errors.New("use either --coldkey_seed_file or --message with --signature, not both")
	}
	if (proof.Message == "") != (proof.Signature == "") {
		return nil, errors.New("--message and --signature go together")
	}
	return proof, nil
}

// snWalletChallenge fetches the single-use challenge for the coldkey. The route
// is public, so no jwt is required; the jwt is still attached when one exists,
// the way the app sends it.
func snWalletChallenge(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string) (*connect.AuthWalletChallengeResult, error) {
	api := connect.NewBringYourApi(ctx, clientStrategy, apiUrl)
	defer api.Close()
	result, err := api.AuthWalletChallengeSync(&connect.AuthWalletChallengeArgs{
		WalletAddress: coldkeySs58,
		Blockchain:    connect.BlockchainTao,
		Purpose:       snWalletChallengePurpose,
	})
	if err != nil {
		return nil, fmt.Errorf("wallet challenge: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("wallet challenge: %s", snPlain(result.Error.Message))
	}
	if err := snCheckChallengeMessage(result.MessageTemplate, result.Challenge); err != nil {
		return nil, err
	}
	return result, nil
}

// The server issues 32 random bytes as padded url-safe base64 (44 characters
// ending in "="); the bounds leave room for a longer challenge without letting
// anything but the base64 alphabet through.
var snChallengeMessagePattern = regexp.MustCompile(`^Sign in to URnetwork\nChallenge: ([A-Za-z0-9_-]{16,128}={0,2})\nTimestamp: ([0-9]{1,12})$`)

// a challenge lives 5 minutes; allow clock skew on top
const snChallengeClockSkew = 10 * time.Minute

// snCheckChallengeMessage refuses to sign anything but the sign-in challenge the
// server documents (three fixed-shape lines carrying the challenge and a
// timestamp). The coldkey signs whatever text this function lets through, so a
// compromised or mistaken endpoint must not be able to get a signature over an
// arbitrary message.
func snCheckChallengeMessage(message string, challenge string) error {
	if message == "" {
		return errors.New("wallet challenge: the server returned no message to sign")
	}
	match := snChallengeMessagePattern.FindStringSubmatch(message)
	if match == nil {
		return errors.New("wallet challenge: the message to sign is not the expected sign-in challenge; refusing to sign it")
	}
	// both fields come from the same server response, so equality alone proves
	// nothing; it only catches a mismatched response. An absent id is refused.
	if challenge == "" || match[1] != challenge {
		return errors.New("wallet challenge: the message to sign does not carry the issued challenge; refusing to sign it")
	}
	issued, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return errors.New("wallet challenge: the challenge timestamp is not a number; refusing to sign it")
	}
	if skew := time.Since(time.Unix(issued, 0)); skew < -snChallengeClockSkew || snChallengeClockSkew < skew {
		return errors.New("wallet challenge: the challenge timestamp is far from the local clock; refusing to sign it")
	}
	return nil
}

// snCheckSigningApiUrl refuses to sign a server challenge unless it came from
// the main API over https. The challenge text is the generic sign-in challenge,
// which the same server also accepts as a wallet login, so signing one issued
// by an endpoint an attacker controls (a changed --api_url, a saved network
// that uses http, a MITM) would hand that party a login for the coldkey.
// URNETWORK_WALLET_ALLOW_CUSTOM_API=1 lifts the check for a private test API.
func snCheckSigningApiUrl(apiUrl string) error {
	if os.Getenv("URNETWORK_WALLET_ALLOW_CUSTOM_API") == "1" {
		return nil
	}
	u, err := url.Parse(apiUrl)
	if err != nil || u.Scheme != "https" || u.Hostname() != defaultAPIHost {
		return fmt.Errorf("refusing to sign a challenge from %q: the coldkey signs only challenges from https://%s (set URNETWORK_WALLET_ALLOW_CUSTOM_API=1 only for a private test API)", snPlain(apiUrl), defaultAPIHost)
	}
	return nil
}

// snPlain strips control characters from text that came from a server or a
// flag before it is printed, so a hostile endpoint cannot inject terminal
// escape sequences.
func snPlain(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, text)
}

// snWrapBytes is the polkadot-js signRaw payload wrapper for type "bytes".
func snWrapBytes(message string) []byte {
	return []byte("<Bytes>" + message + "</Bytes>")
}

// snSignWalletChallenge signs the challenge text the way the Polkadot signRaw
// path does: sr25519 in the "substrate" signing context over the
// <Bytes>…</Bytes>-wrapped UTF-8 text. Returns the 0x-prefixed hex of the
// 64-byte signature, the form the server and the ur.io bridge use.
func snSignWalletChallenge(keypair subkey.KeyPair, message string) (string, error) {
	signature, err := keypair.Sign(snWrapBytes(message))
	if err != nil {
		return "", fmt.Errorf("sign wallet challenge: %w", err)
	}
	if len(signature) != 64 {
		return "", fmt.Errorf("sign wallet challenge: %d-byte signature", len(signature))
	}
	return "0x" + hex.EncodeToString(signature), nil
}

// snSeedFromFile reads a coldkey seed without ever creating the file, and
// refuses one that is not safely held.
//
// This reimplements the checks crv4.LoadSeedFile makes, because the sn module's
// crv4 package does not compile against this line's dependency set (it needs a
// newer scale decoder than the pinned go-substrate-rpc-client provides, and
// nothing here imported crv4 before, so it never surfaced). The checks that
// matter are kept: the file must be a regular file we own, it must not be
// readable by group or other, and it must hold exactly 32 raw bytes or 64 hex
// characters. The contents are never logged and the file is never created.
func snSeedFromFile(path string) ([32]byte, error) {
	var seed [32]byte
	info, err := os.Lstat(path)
	if err != nil {
		return seed, fmt.Errorf("seed file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return seed, fmt.Errorf("seed file %s is a symlink; give the real path", path)
	}
	// Open without following symlinks and run every check on the open
	// descriptor, then read from that same descriptor. Checking by path and
	// reading by path leaves a window in which the path can be swapped for a
	// symlink or another file between the check and the read.
	f, err := openSeedNoFollow(path)
	if err != nil {
		return seed, fmt.Errorf("seed file %s: %w", path, err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return seed, fmt.Errorf("seed file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return seed, fmt.Errorf("seed file %s is not a regular file", path)
	}
	if !snFileOwnedByCaller(info) {
		return seed, fmt.Errorf("seed file %s is owned by another user", path)
	}
	if snSeedModeTooOpen(info) {
		return seed, fmt.Errorf("seed file %s is mode %04o; it must not be readable by group or other (chmod 600)", path, info.Mode().Perm())
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return seed, fmt.Errorf("seed file %s: %w", path, err)
	}
	if len(raw) == 32 {
		copy(seed[:], raw)
		return seed, nil
	}
	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(strings.TrimPrefix(text, "0x"), "0X")
	decoded, decodeErr := hex.DecodeString(text)
	if decodeErr != nil || len(decoded) != 32 {
		return seed, fmt.Errorf("seed file %s: expected raw 32 bytes or 64 hex characters", path)
	}
	copy(seed[:], decoded)
	return seed, nil
}

// snLoadColdkey loads the coldkey seed and refuses one that does not derive the
// given address.
//
// The seed is read into a local, handed to the sr25519 scheme, and neither the
// seed bytes nor the derived private key is ever formatted into a string. Only
// the path and the derived public address appear in errors.
func snLoadColdkey(seedFile string, coldkeySs58 string, pubkey [32]byte) (subkey.KeyPair, error) {
	seed, err := snSeedFromFile(seedFile)
	if err != nil {
		return nil, err
	}
	keypair, err := (sr25519.Scheme{}).FromSeed(seed[:])
	if err != nil {
		return nil, fmt.Errorf("coldkey seed file %s: %w", seedFile, err)
	}
	derived := keypair.Public()
	if len(derived) != 32 || !bytesEqual32(derived, pubkey) {
		return nil, fmt.Errorf("coldkey seed file %s derives %s, not %s", seedFile, keypair.SS58Address(ss58.BittensorPrefix), coldkeySs58)
	}
	return keypair, nil
}

// bytesEqual32 compares a variable-length public key against a 32-byte one
// without branching on the contents.
func bytesEqual32(a []byte, b [32]byte) bool {
	if len(a) != 32 {
		return false
	}
	diff := byte(0)
	for i := range b {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// snNormalizeSignatureHex checks a signature given on the command line (64
// bytes of hex, 0x optional) and returns it 0x-prefixed.
func snNormalizeSignatureHex(signature string) (string, error) {
	clean := strings.TrimSpace(signature)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "0x"), "0X")
	raw, err := hex.DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("--signature must be hex: %w", err)
	}
	if len(raw) != 64 {
		return "", fmt.Errorf("--signature must be a 64-byte sr25519 signature, got %d bytes", len(raw))
	}
	return "0x" + hex.EncodeToString(raw), nil
}

// snEscapeMessage is the one-line form of a challenge: a literal \n per line
// break. The challenge grammar (fixed words, base64url, digits) has no
// backslashes, so it round-trips.
func snEscapeMessage(message string) string {
	return strings.ReplaceAll(message, "\n", `\n`)
}

// snUnescapeMessage inverts snEscapeMessage. A message passed with real
// newlines is left as it is.
func snUnescapeMessage(message string) string {
	if strings.Contains(message, "\n") {
		return message
	}
	return strings.ReplaceAll(message, `\n`, "\n")
}

// snWalletSetArgs turns the proof into the `POST /sn/wallet` body, fetching and
// signing the challenge for the seed-file form.
func snWalletSetArgs(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, pubkey [32]byte, proof *snWalletProof) (*connect.SnSetWalletArgs, error) {
	args := &connect.SnSetWalletArgs{ColdkeySs58: coldkeySs58}
	// The coldkey never signs the generic sign-in challenge again: that text is
	// also a wallet login and release settlement never pays it. A signed set is
	// the network consent (snSetNetworkWalletConsent); only the unsigned legacy
	// request reaches here.
	if proof != nil && !proof.unsigned() {
		return nil, errors.New("a signed wallet set is the network consent; use `provider wallet set` without --legacy-network-wallet")
	}
	return args, nil
}

// snSetWalletSigned validates the ss58 coldkey locally, proves possession of it
// and idempotently sets it as the network's subnet claim wallet via the
// authenticated `POST /sn/wallet` route. Prints the result on success.
func snSetWalletSigned(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, proof *snWalletProof) error {
	coldkeySs58 = strings.TrimSpace(coldkeySs58)
	pubkey, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("invalid ss58 coldkey %q: %s", coldkeySs58, err)
	}
	byJwt, err := readNetworkJwt()
	if err != nil {
		return err
	}
	args, err := snWalletSetArgs(ctx, clientStrategy, apiUrl, coldkeySs58, pubkey, proof)
	if err != nil {
		return err
	}
	api := connect.NewBringYourApi(ctx, clientStrategy, apiUrl)
	defer api.Close()
	api.SetByJwt(byJwt)
	result, err := api.SnSetWalletSync(args)
	if err != nil {
		return err
	}
	if result.Error != nil {
		if args.Signature == "" {
			return fmt.Errorf("%s\n%s", snPlain(result.Error.Message), snWalletProofHint)
		}
		return fmt.Errorf("%s", snPlain(result.Error.Message))
	}
	if args.Signature == "" {
		fmt.Printf("subnet wallet set to %s (pubkey 0x%x), unsigned\n", coldkeySs58, pubkey)
	} else {
		fmt.Printf("subnet wallet set to %s (pubkey 0x%x), proven by the coldkey's signature\n", coldkeySs58, pubkey)
	}
	return nil
}

// snWalletChallengeText is what `provider wallet challenge` prints: the exact
// bytes to sign, how to sign them, and the command that submits the result.
func snWalletChallengeText(coldkeySs58 string, challenge *connect.AuthWalletChallengeResult) string {
	message := challenge.MessageTemplate
	var b strings.Builder
	fmt.Fprintf(&b, "Sign this message with the coldkey %s: sr25519, \"substrate\" signing context.\n", coldkeySs58)
	fmt.Fprintf(&b, "The bytes to sign are the UTF-8 text between the markers exactly as printed (LF line endings, no trailing newline);\n")
	fmt.Fprintf(&b, "a Polkadot extension signRaw of type \"bytes\", which wraps them in <Bytes>...</Bytes>, is accepted too.\n")
	fmt.Fprintf(&b, "The challenge is single-use, bound to this coldkey, and expires in %d seconds.\n", challenge.ExpiresIn)
	fmt.Fprintf(&b, "\n----- message -----\n%s\n----- end -----\n", message)
	fmt.Fprintf(&b, "message bytes (hex): 0x%s\n", hex.EncodeToString([]byte(message)))
	fmt.Fprintf(&b, "\nThen, within that window, submit the 64-byte signature as hex:\n")
	fmt.Fprintf(&b, "  provider wallet set %s --message='%s' --signature=0x<128 hex chars>\n", coldkeySs58, snEscapeMessage(message))
	return b.String()
}

// walletChallenge implements `provider wallet challenge <coldkey_ss58>`.
func walletChallenge(opts docopt.Opts, out io.Writer) error {
	apiUrl, err := resolveApiUrl(opts)
	if err != nil {
		return fmt.Errorf("network config error: %w", err)
	}
	coldkeySs58, _ := opts.String("<coldkey_ss58>")
	coldkeySs58 = strings.TrimSpace(coldkeySs58)
	if _, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix); err != nil {
		return fmt.Errorf("invalid ss58 coldkey %q: %s", coldkeySs58, err)
	}
	proof, err := snWalletProofFromOpts(opts)
	if err != nil {
		return err
	}
	ctx, cancel := walletCommandContext()
	defer cancel()
	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	return snWalletNetworkConsentChallenge(ctx, out, clientStrategy, apiUrl, coldkeySs58, proof)
}

// walletCommandContext is the shared cancellable context of the wallet
// subcommands, matching the signal handling of the other one-shot commands.
func walletCommandContext() (context.Context, context.CancelFunc) {
	event := connect.NewEventWithContext(context.Background())
	return context.WithCancel(event.Ctx())
}
