package main

// Fleet signed-message golden vectors. Every deterministic byte that the
// fleet commands sign or submit is pinned here against the builders of the sn
// module at the pinned upstream commit (protocol.FleetBinding/FleetRevoke,
// crv4 commitment encoding, snchain/onchain calldata encoders) with fixed
// inputs, entirely offline. The sr25519 hotkey signatures are randomized by
// the schnorrkel implementation in this stack (upstream tests verify them, they
// are never byte-pinned), so the bind calldata is pinned through the stabi ABI
// encoder with a fixed stub signature and the real path is additionally decoded
// back to its exact inputs.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"

	snchain "github.com/urfoundation/sn/chain"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/miner/onchain"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// The fixed inputs: seed(0x01..), seed(0x41..), client id 0x11..0x20, chain
// 964, netuid 25, generation 7, epochs [1000,2000], revoke at 1500.
const (
	fleetGoldenCanonicalHex    = "7b22736368656d61223a2275726e6574776f726b2d666c6565742d6d616e69666573742d7631222c22636861696e5f6964223a3936342c226e6574756964223a32352c22636f6f7264696e61746f72223a22307833303331333233333334333533363337333833393361336233633364336533663430343134323433222c22666c6565745f6964223a22307861306131613261336134613561366137613861396161616261636164616561666230623162326233623462356236623762386239626162626263626462656266222c22686f746b6579223a22307830613163373765646234383531393036396262623135346266303838633766313736373838333964653435386530363739333730616539383961663366353563222c2267656e65726174696f6e223a372c226d656d62657273223a5b7b22636c69656e745f6964223a2230783131313231333134313531363137313831393161316231633164316531663230222c22636c69656e745f6b6579223a22307837396235353632653866653635346639343037386231313265386139386261373930316638353361653639356265643765306533393130626164303439363634227d5d7d"
	fleetGoldenCommitmentHex   = "95451092d4bfb5633b49c5fdde86b325c7bc32d64d61e1bcfb7be240181420ee"
	fleetGoldenDigestHex       = "4dc87dc9f46431bb92ba346d3fa62ca6e3e9a0198217f046349bffda2b3f8115"
	fleetGoldenClientSigHex    = "fb88649ac269fb150c642f36648b87c06df7c939cbe34f16e24372212c5ac55da6eb42f80df29c923fa616f8a959e95f6eff975fbaf1bddf89e9a562f7a79401"
	fleetGoldenBindPackHex     = "d10781fa00000000000000000000000000000000000000000000000000000000000003c40000000000000000000000000000000000000000000000000000000000000019000000000000000000000000303132333435363738393a3b3c3d3e3f40414243a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf0a1c77edb48519069bbb154bf088c7f17678839de458e0679370ae989af3f55c1112131415161718191a1b1c1d1e1f200000000000000000000000000000000079b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664000000000000000000000000000000000000000000000000000000000000000700000000000000000000000000000000000000000000000000000000000003e800000000000000000000000000000000000000000000000000000000000007d095451092d4bfb5633b49c5fdde86b325c7bc32d64d61e1bcfb7be240181420ee00000000000000000000000000000000000000000000000000000000000001a000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000040fb88649ac269fb150c642f36648b87c06df7c939cbe34f16e24372212c5ac55da6eb42f80df29c923fa616f8a959e95f6eff975fbaf1bddf89e9a562f7a79401000000000000000000000000000000000000000000000000000000000000004042424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242424242"
	fleetGoldenBindSelectorHex = "d10781fa"
	fleetGoldenRevokeDigestHex = "9aa9fee9e190b0c0fd8423f92b74c05a245cc7081d64b2d4504968a3295dea35"
	fleetGoldenRevokeDataHex   = "7df528561112131415161718191a1b1c1d1e1f2000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000700000000000000000000000000000000000000000000000000000000000005dc000000000000000000000000000000000000000000000000000000000000008000000000000000000000000000000000000000000000000000000000000000405395c384662446cf4e0f2d360c9f1f1917d0dfc955789624f18ec5e930ebb10e8e10aaa57c58459d6dc0212ebc77e83a533ef506f4a6f2a6f2f22fdb30141c08"
	fleetGoldenCommitmentInfo  = "04297572666c6565743195451092d4bfb5633b49c5fdde86b325c7bc32d64d61e1bcfb7be240181420ee"
	fleetGoldenSetCommitment   = "1200190004297572666c6565743195451092d4bfb5633b49c5fdde86b325c7bc32d64d61e1bcfb7be240181420ee"
	fleetGoldenRegisterCall    = "078619000a1c77edb48519069bbb154bf088c7f17678839de458e0679370ae989af3f55c6400000000000000"
)

func fleetGoldenSeed(start byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = start + byte(i)
	}
	return seed
}

