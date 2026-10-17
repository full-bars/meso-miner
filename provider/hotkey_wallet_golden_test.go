package main

// Byte-for-byte golden tests for every signed message the wallet hotkey
// family introduces: the global hotkey wallet consent (the "challenge" that
// the coldkey and the hotkey both sign) and the operator's hotkey network
// delegation (the payload the hotkey alone signs at an authenticating
// operator). The expected bytes below are literals produced by the upstream
// builders at 4ca8316e (github.com/urfoundation/sn, the module pin
// v0.0.0-20261016114744-4ca8316e4f3f) over the fixed synthetic inputs in
// goldenConsentStatement / goldenDelegationStatement, and cross-checked
// against the in-tree copies during the port. The test is offline: it never
// touches the network, and a protocol or module-pin drift that changes the
// wire bytes fails here first.
//
// The sr25519 signatures themselves are deliberately not pinned: schnorrkel
// signing is randomized, so only the signed payload bytes are stable. What
// the family signs is exactly these bytes, wrapped in <Bytes>...</Bytes> by
// hotkeywallet.Sign; the kept hotkeywallet package tests cover the wrapper.

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"

	"github.com/urnetwork/connect/hotkeywallet"
)

// The first line of every consent message.
const goldenConsentPrefix = "Approve URnetwork hotkey wallet mapping\n"

// The first line of every delegation message.
const goldenDelegationPrefix = "Delegate URnetwork network wallet to hotkey\n"

// The frozen consent statement JSON (generation 3, epochs 100 through 1100).
const goldenConsentJSON = `{"schema":"urnetwork-hotkey-wallet-mapping-consent-v1","scope":"hotkey","subnet":{"chain_id":7,"genesis_hash":[3,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"netuid":25},"hotkey":[26,79,238,72,193,186,26,72,232,205,67,120,42,132,133,214,53,170,145,207,184,44,187,71,127,12,28,87,107,196,3,28],"coldkey":[24,157,172,41,41,109,49,129,77,200,197,108,243,211,106,5,67,55,43,186,117,56,250,50,42,74,235,254,188,57,224,86],"generation":3,"previous_hash":[97,142,39,66,168,229,59,244,169,86,79,30,10,174,248,168,38,146,85,41,173,166,97,140,132,218,230,82,52,184,83,198],"nonce":[128,129,130,131,132,133,134,135,136,137,138,139,140,141,142,143,144,145,146,147,148,149,150,151,152,153,154,155,156,157,158,159],"issued_at":1791244800,"from_epoch":100,"through_epoch":1100}`

// The frozen delegation statement JSON (consent generation 3, delegation
// generation 2, prospective boundary epoch 0 block 500).
const goldenDelegationJSON = `{"schema":"urnetwork-hotkey-network-delegation-v1","scope":"hotkey-network","domain":{"chain_id":7,"genesis_hash":[3,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"netuid":25,"coordinator":"0x0400000000000000000000000000000000000000","settlement_vault":"0x0500000000000000000000000000000000000000","deployment_id_hash":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"policy_hash":[7,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"no_id":8},"user_id":[7,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"network_id":[9,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"hotkey":[26,79,238,72,193,186,26,72,232,205,67,120,42,132,133,214,53,170,145,207,184,44,187,71,127,12,28,87,107,196,3,28],"consent_head_hash":[97,142,39,66,168,229,59,244,169,86,79,30,10,174,248,168,38,146,85,41,173,166,97,140,132,218,230,82,52,184,83,198],"consent_generation":3,"generation":2,"previous_hash":[106,240,214,130,221,47,50,146,253,189,114,184,156,171,73,222,213,209,227,84,139,95,21,217,201,42,215,180,100,135,11,85],"nonce":[223,140,252,16,143,209,165,216,58,223,64,229,109,57,223,185,218,5,69,57,83,29,165,199,194,102,250,137,112,191,10,79],"issued_at":1791244800,"expires_at":1791245100,"from_epoch":100,"through_epoch":1100,"prospective":{"boundary":{"epoch":0,"block":500,"hash":[12,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]},"signer":"0x17c5185167401ed00cf5f5b2fc97d9bbfdb7d025","signature":[6,52,132,180,146,179,149,230,168,249,96,101,198,226,49,17,158,144,220,109,61,123,92,54,49,12,73,187,126,195,225,196,104,153,33,222,196,131,118,73,207,150,245,103,152,143,109,116,74,150,155,9,253,33,206,88,181,8,203,59,160,130,67,87,1]}}`

