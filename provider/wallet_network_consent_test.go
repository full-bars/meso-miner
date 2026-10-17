package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// Synthetic identities only: placeholder ids, a synthetic operator key and a
// seeded coldkey.

var consentTestUser = [16]byte{7}
var consentTestNetwork = [16]byte{9}

func consentTestOperator(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func consentTestEpoch() *connect.SnEpochResult {
	return &connect.SnEpochResult{
		Epoch:                  50,
		ChainId:                964,
		ContractAddress:        "0x00000000000000000000000000000000000000c1",
		SettlementVaultAddress: "0x00000000000000000000000000000000000000d2",
		GenesisHash:            "0x" + strings.Repeat("ab", 32),
		Netuid:                 "25",
		NoId:                   "3",
	}
}

func consentTestExpect(t testing.TB, seed []byte, now time.Time) snNetworkConsentExpect {
	t.Helper()
	address := walletTestAddress(&testing.T{}, seed)
	pubkey, err := ss58.DecodeWithPrefix(address, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return snNetworkConsentExpect{
		Coldkey:      pubkey,
		Identity:     snNetworkIdentity{UserId: consentTestUser, NetworkId: consentTestNetwork},
		FromEpoch:    51,
		ThroughEpoch: 151,
		Epoch:        consentTestEpoch(),
		Now:          now,
	}
}

// consentTestMessage builds a statement that matches the expectation, lets the
// caller change it, signs it as the operator and returns its message.
func consentTestMessage(t testing.TB, want snNetworkConsentExpect, mutate func(*protocol.NetworkWalletMappingStatement)) string {
	t.Helper()
	epoch := want.Epoch
	genesis, _ := hex.DecodeString(strings.TrimPrefix(epoch.GenesisHash, "0x"))
	statement := protocol.NetworkWalletMappingStatement{
		Schema: protocol.NetworkWalletMappingConsentSchema,
		Scope:  protocol.NetworkWalletMappingScope,
		Domain: protocol.ClientKeyHistoryDomain{
			ChainID:          epoch.ChainId,
			GenesisHash:      [32]byte(genesis),
			Netuid:           25,
			Coordinator:      common.HexToAddress(epoch.ContractAddress),
			SettlementVault:  common.HexToAddress(epoch.SettlementVaultAddress),
			DeploymentIDHash: [32]byte{1},
			PolicyHash:       [32]byte{2},
			NoID:             3,
		},
		UserId:       want.Identity.UserId,
		NetworkId:    want.Identity.NetworkId,
		Coldkey:      want.Coldkey,
		Generation:   1,
		Nonce:        [32]byte{10},
		IssuedAt:     want.Now.Unix(),
		ExpiresAt:    want.Now.Add(300 * time.Second).Unix(),
		FromEpoch:    want.FromEpoch,
		ThroughEpoch: want.ThroughEpoch,
	}
	if mutate != nil {
		mutate(&statement)
	}
	boundary := protocol.ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}
	if err := protocol.SignProspectiveNetworkWalletMapping(&statement, boundary, consentTestOperator(t)); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func TestNetworkConsentVerifierAcceptsTheMatchingStatement(t *testing.T) {
	want := consentTestExpect(t, walletTestSeed(0x11), time.Now())
	statement, err := snVerifyNetworkConsentMessage(consentTestMessage(t, want, nil), want)
	if err != nil {
		t.Fatalf("a matching statement was refused: %v", err)
	}
	if statement.FromEpoch != 51 || statement.ThroughEpoch != 151 {
		t.Fatalf("unexpected interval %d..%d", statement.FromEpoch, statement.ThroughEpoch)
	}
}

// Every disagreement between the statement and what the signer knows is
// refused before the coldkey is touched.
func TestNetworkConsentVerifierRefusalMatrix(t *testing.T) {
	now := time.Now()
	want := consentTestExpect(t, walletTestSeed(0x11), now)
	other := consentTestExpect(t, walletTestSeed(0x55), now)

	mutations := map[string]func(*protocol.NetworkWalletMappingStatement){
		"another coldkey":        func(s *protocol.NetworkWalletMappingStatement) { s.Coldkey = other.Coldkey },
		"another owner":          func(s *protocol.NetworkWalletMappingStatement) { s.UserId = [16]byte{99} },
		"another network":        func(s *protocol.NetworkWalletMappingStatement) { s.NetworkId = [16]byte{98} },
		"another interval start": func(s *protocol.NetworkWalletMappingStatement) { s.FromEpoch = 52 },
		"another interval end":   func(s *protocol.NetworkWalletMappingStatement) { s.ThroughEpoch = 152 },
		"another chain":          func(s *protocol.NetworkWalletMappingStatement) { s.Domain.ChainID = 1 },
		"another genesis":        func(s *protocol.NetworkWalletMappingStatement) { s.Domain.GenesisHash = [32]byte{5} },
		"another netuid":         func(s *protocol.NetworkWalletMappingStatement) { s.Domain.Netuid = 26 },
		"another coordinator":    func(s *protocol.NetworkWalletMappingStatement) { s.Domain.Coordinator = common.Address{1} },
		"another vault":          func(s *protocol.NetworkWalletMappingStatement) { s.Domain.SettlementVault = common.Address{2} },
		"another operator id":    func(s *protocol.NetworkWalletMappingStatement) { s.Domain.NoID = 4 },
		"issued in the future": func(s *protocol.NetworkWalletMappingStatement) {
			s.IssuedAt = now.Add(time.Hour).Unix()
			s.ExpiresAt = s.IssuedAt + 300
		},
		"already expired": func(s *protocol.NetworkWalletMappingStatement) {
			s.IssuedAt = now.Add(-time.Hour).Unix()
			s.ExpiresAt = s.IssuedAt + 300
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			// interval mutations must still pass the protocol's own bounds
			message := consentTestMessage(t, want, mutate)
			if _, err := snVerifyNetworkConsentMessage(message, want); err == nil {
				t.Fatal("the statement was accepted for signing")
			}
		})
	}

	// not a consent at all
	login := "Sign in to URnetwork\nChallenge: " + strings.Repeat("a", 43) + "=\nTimestamp: " + fmt.Sprint(now.Unix())
	for name, message := range map[string]string{
		"a login challenge":     login,
		"empty":                 "",
		"the provider prefix":   strings.Replace(consentTestMessage(t, want, nil), protocol.NetworkWalletMappingConsentPrefix, protocol.WalletMappingConsentPrefix, 1),
		"trailing data":         consentTestMessage(t, want, nil) + "\n",
		"a different JSON body": protocol.NetworkWalletMappingConsentPrefix + `{"schema":"x"}`,
	} {
		if _, err := snVerifyNetworkConsentMessage(message, want); err == nil {
			t.Fatalf("%s: accepted for signing", name)
		}
	}

	// a consent starting at or before the current epoch is not prospective
	late := want
	late.Epoch = consentTestEpoch()
	late.Epoch.Epoch = 51
	if _, err := snVerifyNetworkConsentMessage(consentTestMessage(t, want, nil), late); err == nil {
		t.Fatal("a statement starting at the current epoch was accepted")
	}

	// the subnet identity the epoch route cannot state is a reason to stop
	for name, strip := range map[string]func(*connect.SnEpochResult){
		"no vault":   func(e *connect.SnEpochResult) { e.SettlementVaultAddress = "" },
		"no genesis": func(e *connect.SnEpochResult) { e.GenesisHash = "" },
		"no netuid":  func(e *connect.SnEpochResult) { e.Netuid = "" },
		"no op id":   func(e *connect.SnEpochResult) { e.NoId = "" },
	} {
		stripped := want
		stripped.Epoch = consentTestEpoch()
		strip(stripped.Epoch)
		if _, err := snVerifyNetworkConsentMessage(consentTestMessage(t, want, nil), stripped); err == nil {
			t.Fatalf("%s: accepted without a way to verify the domain", name)
		}
	}

	// the epoch route may point at the v2 vault after the migration; the v1
	// domain vault is still accepted
	moved := want
	moved.Epoch = consentTestEpoch()
	moved.Epoch.SettlementVaultAddress = "0x00000000000000000000000000000000000000e3"
	v1 := consentTestMessage(t, want, func(s *protocol.NetworkWalletMappingStatement) {
		s.Domain.SettlementVault = common.HexToAddress(snKnownV1SettlementVault)
	})
	if _, err := snVerifyNetworkConsentMessage(v1, moved); err != nil {
		t.Fatalf("the v1 domain vault was refused after the epoch route moved: %v", err)
	}
}

func TestNetworkJwtIdentityRefusesAClientJwt(t *testing.T) {
	user := connect.Id(consentTestUser)
	network := connect.Id(consentTestNetwork)
	good := createFakeJWTWithClaims(map[string]interface{}{"user_id": user.String(), "network_id": network.String()})
	identity, err := snNetworkJwtIdentity(good)
	if err != nil || identity.UserId != consentTestUser || identity.NetworkId != consentTestNetwork {
		t.Fatalf("the network jwt did not parse: %v %+v", err, identity)
	}
	client := createFakeJWTWithClaims(map[string]interface{}{"user_id": user.String(), "network_id": network.String(), "client_id": network.String()})
	if _, err := snNetworkJwtIdentity(client); err == nil {
		t.Fatal("a client jwt was accepted for a network consent")
	}
	if _, err := snNetworkJwtIdentity(createFakeJWTWithClaims(map[string]interface{}{"network_id": network.String()})); err == nil {
		t.Fatal("a jwt without an owner was accepted")
	}
}

// consentServer is a fake platform: it issues the verified statement, stores
// what it is sent, and can lose the acknowledgement of the first submission.
type consentServer struct {
	t             *testing.T
	want          snNetworkConsentExpect
	loseFirstAck  bool
	badReceipt    bool
	challenges    int
	submissions   []connect.SnSetWalletArgs
	submitCounter int
}

func (self *consentServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sn/epoch":
			_ = json.NewEncoder(w).Encode(consentTestEpoch())
		case "/sn/wallet/network-consent":
			self.challenges++
			var args connect.SnNetworkWalletConsentArgs
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				self.t.Error(err)
			}
			// like the server, issue exactly the interval asked for
			asked := self.want
			asked.FromEpoch, asked.ThroughEpoch = args.FromEpoch, args.ThroughEpoch
			asked.Now = time.Now() // the server stamps each statement when it issues it
			message := consentTestMessage(self.t, asked, nil)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": message})
		case "/sn/wallet":
			var args connect.SnSetWalletArgs
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				self.t.Error(err)
			}
			self.submissions = append(self.submissions, args)
			self.submitCounter++
			if self.loseFirstAck && self.submitCounter == 1 {
				// the platform stored it but the answer never arrives
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			signature, _ := hex.DecodeString(strings.TrimPrefix(args.Signature, "0x"))
			_, hash, err := protocol.VerifyNetworkWalletMappingConsent(context.Background(), protocol.WalletMappingConsent{Message: args.Message, Signature: [64]byte(signature)})
			if err != nil {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"code": "signature_mismatch", "message": "bad"}})
				return
			}
			receipt := hex.EncodeToString(hash[:])
			if self.badReceipt {
				receipt = strings.Repeat("0", 64)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"mapping_hash": receipt, "mapping_generation": 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func consentTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	user := connect.Id(consentTestUser)
	network := connect.Id(consentTestNetwork)
	jwt := createFakeJWTWithClaims(map[string]interface{}{"user_id": user.String(), "network_id": network.String()})
	dir := filepath.Join(home, ".urnetwork")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jwt"), []byte(jwt), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runConsentSet(t *testing.T, server *httptest.Server, address string, seedPath string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return snSetNetworkWalletConsent(ctx, connect.NewClientStrategyWithDefaults(ctx), server.URL, address, &snWalletProof{SeedFile: seedPath})
}

