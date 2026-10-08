package main

// sn_fleet.go — the fleet-bind/revoke machinery behind `provider bind-head`
// and `provider unbind-head` (sn/miner/fleet.go is the reference flow).
//
// The upstream sn line replaced the per-head bind/unbind model with the
// release-1.0 many-to-one dual-signed fleet binding: a FleetManifest names
// the fleet (chain, netuid, coordinator, fleet id, hotkey, generation,
// members), a binding carries one member's client_id/client_key, and BOTH
// the client key (Ed25519) and the hotkey (sr25519) sign the same
// domain-separated digest. Revocation ends one binding generation at a
// future epoch, signed by the client. These commands keep their names and
// their offline-print/EVM-submit shape; the flags follow the new model
// (manifest + hotkey seed + epochs).

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/miner/onchain"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// stCoordinator holds the shared abigen packers/unpackers for STCoordinator
// (fleet bind/revoke), mirroring stSubnet for the subnet contract.
var stCoordinator = stabi.NewSTCoordinator()

// parseClientID16Arg parses a 16-byte client id (0x-optional hex, as the
// manifest members use).
func parseClientID16Arg(field string, value string) ([16]byte, error) {
	var out [16]byte
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "0x"), "0X"))
	if err != nil || len(b) != len(out) {
		return out, fmt.Errorf("%s must be a 16-byte hex value", field)
	}
	copy(out[:], b)
	return out, nil
}

// providerClientId16 reads the provider's own client_id from the client JWT
// store ("direct" = the native connection's identity).
func providerClientId16() ([16]byte, error) {
	var out [16]byte
	if globalClientJWTStore == nil {
		return out, fmt.Errorf("client JWT store uninitialized; pass --client_id=<hex16>")
	}
	entry, ok := globalClientJWTStore.Get("direct")
	if !ok || strings.TrimSpace(entry.ClientID) == "" {
		return out, fmt.Errorf("no provider client_id in the client JWT store; pass --client_id=<hex16>")
	}
	compact := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(entry.ClientID)), "-", "")
	b, err := hex.DecodeString(compact)
	if err != nil || len(b) != len(out) {
		return out, fmt.Errorf("provider client_id %q is not a 16-byte uuid", entry.ClientID)
	}
	copy(out[:], b)
	return out, nil
}

// snLoadFleetManifestOpt loads and validates the --manifest file.
// Allowlisted fleet chains, mirroring the sn reference's mainnet runtime gate
// (miner/fleet_mainnet_runtime.go): 964 is mainnet, 945 is the provisional
// testnet. The reference additionally requires a SHA-256-pinned reviewed
// authority document for 964 and explicit provisional flags for 945 — the
// port enforce the allowlist itself and prints the commitment hash so the
// operator can verify the manifest against the on-chain commitment.
const (
	fleetChainIDMainnet            = 964
	fleetChainIDProvisionalTestnet = 945
)

func snLoadFleetManifestOpt(opts docopt.Opts) (*protocol.FleetManifest, error) {
	path, _ := opts.String("--manifest")
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("--manifest: the fleet manifest is required (the fleet model is manifest-driven)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--manifest: %s", err)
	}
	manifest, err := protocol.ParseFleetManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("--manifest: %s", err)
	}
	switch manifest.ChainID {
	case fleetChainIDMainnet:
	case fleetChainIDProvisionalTestnet:
		fmt.Printf("warning: manifest chain id %d is the provisional testnet fleet; do not bind a mainnet hotkey to it\n", manifest.ChainID)
	default:
		return nil, fmt.Errorf("--manifest: fleet manifest chain id %d is not an allowlisted fleet chain (%d mainnet, %d provisional testnet); refusing to sign for a fleet no coordinator has attested", manifest.ChainID, fleetChainIDMainnet, fleetChainIDProvisionalTestnet)
	}
	return manifest, nil
}

// snClientIdOpt resolves the client id: --client_id, else the provider's own.
func snClientIdOpt(opts docopt.Opts) ([16]byte, error) {
	value, _ := opts.String("--client_id")
	if strings.TrimSpace(value) != "" {
		return parseClientID16Arg("--client_id", value)
	}
	return providerClientId16()
}

// snUint64Opt parses a required decimal uint64 option.
func snUint64Opt(opts docopt.Opts, name string) (uint64, error) {
	value, _ := opts.String(name)
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %s (required)", name, err)
	}
	return parsed, nil
}

// snClientKeyOpt resolves the client signing key: --client_seed_file, else
// the provider's own client identity key.
func snClientKeyOpt(opts docopt.Opts) (ed25519.PrivateKey, error) {
	seedFile, _ := opts.String("--client_seed_file")
	if strings.TrimSpace(seedFile) != "" {
		return snLoadClientSeedOverride(seedFile)
	}
	return snLoadClientKey()
}

// snFleetMember finds the manifest member for a client id.
func snFleetMember(manifest *protocol.FleetManifest, clientID [16]byte) (protocol.FleetMember, error) {
	for _, member := range manifest.Members {
		if member.ClientID == clientID {
			return member, nil
		}
	}
	return protocol.FleetMember{}, fmt.Errorf("client_id 0x%x is not in the manifest", clientID)
}

// snLoadHotkeySeed loads an sr25519 hotkey seed file (raw 32 bytes, or hex
// text, mirroring how sn/miner loads seeds). The file is read under the
// seed-custody policy in snReadSeedFile (regular file, 0600/0400, no
// hardlinks, owner-only, O_NOFOLLOW); the hotkey is the higher-value key.
func snLoadHotkeySeed(path string) ([]byte, error) {
	raw, err := snReadSeedFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		trimmed := strings.TrimSpace(string(raw))
		decoded, decodeErr := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "0x"), "0X"))
		if decodeErr != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("%s: expected raw or hex 32-byte sr25519 seed", path)
		}
		raw = decoded
	}
	return raw, nil
}