// The subnet of the synthetic operator: the same values the provider-side
// tests use.
func goldenDomain() protocol.ClientKeyHistoryDomain {
	return protocol.ClientKeyHistoryDomain{
		ChainID:          7,
		GenesisHash:      [32]byte{3},
		Netuid:           25,
		Coordinator:      common.Address{4},
		SettlementVault:  common.Address{5},
		DeploymentIDHash: [32]byte{6},
		PolicyHash:       [32]byte{7},
		NoID:             8,
	}
}

func goldenBoundary() protocol.ClientKeyEffectiveBoundary {
	return protocol.ClientKeyEffectiveBoundary{Epoch: 0, Block: 500, Hash: [32]byte{12}}
}

// goldenKey derives a fixed sr25519 keypair from a repeated seed byte.
func goldenKey(t testing.TB, fill byte) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = fill
	}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The fixed statement the coldkey and the hotkey sign.
func goldenConsentStatement(t testing.TB) protocol.HotkeyWalletMappingStatement {
	t.Helper()
	var nonce [32]byte
	for i := range nonce {
		nonce[i] = byte(0x80 + i)
	}
	return protocol.HotkeyWalletMappingStatement{
		Schema:       protocol.HotkeyWalletMappingConsentSchema,
		Scope:        protocol.HotkeyWalletMappingScope,
		Subnet:       goldenDomain().HotkeySubnet(),
		Hotkey:       goldenKey(t, 2).PublicKey(),
		Coldkey:      goldenKey(t, 1).PublicKey(),
		Generation:   3,
		PreviousHash: sha256.Sum256([]byte("d17 hotkey golden consent head")),
		Nonce:        nonce,
		IssuedAt:     1791244800,
		FromEpoch:    100,
		ThroughEpoch: 1100,
	}
}

// The fixed delegation challenge, prospective approval included: the
// operator's ECDSA signature over the boundary is deterministic.
func goldenDelegationStatement(t testing.TB) protocol.HotkeyNetworkDelegationStatement {
	t.Helper()
	operatorKey, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	statement := protocol.HotkeyNetworkDelegationStatement{
		Schema:            protocol.HotkeyNetworkDelegationSchema,
		Scope:             protocol.HotkeyNetworkDelegationScope,
		Domain:            goldenDomain(),
		UserId:            [16]byte{7},
		NetworkId:         [16]byte{9},
		Hotkey:            goldenKey(t, 2).PublicKey(),
		ConsentHeadHash:   sha256.Sum256([]byte("d17 hotkey golden consent head")),
		ConsentGeneration: 3,
		Generation:        2,
		PreviousHash:      sha256.Sum256([]byte("d17 hotkey golden delegation previous")),
		Nonce:             sha256.Sum256([]byte("d17 hotkey golden delegation nonce")),
		IssuedAt:          1791244800,
		ExpiresAt:         1791245100,
		FromEpoch:         100,
		ThroughEpoch:      1100,
	}
	if err := protocol.SignProspectiveHotkeyNetworkDelegation(&statement, goldenBoundary(), operatorKey); err != nil {
		t.Fatal(err)
	}
	return statement
}

// The first differing byte of got and want, for a readable failure.
func goldenFirstDifference(got, want string) int {
	index := 0
	for index < len(got) && index < len(want) && got[index] == want[index] {
		index++
	}
	return index
}