func TestNetworkConsentSetStoresTheVerifiedConsent(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now())}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	if err := runConsentSet(t, server, address, seedPath); err != nil {
		t.Fatal(err)
	}
	if len(fake.submissions) != 1 {
		t.Fatalf("%d submissions, want 1", len(fake.submissions))
	}
	if fake.submissions[0].ClientId != nil {
		t.Fatal("a network consent must not carry a client id")
	}
	path, _ := snConsentJournalPath()
	entry, err := snConsentJournalLoad(path, server.URL, address)
	if err != nil || entry == nil || !entry.Acked {
		t.Fatalf("the consent is not retained as acknowledged: %v %+v", err, entry)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the journal is not private: %v %v", err, info)
	}
}

// A lost acknowledgement is answered by the exact original: the same message
// and the same randomized signature, with no second challenge and no second
// signature.
func TestNetworkConsentReplaysTheRetainedOriginalAfterALostAcknowledgement(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now()), loseFirstAck: true}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	if err := runConsentSet(t, server, address, seedPath); err == nil {
		t.Fatal("a lost acknowledgement was reported as stored")
	}
	if err := runConsentSet(t, server, address, seedPath); err != nil {
		t.Fatalf("the replay failed: %v", err)
	}
	if len(fake.submissions) != 2 {
		t.Fatalf("%d submissions, want 2", len(fake.submissions))
	}
	if fake.challenges != 1 {
		t.Fatalf("the replay asked for a new challenge (%d)", fake.challenges)
	}
	first, second := fake.submissions[0], fake.submissions[1]
	if first.Message != second.Message || first.Signature != second.Signature {
		t.Fatal("the replay was not byte-identical to the retained original")
	}
}