// snLoadClientSeedOverride loads an Ed25519 client seed file (raw 32 bytes,
// or hex text) under the same regular-file/permission policy.
func snLoadClientSeedOverride(path string) (ed25519.PrivateKey, error) {
	raw, err := snReadSeedFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.SeedSize {
		trimmed := strings.TrimSpace(string(raw))
		decoded, decodeErr := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "0x"), "0X"))
		if decodeErr != nil || len(decoded) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s: expected raw or hex 32-byte Ed25519 seed", path)
		}
		raw = decoded
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// snReadSeedFile reads a seed file only under the custody policy that
// matters for a key of this value: a regular file, mode exactly 0600 or
// 0400, not hardlinked elsewhere (nlink 1), owned by the current user, and
// opened with O_NOFOLLOW then re-stat'd against the Lstat so the path cannot
// be swapped for a symlink between check and open. The read is bounded at
// 4096 bytes. (crv4's loader adds an os.Root wrapper on top; the checks
// below are the parts this port enforces itself.)
func snReadSeedFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	switch info.Mode().Perm() {
	case 0o600, 0o400:
	default:
		return nil, fmt.Errorf("%s: seed file must be mode 0600 or 0400 (mode %o)", path, info.Mode().Perm())
	}
	if err := snSeedFileOwnership(info); err != nil {
		return nil, fmt.Errorf("%s: %s", path, err)
	}
	file, err := os.OpenFile(path, seedFileOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openStat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := snSeedFileSameInode(info, openStat); err != nil {
		return nil, fmt.Errorf("%s: %s", path, err)
	}
	return io.ReadAll(io.LimitReader(file, 4096))
}

// snFleetBindingAndSign mirrors sn/miner's fleetBindingAndSign: build the
// binding from the manifest + member + epochs, then dual-sign it — client
// Ed25519 over the digest, hotkey sr25519 over the same digest.
func snFleetBindingAndSign(manifest *protocol.FleetManifest, member protocol.FleetMember, from, to uint64, clientKey ed25519.PrivateKey, hotkeySeed []byte) (protocol.FleetBinding, []byte, []byte, error) {
	binding, err := manifest.Binding(member, from, to)
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	// Validate the hotkey BEFORE producing any signature: a mismatched seed
	// must not leave a client signature over an attacker-chosen digest in
	// memory (the reference validates the hotkey before signing, too).
	hotkey, err := (sr25519.Scheme{}).FromSeed(hotkeySeed)
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	if !bytesEqualConst(hotkey.Public(), manifest.Hotkey[:]) {
		return binding, nil, nil, errors.New("hotkey seed does not match the manifest hotkey")
	}
	clientSignature, err := binding.SignClient(clientKey)
	if err != nil {
		return binding, nil, nil, err
	}
	digest, err := binding.Digest()
	if err != nil {
		return binding, nil, nil, err
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		return binding, nil, nil, err
	}
	return binding, clientSignature, hotkeySignature, nil
}

func bytesEqualConst(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	diff := byte(0)
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// snReadFleetRevokeDigest reads the coordinator's canonical revoke digest
// via eth_call (finalized), trying each endpoint in order — the same
// read-side transport as the claim path (sn_rpc.go), against the
// coordinator contract. Each endpoint is chain-id-authenticated before the
// view call, and the return is ABI-decoded, so a wrong-chain or wrong-ABI
// node fails over instead of yielding a plausible digest.
func snReadFleetRevokeDigest(ctx context.Context, chainId uint64, rpcUrls []string, coordinatorHex string, calldata []byte) (digest [32]byte, rpcUrl string, err error) {
	for _, url := range rpcUrls {
		chainIdHex, rpcErr := ethRpcHexResult(ctx, url, "eth_chainId", []any{})
		if rpcErr != nil {
			continue
		}
		rpcChainId, rpcErr := parseEthHexQuantity(chainIdHex)
		if rpcErr != nil || rpcChainId != chainId {
			continue
		}
		callHex, rpcErr := ethRpcHexResult(ctx, url, "eth_call", []any{
			map[string]any{
				"to":   coordinatorHex,
				"data": fmt.Sprintf("0x%x", calldata),
			},
			"finalized",
		})
		if rpcErr != nil {
			continue
		}
		returnData, rpcErr := parseEthHexBytes(callHex)
		if rpcErr != nil {
			continue
		}
		unpacked, unpackErr := stCoordinator.UnpackFleetRevokeDigest(returnData)
		if unpackErr != nil {
			continue
		}
		return unpacked, url, nil
	}
	return digest, "", fmt.Errorf("no --rpc endpoint answered the fleet revoke digest")
}

// snFleetSubmit submits the calldata through sn/miner/onchain as the relayer
// (the same path the claim command uses). RuntimeAdmission is deliberately
// NOT used here: the provider line binds fleets directly (crv4/onchain own
// admission on the sn/miner side), so this path stays the plain EVM submit.
func snFleetSubmit(ctx context.Context, manifest *protocol.FleetManifest, rpcUrls []string, keyFile string, calldata []byte, dryRun bool) (*types.Receipt, error) {
	key, err := onchain.LoadKeyFile(keyFile)
	if err != nil {
		return nil, err
	}
	return onchain.Submit(ctx, onchain.SubmitParams{
		Contract: common.Address(manifest.Coordinator),
		Rpcs:     rpcUrls,
		Key:      key,
		Calldata: calldata,
		ChainID:  new(big.Int).SetUint64(manifest.ChainID),
		DryRun:   dryRun,
	})
}
