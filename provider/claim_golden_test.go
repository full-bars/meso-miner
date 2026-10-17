// Offline golden bytes for everything the claim daemon computes from signed or
// digest input. Every expected literal below was produced with the upstream
// builders at sn 4ca8316e (the module this repo pins: onchain.BuildClaimCalldata,
// merkle.PayoutLeaf/NewTree sorted-pair keccak, stabi event packs and geth
// types.SignTx) on fixed synthetic inputs, then frozen here. A drift in the
// daemon's own digest, calldata or signed-bytes handling fails against these
// bytes, not against the daemon itself.
package main

import (
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/merkle"
	"github.com/urnetwork/connect"
)

// Fixed synthetic identity for the golden vectors; never a production key.
const goldenColdkeyBBase = 0x40
const goldenRelayerKeyHex = "4646464646464646464646464646464646464646464646464646464646464646"
const goldenRelayerAddress = "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F"

func goldenColdkey() [32]byte {
	var coldkey [32]byte
	for i := range coldkey {
		coldkey[i] = byte(0x10 + i)
	}
	return coldkey
}

func goldenColdkeyB() [32]byte {
	var coldkey [32]byte
	for i := range coldkey {
		coldkey[i] = byte(goldenColdkeyBBase + i)
	}
	return coldkey
}

func goldenClaim(t *testing.T) (*connect.SnPoolClaimResult, [32]byte) {
	t.Helper()
	coldkey := goldenColdkey()
	share := big.NewInt(10_000)
	tree, err := merkle.NewTree([]merkle.Leaf{merkle.PayoutLeaf(coldkey, share), merkle.PayoutLeaf(goldenColdkeyB(), share)})
	if err != nil {
		t.Fatal(err)
	}
	root := tree.Root()
	if got := hex.EncodeToString(root[:]); got != "eb3d83126d345ec6866bacdf5681a00744a6a8faf7016dbf608d4f13bbc70cb3" {
		t.Fatalf("upstream merkle root drifted: %s", got)
	}
	proof, err := tree.Proof(merkle.PayoutLeaf(coldkey, share))
	if err != nil {
		t.Fatal(err)
	}
	proofBytes := make([][]byte, len(proof))
	for i := range proof {
		proofBytes[i] = proof[i][:]
	}
	// epoch 7, no_id 0x01, coldkey 0x1011..2f, share 10_000, vault 0x..1234.
	return &connect.SnPoolClaimResult{Epoch: 7, NoId: []byte{0x01}, Coldkey: coldkey[:], ShareBps: 10_000, PayoutRoot: root[:], Proof: proofBytes, ContractAddress: "0x0000000000000000000000000000000000001234", ChainId: 945}, root
}

// The daemon's own key digest (keccak256 of the 32-byte padded no_id followed
// by the coldkey) must equal the upstream claim-key construction.
func TestClaimDaemonGoldenKeyDigest(t *testing.T) {
	coldkey := goldenColdkey()
	digest, err := claimKey([]byte{0x01}, coldkey[:])
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(digest[:]); got != "5f07bf4534e934ba9f2a6f4e6003a242942c18afdcfbe8225c625644534e17a9" {
		t.Fatalf("claim key digest drifted from the upstream construction: %s", got)
	}
}

// The vault and calldata the daemon signs implicitly (through its own
// claimCalldata) must be byte-identical to the upstream
// onchain.BuildClaimCalldata output for the same intent.
func TestClaimDaemonGoldenCalldata(t *testing.T) {
	claim, _ := goldenClaim(t)
	vault, calldata, err := claimCalldata(claim)
	if err != nil {
		t.Fatal(err)
	}
	if vault.Hex() != "0x0000000000000000000000000000000000001234" {
		t.Fatalf("settlement vault drifted: %s", vault.Hex())
	}
	wantCalldata := "0xce479a1b00000000000000000000000000000000000000000000000000000000000000070000000000000000000000000000000000000000000000000000000000000001101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f000000000000000000000000000000000000000000000000000000000000271000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000001c872262085aa9ea7339b63a7b8d9887df37c640bcf2f497f05bdd009391f056a"
	if got := "0x" + hex.EncodeToString(calldata); got != wantCalldata {
		t.Fatalf("claimMiner calldata drifted from the upstream builder:\n got %s\nwant %s", got, wantCalldata)
	}
}