func TestGoldenHotkeyWalletConsentMessage(t *testing.T) {
	statement := goldenConsentStatement(t)
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	expected := goldenConsentPrefix + goldenConsentJSON
	if message != expected {
		index := goldenFirstDifference(message, expected)
		t.Fatalf("the consent message is not the frozen bytes: %d bytes, first difference at %d\ngot  %q\nwant %q",
			len(message), index,
			message[max(0, index-8):min(len(message), index+8)],
			expected[max(0, index-8):min(len(expected), index+8)])
	}
	if protocol.HotkeyWalletMappingConsentPrefix != goldenConsentPrefix {
		t.Fatalf("the consent prefix drifted from the frozen first line: %q", protocol.HotkeyWalletMappingConsentPrefix)
	}

	// our own decoder accepts the frozen bytes and re-encodes them unchanged
	decoded, err := protocol.DecodeHotkeyWalletMappingStatement(message)
	if err != nil {
		t.Fatalf("the frozen consent message does not decode: %v", err)
	}
	if decoded.Hotkey != statement.Hotkey || decoded.Coldkey != statement.Coldkey || decoded.Generation != statement.Generation || decoded.PreviousHash != statement.PreviousHash || decoded.Nonce != statement.Nonce || decoded.IssuedAt != statement.IssuedAt || decoded.FromEpoch != statement.FromEpoch || decoded.ThroughEpoch != statement.ThroughEpoch || decoded.Subnet != statement.Subnet {
		t.Fatalf("the decoded consent statement is not the fixed one: %+v", decoded)
	}
	if again, err := decoded.Message(); err != nil || again != message {
		t.Fatalf("the consent message does not re-encode unchanged: %v", err)
	}

	// and it is exactly what the family signs and the operator accepts: the
	// coldkey and the hotkey sign these bytes, verified offline
	coldkeySig, err := hotkeywallet.Sign(goldenKey(t, 1), message)
	if err != nil {
		t.Fatal(err)
	}
	hotkeySig, err := hotkeywallet.Sign(goldenKey(t, 2), message)
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.HotkeyWalletMappingConsent{Message: message, ColdkeySignature: coldkeySig, HotkeySignature: hotkeySig}
	if _, _, err := protocol.VerifyHotkeyWalletMappingConsent(context.Background(), original); err != nil {
		t.Fatalf("the signed frozen consent does not verify: %v", err)
	}
}

func TestGoldenHotkeyWalletDelegationMessage(t *testing.T) {
	statement := goldenDelegationStatement(t)
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	expected := goldenDelegationPrefix + goldenDelegationJSON
	if message != expected {
		index := goldenFirstDifference(message, expected)
		t.Fatalf("the delegation message is not the frozen bytes: %d bytes, first difference at %d\ngot  %q\nwant %q",
			len(message), index,
			message[max(0, index-8):min(len(message), index+8)],
			expected[max(0, index-8):min(len(expected), index+8)])
	}
	if protocol.HotkeyNetworkDelegationPrefix != goldenDelegationPrefix {
		t.Fatalf("the delegation prefix drifted from the frozen first line: %q", protocol.HotkeyNetworkDelegationPrefix)
	}

	// our own decoder accepts the frozen bytes and re-encodes them unchanged
	decoded, err := protocol.DecodeHotkeyNetworkDelegationStatement(message)
	if err != nil {
		t.Fatalf("the frozen delegation message does not decode: %v", err)
	}
	if decoded.Hotkey != statement.Hotkey || decoded.ConsentHeadHash != statement.ConsentHeadHash || decoded.ConsentGeneration != statement.ConsentGeneration || decoded.Generation != statement.Generation || decoded.PreviousHash != statement.PreviousHash || decoded.Nonce != statement.Nonce || decoded.IssuedAt != statement.IssuedAt || decoded.ExpiresAt != statement.ExpiresAt || decoded.FromEpoch != statement.FromEpoch || decoded.ThroughEpoch != statement.ThroughEpoch || decoded.Domain != statement.Domain || decoded.UserId != statement.UserId || decoded.NetworkId != statement.NetworkId {
		t.Fatalf("the decoded delegation statement is not the fixed one: %+v", decoded)
	}
	if again, err := decoded.Message(); err != nil || again != message {
		t.Fatalf("the delegation message does not re-encode unchanged: %v", err)
	}

	// and it is exactly what the family signs and the operator accepts: the
	// hotkey signs these bytes and the delegation verifies offline, with the
	// prospective approval over the fixed boundary
	hotkeySig, err := hotkeywallet.Sign(goldenKey(t, 2), message)
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: hotkeySig}
	verified, _, err := protocol.VerifyHotkeyNetworkDelegation(context.Background(), original)
	if err != nil {
		t.Fatalf("the signed frozen delegation does not verify: %v", err)
	}
	if verified.Hotkey != statement.Hotkey || verified.ConsentHeadHash != statement.ConsentHeadHash || verified.ConsentGeneration != statement.ConsentGeneration {
		t.Fatalf("the verified delegation does not name the fixed consent: %+v", verified)
	}
}