func TestNetworkConsentRefusesAReceiptThatDoesNotNameTheConsent(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now()), badReceipt: true}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	if err := runConsentSet(t, server, address, seedPath); err == nil {
		t.Fatal("a receipt for another consent was accepted")
	}
	path, _ := snConsentJournalPath()
	if entry, _ := snConsentJournalLoad(path, server.URL, address); entry == nil || entry.Acked {
		t.Fatal("a refused receipt must leave the consent unacknowledged for a replay")
	}
}

// The key is never loaded for a statement that fails verification.
func TestNetworkConsentNeverLoadsTheKeyForABadStatement(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	// a seed file that cannot be read: any attempt to load it would error with
	// its path, which a verification failure must come before
	missing := filepath.Join(t.TempDir(), "missing-seed")
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now())}
	other := consentTestExpect(t, walletTestSeed(0x55), time.Now())
	fake.want.Coldkey = other.Coldkey // the server names another coldkey
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	err := runConsentSet(t, server, address, missing)
	if err == nil || strings.Contains(err.Error(), "missing-seed") {
		t.Fatalf("the key was loaded before the statement was verified: %v", err)
	}
	if len(fake.submissions) != 0 {
		t.Fatal("something was sent for a statement that failed verification")
	}
}

// The challenge text must carry the message as hex too, so a browser signer
// with a one-line data field (Polkadot-js Sign and verify with a wallet
// extension) can sign it, and the submit command must carry the interval.
func TestNetworkConsentChallengeTextCarriesTheHexAndTheSubmitCommand(t *testing.T) {
	consentTestHome(t)
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now())}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out strings.Builder
	proof := &snWalletProof{}
	if err := snWalletNetworkConsentChallenge(ctx, &out, connect.NewClientStrategyWithDefaults(ctx), server.URL, address, proof); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"message bytes (hex",
		"provider wallet set " + address,
		"--wallet-from-epoch=51",
		"--signature=0x",
		"Approve URnetwork network wallet mapping",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the challenge text lacks %q:\n%s", want, text)
		}
	}
	if fake.challenges != 1 || len(fake.submissions) != 0 {
		t.Fatalf("a challenge must request once and sign nothing (%d challenges, %d submissions)", fake.challenges, len(fake.submissions))
	}
}