// fleetGoldenFixture rebuilds the fixed inputs and runs the ported signing
// path (fleetBindingAndSign) so the golden comparison covers this port, not
// only the module libraries.
func fleetGoldenFixture(t *testing.T) (*protocol.FleetManifest, protocol.FleetBinding, []byte, []byte, [16]byte, ed25519.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	clientSeed := fleetGoldenSeed(0x01)
	hotkeySeed := fleetGoldenSeed(0x41)
	clientPrivate := ed25519.NewKeyFromSeed(clientSeed)
	hotkey, err := crv4.KeypairFromSeed([32]byte(hotkeySeed))
	if err != nil {
		t.Fatal(err)
	}
	var clientID [16]byte
	for i := range clientID {
		clientID[i] = 0x11 + byte(i)
	}
	manifest := &protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: 964, Netuid: 25, Hotkey: hotkey.PublicKey(), Generation: 7}
	for i := range manifest.Coordinator {
		manifest.Coordinator[i] = 0x30 + byte(i)
	}
	for i := range manifest.FleetID {
		manifest.FleetID[i] = 0xa0 ^ byte(i)
	}
	var clientKey [32]byte
	copy(clientKey[:], clientPrivate.Public().(ed25519.PublicKey))
	manifest.Members = []protocol.FleetMember{{ClientID: clientID, ClientKey: clientKey}}
	clientSeedPath := filepath.Join(dir, "client.seed")
	hotkeySeedPath := filepath.Join(dir, "hotkey.seed")
	if err := os.WriteFile(clientSeedPath, clientSeed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hotkeySeedPath, hotkeySeed, 0600); err != nil {
		t.Fatal(err)
	}
	opts := docopt.Opts{
		"--client_id":        hex.EncodeToString(clientID[:]),
		"--client_seed_file": clientSeedPath,
		"--hotkey_seed_file": hotkeySeedPath,
		"--valid_from_epoch": "1000",
		"--valid_to_epoch":   "2000",
		"--effective_epoch":  "1500",
	}
	binding, clientSignature, hotkeySignature, err := fleetBindingAndSign(opts, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, binding, clientSignature, hotkeySignature, clientID, clientPrivate
}