// The receipt digest the daemon records in the durable queue must equal the
// digest of the exact canonical receipt bytes (sha256 over the JSON log list).
func TestClaimDaemonGoldenReceiptDigest(t *testing.T) {
	vault := common.HexToAddress("0x0000000000000000000000000000000000001234")
	blockHash := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000aa")
	txHash := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000bb")
	receipt := &types.Receipt{
		Status:           types.ReceiptStatusSuccessful,
		BlockNumber:      big.NewInt(90),
		BlockHash:        blockHash,
		TxHash:           txHash,
		TransactionIndex: 1,
		Logs: []*types.Log{{
			Address:     vault,
			Topics:      []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02")},
			Data:        []byte{0x03, 0x04, 0x05},
			BlockNumber: 90,
			BlockHash:   blockHash,
			TxHash:      txHash,
			TxIndex:     1,
			Index:       0,
		}},
	}
	entry := &ClaimQueueEntry{Epoch: 7, Status: "submitting", TxHash: txHash.Hex()}
	if err := recordFinalizedClaimReceipt(entry, receipt); err != nil {
		t.Fatal(err)
	}
	if entry.FinalizedBlock != 90 || entry.FinalizedBlockHash != strings.ToLower(blockHash.Hex()) {
		t.Fatalf("finalized identity drifted: %+v", entry)
	}
	if entry.ReceiptLogsHash != "sha256:01769fbf26e1ca23ba1cf9376c4c0891112c6c9f5b646798a3d62a1684483ae9" {
		t.Fatalf("receipt log digest drifted: %s", entry.ReceiptLogsHash)
	}
}