func TestNetworkConsentIntervalGuards(t *testing.T) {
	epoch := consentTestEpoch() // epoch 50
	interval := func(from, through int64) *snWalletProof {
		return &snWalletProof{fromEpoch: &from, throughEpoch: &through}
	}
	if from, through, err := snConsentInterval(nil, epoch); err != nil || from != 51 || through != 51+65535 {
		t.Fatalf("the default interval is %d..%d (%v)", from, through, err)
	}
	if _, _, err := snConsentInterval(interval(52, 52+65535), epoch); err != nil {
		t.Fatalf("a full interval one epoch later was refused: %v", err)
	}
	for name, proof := range map[string]*snWalletProof{
		"a typo far in the future": interval(500, 500+65535),
		"a short interval":         interval(51, 151),
	} {
		if _, _, err := snConsentInterval(proof, epoch); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	t.Setenv("URNETWORK_WALLET_ALLOW_ODD_INTERVAL", "1")
	if _, _, err := snConsentInterval(interval(51, 151), epoch); err != nil {
		t.Fatalf("the explicit override was ignored: %v", err)
	}
}

// A retained, unacknowledged consent must not take over a later request, and a
// definite refusal must not leave it stuck.
func TestNetworkConsentRetainedEntryDoesNotTakeOver(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now()), loseFirstAck: true}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	// the first submission is lost: an unacknowledged entry remains
	if err := runConsentSet(t, server, address, seedPath); err == nil {
		t.Fatal("a lost acknowledgement was reported as stored")
	}
	path, _ := snConsentJournalPath()
	if entry, _ := snConsentJournalLoad(path, server.URL+"/", address); entry == nil || entry.Acked {
		t.Fatal("the unacknowledged entry is missing, or the trailing slash split the journal key")
	}

	// a request for a different interval discards it and signs a new consent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	from, through := int64(52), int64(52+65535)
	proof := &snWalletProof{SeedFile: seedPath, fromEpoch: &from, throughEpoch: &through}
	fake.loseFirstAck = false
	if err := snSetNetworkWalletConsent(ctx, connect.NewClientStrategyWithDefaults(ctx), server.URL, address, proof); err != nil {
		t.Fatalf("a new interval was blocked by the stale entry: %v", err)
	}
	if fake.challenges != 2 {
		t.Fatalf("a new interval did not request a new challenge (%d)", fake.challenges)
	}
	last := fake.submissions[len(fake.submissions)-1]
	statement, err := protocol.DecodeNetworkWalletMappingStatement(last.Message)
	if err != nil || statement.FromEpoch != 52 {
		t.Fatalf("the stored consent is not the requested interval: %v %+v", err, statement)
	}
}