// TestFleetBindingGoldenVectors pins the manifest, the binding digest and both
// signatures, the bind calldata wire encoding, and the publish/register
// artifacts (the tagged Raw40 commitment info that lands on Subtensor and the
// exact commitments.set_commitment / SubtensorModule.register_limit calls).
func TestFleetBindingGoldenVectors(t *testing.T) {
	requireSeedFileCustody(t)
	manifest, binding, clientSignature, hotkeySignature, clientID, clientPrivate := fleetGoldenFixture(t)

	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(canonical) != fleetGoldenCanonicalHex {
		t.Fatalf("manifest canonical bytes drifted:\n got %x\nwant %s", canonical, fleetGoldenCanonicalHex)
	}
	if _, err := protocol.ParseFleetManifest(canonical); err != nil {
		t.Fatalf("golden manifest no longer parses: %v", err)
	}
	hash, err := manifest.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(hash[:]) != fleetGoldenCommitmentHex {
		t.Fatalf("commitment hash drifted: got %x want %s", hash, fleetGoldenCommitmentHex)
	}

	// The ported path must equal the module's own builder over fixed inputs.
	member, err := fleetMember(manifest, clientID)
	if err != nil {
		t.Fatal(err)
	}
	upstreamBinding, err := manifest.Binding(member, 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if binding != upstreamBinding {
		t.Fatalf("ported binding differs from the module builder:\n got %+v\nwant %+v", binding, upstreamBinding)
	}
	digest, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(digest[:]) != fleetGoldenDigestHex {
		t.Fatalf("binding digest drifted: got %x want %s", digest, fleetGoldenDigestHex)
	}
	if hex.EncodeToString(clientSignature) != fleetGoldenClientSigHex {
		t.Fatalf("client signature drifted: got %x want %s", clientSignature, fleetGoldenClientSigHex)
	}
	if !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
		t.Fatal("binding signatures do not verify")
	}
	if !ed25519.Verify(clientPrivate.Public().(ed25519.PublicKey), digest[:], clientSignature) {
		t.Fatal("client signature is not a raw Ed25519 signature over the digest")
	}

	// Bind calldata: the exact wire encoding, pinned through the stabi ABI
	// encoder with a fixed stub hotkey signature, and the real path decoded
	// back to the same inputs (the sr25519 signature is randomized).
	stubHotkeySignature := make([]byte, 64)
	for i := range stubHotkeySignature {
		stubHotkeySignature[i] = 0x42
	}
	bindingPack, err := stCoordinator.TryPackBindFleetMember(stabi.STCoordinatorFleetBinding{
		ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.Address(binding.Coordinator),
		FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID,
		ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch,
		ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash,
	}, clientSignature, stubHotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(bindingPack) != fleetGoldenBindPackHex {
		t.Fatalf("bind calldata encoding drifted:\n got %x\nwant %s", bindingPack, fleetGoldenBindPackHex)
	}
	realCalldata, err := onchain.BuildFleetBindingCalldata(binding, clientSignature, hotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(realCalldata[:4]) != fleetGoldenBindSelectorHex {
		t.Fatalf("bind selector drifted: got %x want %s", realCalldata[:4], fleetGoldenBindSelectorHex)
	}
	if len(realCalldata) != len(bindingPack) {
		t.Fatalf("bind calldata length drifted: got %d want %d", len(realCalldata), len(bindingPack))
	}
	abi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := abi.Methods["bindFleetMember"].Inputs.Unpack(realCalldata[4:])
	if err != nil {
		t.Fatal(err)
	}
	if len(unpacked) != 3 {
		t.Fatalf("bind calldata unpacked %d args, want 3", len(unpacked))
	}
	// The ABI unpacker models the tuple as an anonymous struct; round-trip it
	// through its JSON tags into the binding type to compare field for field.
	rawPacked, err := json.Marshal(unpacked[0])
	if err != nil {
		t.Fatal(err)
	}
	var packed stabi.STCoordinatorFleetBinding
	if err := json.Unmarshal(rawPacked, &packed); err != nil {
		t.Fatal(err)
	}
	if packed.ChainId != binding.ChainID || packed.Netuid != binding.Netuid || common.Address(binding.Coordinator) != packed.Coordinator ||
		packed.FleetId != binding.FleetID || packed.Hotkey != binding.Hotkey || packed.ClientId != binding.ClientID ||
		packed.ClientKey != binding.ClientKey || packed.Generation != binding.Generation ||
		packed.ValidFromEpoch != binding.ValidFromEpoch || packed.ValidToEpoch != binding.ValidToEpoch || packed.CommitmentHash != binding.CommitmentHash {
		t.Fatalf("bind calldata carries a different binding: %+v", packed)
	}
	if string(unpacked[1].([]byte)) != string(clientSignature) || string(unpacked[2].([]byte)) != string(hotkeySignature) {
		t.Fatal("bind calldata carries different signatures")
	}

	// Publish artifact: the commitment info stored on Subtensor and the exact
	// commitments.set_commitment call, both from the module builders.
	commitmentInfo, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(commitmentInfo) != fleetGoldenCommitmentInfo {
		t.Fatalf("commitment info drifted: got %x want %s", commitmentInfo, fleetGoldenCommitmentInfo)
	}
	_, _, _, raw := fleetRuntimeCurrentTestArtifactInputs(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(codec.HexEncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	setCommitmentCall, err := (&crv4.Chain{Meta: metadata, Runtime: &types.RuntimeVersion{SpecVersion: 8001, TransactionVersion: 1}}).NewSetFleetCommitmentCall(manifest.Netuid, hash)
	if err != nil {
		t.Fatal(err)
	}
	setCommitmentBytes, err := codec.Encode(setCommitmentCall)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(setCommitmentBytes) != fleetGoldenSetCommitment {
		t.Fatalf("set_commitment call drifted: got %x want %s", setCommitmentBytes, fleetGoldenSetCommitment)
	}

	// Register artifact: the exact SubtensorModule.register_limit call.
	registerCall, err := snchain.BurnRegisterLimitCall(metadata, manifest.Netuid, manifest.Hotkey, 100)
	if err != nil {
		t.Fatal(err)
	}
	registerBytes, err := codec.Encode(registerCall)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(registerBytes) != fleetGoldenRegisterCall {
		t.Fatalf("register_limit call drifted: got %x want %s", registerBytes, fleetGoldenRegisterCall)
	}
}

// TestFleetRevokeGoldenVector pins the revoke digest and the complete revoke
// calldata (the Ed25519 client signature over the digest is deterministic).
func TestFleetRevokeGoldenVector(t *testing.T) {
	requireSeedFileCustody(t)
	manifest, _, _, _, clientID, clientPrivate := fleetGoldenFixture(t)
	revoke := protocol.FleetRevoke{ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator, ClientID: clientID, Generation: manifest.Generation, EffectiveEpoch: 1500}
	digest, err := revoke.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(digest[:]) != fleetGoldenRevokeDigestHex {
		t.Fatalf("revoke digest drifted: got %x want %s", digest, fleetGoldenRevokeDigestHex)
	}
	signature := ed25519.Sign(clientPrivate, digest[:])
	if !revoke.VerifyClient(clientPrivate.Public().(ed25519.PublicKey), signature) {
		t.Fatal("revoke signature does not verify")
	}
	calldata, err := stCoordinator.TryPackRevokeFleetBinding(clientID, manifest.Generation, 1500, signature)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(calldata) != fleetGoldenRevokeDataHex {
		t.Fatalf("revoke calldata drifted:\n got %x\nwant %s", calldata, fleetGoldenRevokeDataHex)
	}
	// The revoke calldata must carry exactly the digest the coordinator
	// returns from fleetRevokeDigest; the port checks that equality before it
	// signs, so the packed selector and args are pinned above.
	if got := fmt.Sprintf("0x%x", calldata[:4]); got != "0x7df52856" {
		t.Fatalf("revoke selector drifted: got %s", got)
	}
}