// The signed transaction the daemon retains and rebroadcasts is exact bytes:
// the golden RLP and hash come from the upstream geth signer, and the daemon's
// own child-output parser and intent authenticator must accept exactly those
// bytes and no rewrites.
func TestClaimDaemonGoldenSignedTransaction(t *testing.T) {
	claim, _ := goldenClaim(t)
	vault, calldata, err := claimCalldata(claim)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(goldenRelayerKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	if got := crypto.PubkeyToAddress(key.PublicKey).Hex(); got != goldenRelayerAddress {
		t.Fatalf("relayer address drifted: %s", got)
	}
	signer := types.LatestSignerForChainID(big.NewInt(945))
	signed, err := types.SignTx(types.NewTransaction(23, vault, big.NewInt(0), 120_000, big.NewInt(1), calldata), signer, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wantRaw := "0xf9014717018301d4c094000000000000000000000000000000000000123480b8e4ce479a1b00000000000000000000000000000000000000000000000000000000000000070000000000000000000000000000000000000000000000000000000000000001101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f000000000000000000000000000000000000000000000000000000000000271000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000001c872262085aa9ea7339b63a7b8d9887df37c640bcf2f497f05bdd009391f056a820785a053919813bb4713fe58437e90ebebc2685b59efe55c40e92181c01a7da1fcc352a04dc167e0b37f28331b4a0f5924a250878f91865b38180c8d0179e76f70fad90e"
	wantHash := "0x902ca5abb28876a1c7174fe9dfda1af3e575d2e3ce237f5ca654be4ac858e798"
	if got := "0x" + hex.EncodeToString(raw); got != wantRaw || signed.Hash().Hex() != wantHash {
		t.Fatalf("signed claim transaction drifted from the upstream signer:\n got raw %s\nwant raw %s\n got hash %s\nwant hash %s", got, wantRaw, signed.Hash().Hex(), wantHash)
	}

	// The daemon parses the child `claim` output: "prepared: tx <hash> raw <hex>".
	if hash, prepared := claimPreparedTx("prepared: tx " + wantHash + " raw " + wantRaw); hash != wantHash || prepared != wantRaw {
		t.Fatalf("prepared tx parser drifted: %q %q", hash, prepared)
	}
	if got := claimTxHash("sent: tx " + wantHash); got != wantHash {
		t.Fatalf("tx hash parser drifted: %s", got)
	}

	// The exact retained bytes authenticate against the configured relayer.
	keyPath := filepath.Join(t.TempDir(), "relayer.key")
	if err := os.WriteFile(keyPath, []byte(goldenRelayerKeyHex), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := &ClaimQueueEntry{Epoch: 7, Status: "submitting", TxHash: wantHash, RawTxHex: wantRaw}
	tx, intent, from, err := authenticateSignedClaim(&ClaimDaemonConfig{KeyFile: keyPath}, entry)
	if err != nil {
		t.Fatal(err)
	}
	coldkey := goldenColdkey()
	if tx.Hash().Hex() != wantHash || from.Hex() != goldenRelayerAddress || !intent.E.IsInt64() || intent.E.Int64() != 7 || intent.NoID.Cmp(big.NewInt(1)) != 0 || intent.Coldkey != coldkey || !intent.ShareBps.IsUint64() || intent.ShareBps.Uint64() != 10_000 {
		t.Fatalf("retained signed bytes did not authenticate to the golden intent: %v %v %v", tx.Hash().Hex(), from.Hex(), intent)
	}
}

// The settlement vault resolution: the explicit field wins, contract_address
// is the fallback, and a present-but-different pair is refused before any
// transaction is built (the review-required restore of upstream's check).
func TestClaimCalldataSettlementVaultResolution(t *testing.T) {
	const vaultA = "0x0000000000000000000000000000000000001234"
	const vaultB = "0x0000000000000000000000000000000000005678"

	validClaim := func() *connect.SnPoolClaimResult {
		var coldkey [32]byte
		coldkey[31] = 7
		share := new(big.Int).SetUint64(100)
		root := merkle.PayoutLeaf(coldkey, share)
		return &connect.SnPoolClaimResult{
			Epoch:      1,
			NoId:       []byte{1, 2, 3},
			Coldkey:    coldkey[:],
			PayoutRoot: root[:],
			ShareBps:   100,
		}
	}

	t.Run("only contract set", func(t *testing.T) {
		claim := validClaim()
		claim.ContractAddress = vaultA
		vault, _, err := claimCalldata(claim)
		if err != nil {
			t.Fatal(err)
		}
		if vault != common.HexToAddress(vaultA) {
			t.Fatalf("vault = %s, want %s", vault, vaultA)
		}
	})
	t.Run("both equal", func(t *testing.T) {
		claim := validClaim()
		claim.ContractAddress = vaultA
		claim.SettlementVaultAddress = vaultA
		vault, _, err := claimCalldata(claim)
		if err != nil {
			t.Fatal(err)
		}
		if vault != common.HexToAddress(vaultA) {
			t.Fatalf("vault = %s, want %s", vault, vaultA)
		}
	})
	t.Run("both differ refused", func(t *testing.T) {
		claim := validClaim()
		claim.ContractAddress = vaultA
		claim.SettlementVaultAddress = vaultB
		if _, _, err := claimCalldata(claim); err == nil || !strings.Contains(err.Error(), "inconsistent settlement vault") {
			t.Fatalf("err = %v, want the inconsistent settlement vault refusal", err)
		}
	})
	t.Run("vault only", func(t *testing.T) {
		claim := validClaim()
		claim.SettlementVaultAddress = vaultB
		vault, _, err := claimCalldata(claim)
		if err != nil {
			t.Fatal(err)
		}
		if vault != common.HexToAddress(vaultB) {
			t.Fatalf("vault = %s, want %s", vault, vaultB)
		}
	})
}