func TestNetworkConsentDefiniteRefusalDropsTheRetainedEntry(t *testing.T) {
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now()), loseFirstAck: true}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	if err := runConsentSet(t, server, address, seedPath); err == nil {
		t.Fatal("a lost acknowledgement was reported as stored")
	}
	// now the server answers every submission with a definite refusal
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sn/epoch":
			_ = json.NewEncoder(w).Encode(consentTestEpoch())
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"message": "original challenge is expired or not yet effective"}})
		}
	}))
	defer refusing.Close()
	path, _ := snConsentJournalPath()
	entry, _ := snConsentJournalLoad(path, server.URL, address)
	if entry == nil {
		t.Fatal("no retained entry")
	}
	moved := *entry
	moved.ApiUrl = refusing.URL
	if err := snConsentJournalStore(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := runConsentSet(t, refusing, address, seedPath); err == nil || !strings.Contains(err.Error(), "discarded") {
		t.Fatalf("a definite refusal did not discard the entry: %v", err)
	}
	if left, _ := snConsentJournalLoad(path, refusing.URL, address); left != nil {
		t.Fatal("the refused entry is still in the journal")
	}
}

func TestWalletStatusShowsTheConsentApartFromALoginProofWallet(t *testing.T) {
	consentTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sn/epoch":
			_ = json.NewEncoder(w).Encode(consentTestEpoch())
		case "/sn/wallet":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"wallets": []map[string]interface{}{
				{"coldkey_ss58": "5Network", "consent_scope": "network", "from_epoch": 51, "through_epoch": 65585, "mapping_hash": "abc\x1b[2J", "mapping_generation": 1},
				{"coldkey_ss58": "5Login", "set_at_millis": 1},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out strings.Builder
	if err := snWalletStatus(ctx, &out, connect.NewClientStrategyWithDefaults(ctx), server.URL); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"network consent for the network: coldkey 5Network, epochs 51 through 65585",
		"starts in 1 epoch(s)",
		"login-proof wallet 5Login",
		"release settlement does not pay it",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status lacks %q:\n%s", want, text)
		}
	}
	if strings.ContainsRune(text, 0x1b) {
		t.Fatal("a control character from the server reached the terminal")
	}
}

func walletTestKeypair(t testing.TB, seed []byte) subkey.KeyPair {
	t.Helper()
	keypair, err := (sr25519.Scheme{}).FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return keypair
}
