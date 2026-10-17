package main

// wallet_network_consent.go -- the canonical prospective NETWORK wallet
// consent behind `provider wallet challenge|set`.
//
// Release settlement pays only a wallet that a coldkey consented to with a
// purpose-bound statement: a displayed schema, the network, the coldkey, an
// earning-epoch interval and the operator's prospective approval. The generic
// sign-in challenge the earlier path signed is a login, which settlement never
// pays, and which the same server also accepts as a wallet login.
//
// One network consent covers every provider client of the network, current and
// future, so a fleet of many proxies needs one coldkey signature, made on the
// operator's own machine. Nothing here runs on, or is needed by, a fleet node.
//
// Every field of the statement the server returns is checked against what this
// side knows BEFORE the coldkey is loaded, so a hostile or mistaken endpoint
// cannot get a signature over anything else.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"

	"github.com/urnetwork/connect"
)

// snKnownV1SettlementVault is the settlement vault GET /sn/epoch returned on
// 2026-10-15, before the v2 migration. It is a convenience for the v1 domain
// vault, not an independent trust anchor.
const snKnownV1SettlementVault = "0x98BF47ba01828676855B5ED10F2f22a867F0cB59"

// the consent text is valid for 5 minutes; allow clock skew on top
const snConsentClockSkew = 2 * time.Minute

// the protocol's finite consent interval is 65,536 epochs
const snConsentSpan = 65535

// snNetworkIdentity is what the network JWT says about its owner, read without
// verifying the signature: the server checks it, this side only compares.
type snNetworkIdentity struct {
	UserId    [16]byte
	NetworkId [16]byte
}

// snNetworkJwtIdentity reads user_id and network_id from a network JWT and
// refuses a client JWT: a network consent is made by the network owner, not by
// one provider client.
func snNetworkJwtIdentity(byJwt string) (snNetworkIdentity, error) {
	var identity snNetworkIdentity
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return identity, fmt.Errorf("the network jwt does not parse: %w", err)
	}
	if _, hasClient := claims["client_id"]; hasClient {
		return identity, errors.New("the saved jwt belongs to a client; run `provider auth` on this machine to save a network jwt")
	}
	for claim, into := range map[string]*[16]byte{"user_id": &identity.UserId, "network_id": &identity.NetworkId} {
		raw, ok := claims[claim].(string)
		id, err := connect.ParseId(raw)
		if !ok || err != nil || id == (connect.Id{}) {
			return identity, fmt.Errorf("the network jwt has no usable %s claim", claim)
		}
		*into = [16]byte(id)
	}
	return identity, nil
}

// snNetworkConsentExpect is everything the signer knows independently of the
// message it is asked to sign.
type snNetworkConsentExpect struct {
	Coldkey      [32]byte
	Identity     snNetworkIdentity
	FromEpoch    uint64
	ThroughEpoch uint64
	Epoch        *connect.SnEpochResult
	Now          time.Time
	// Previous is the consent kept in the local journal, if any. It only drives
	// the replay of an unacknowledged consent: the server owns the chain.
	Previous *snConsentJournalEntry
}

// snVerifyNetworkConsentMessage decodes the statement canonically (which also
// verifies the operator's prospective signature and every protocol bound) and
// compares each field with the independent expectation. It returns an error for
// anything that is not exactly the consent asked for.
func snVerifyNetworkConsentMessage(message string, want snNetworkConsentExpect) (*protocol.NetworkWalletMappingStatement, error) {
	if want.Epoch == nil {
		return nil, errors.New("wallet consent: the epoch state is unavailable; refusing to sign")
	}
	statement, err := protocol.DecodeNetworkWalletMappingStatement(message)
	if err != nil {
		return nil, fmt.Errorf("wallet consent: the message to sign is not a network wallet consent: %w; refusing to sign it", err)
	}
	refuse := func(what string) (*protocol.NetworkWalletMappingStatement, error) {
		return nil, fmt.Errorf("wallet consent: %s; refusing to sign it", what)
	}
	if statement.Schema != protocol.NetworkWalletMappingConsentSchema || statement.Scope != protocol.NetworkWalletMappingScope {
		return refuse("the statement is not a network consent")
	}
	if statement.Coldkey != want.Coldkey {
		return refuse("the statement names a different coldkey")
	}
	if statement.UserId != want.Identity.UserId || statement.NetworkId != want.Identity.NetworkId {
		return refuse("the statement names a different network or owner than the saved login")
	}
	if statement.FromEpoch != want.FromEpoch || statement.ThroughEpoch != want.ThroughEpoch {
		return refuse(fmt.Sprintf("the statement covers epochs %d to %d, not the %d to %d asked for",
			statement.FromEpoch, statement.ThroughEpoch, want.FromEpoch, want.ThroughEpoch))
	}
	if statement.FromEpoch <= want.Epoch.Epoch {
		return refuse("the earning interval starts at or before the current epoch; a consent is prospective")
	}
	if statement.Prospective.Boundary.Epoch > want.Epoch.Epoch {
		return refuse("the operator's approval is from a future epoch")
	}
	if statement.Prospective.Boundary.Epoch >= statement.FromEpoch {
		return refuse("the operator's approval boundary is not before the earning interval")
	}

	// the subnet identity: every field the epoch route can state is compared,
	// and a field the server cannot state is a reason to stop, not to skip
	domain := statement.Domain
	epoch := want.Epoch
	if epoch.ChainId == 0 || domain.ChainID != epoch.ChainId {
		return refuse("the statement's chain does not match the current epoch state")
	}
	if epoch.ContractAddress == "" || domain.Coordinator != common.HexToAddress(epoch.ContractAddress) {
		return refuse("the statement's coordinator does not match the current epoch state")
	}
	// the statement carries the v1 domain vault; the epoch route may point at the
	// v2 vault after the migration, so the v1 vault observed before it is also
	// accepted
	vaultOk := epoch.SettlementVaultAddress != "" && domain.SettlementVault == common.HexToAddress(epoch.SettlementVaultAddress)
	if !vaultOk && domain.SettlementVault != common.HexToAddress(snKnownV1SettlementVault) {
		return refuse("the statement's settlement vault does not match the current epoch state")
	}
	genesis, err := hex.DecodeString(strings.TrimPrefix(epoch.GenesisHash, "0x"))
	if err != nil || len(genesis) != 32 || [32]byte(genesis) != domain.GenesisHash {
		return refuse("the statement's genesis hash does not match the current epoch state")
	}
	netuid, err := epoch.Netuid.Int64()
	if err != nil || netuid <= 0 || uint64(netuid) != uint64(domain.Netuid) {
		return refuse("the statement's netuid does not match the current epoch state")
	}
	noId, err := epoch.NoId.Int64()
	if err != nil || noId <= 0 || uint64(noId) != domain.NoID {
		return refuse("the statement's operator id does not match the current epoch state")
	}

	// freshness: the text lives five minutes; bound it against the local clock
	issued := time.Unix(statement.IssuedAt, 0)
	expires := time.Unix(statement.ExpiresAt, 0)
	if want.Now.Add(snConsentClockSkew).Before(issued) {
		return refuse("the statement was issued in the future")
	}
	if !want.Now.Before(expires) {
		return refuse("the statement has expired")
	}

	return statement, nil
}

// snConsentJournalEntry is the consent retained before it is sent, so a lost
// acknowledgement can be answered by replaying the identical original: the same
// nonce and the same randomized signature, never a second signature.
type snConsentJournalEntry struct {
	ApiUrl     string   `json:"api_url"`
	Coldkey    string   `json:"coldkey_ss58"`
	Message    string   `json:"message"`
	Signature  string   `json:"signature"`
	Acked      bool     `json:"acked"`
	Generation uint64   `json:"generation"`
	Hash       [32]byte `json:"hash"`
	FromEpoch  uint64   `json:"from_epoch"`
}

func snConsentJournalPath() (string, error) {
	return providerStatePath("wallet_consent.json")
}

// snConsentApiKey normalizes the api url so a trailing slash or a different
// letter case does not split the journal key.
func snConsentApiKey(apiUrl string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(apiUrl), "/"))
}

// snConsentJournalLoad returns the entry for this API and coldkey, or nil.
func snConsentJournalLoad(path string, apiUrl string, coldkey string) (*snConsentJournalEntry, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []snConsentJournalEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("the wallet consent journal %s does not parse: %w", path, err)
	}
	for i := range entries {
		if snConsentApiKey(entries[i].ApiUrl) == snConsentApiKey(apiUrl) && entries[i].Coldkey == coldkey {
			entry := entries[i]
			return &entry, nil
		}
	}
	return nil, nil
}

// snConsentJournalStore takes the journal lock and writes the entry: the
// read-modify-write of snConsentJournalStoreLocked runs inside the lock, so
// two concurrent stores serialize and the second one's load already sees the
// first one's entry.
func snConsentJournalStore(path string, entry snConsentJournalEntry) error {
	release, err := snConsentJournalLockFn(path)
	if err != nil {
		return err
	}
	defer release()
	return snConsentJournalStoreLocked(path, entry)
}

// snConsentJournalStoreLocked writes the entry atomically at mode 0600: temp
// file in the same directory, fsync, rename. The caller holds the journal lock
// (snConsentJournalLock); without it the load below could miss a concurrent
// writer's entry and this write would drop it.
func snConsentJournalStoreLocked(path string, entry snConsentJournalEntry) error {
	var entries []snConsentJournalEntry
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("the wallet consent journal %s does not parse: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if snConsentJournalAfterLoadHook != nil {
		snConsentJournalAfterLoadHook()
	}
	replaced := false
	for i := range entries {
		if snConsentApiKey(entries[i].ApiUrl) == snConsentApiKey(entry.ApiUrl) && entries[i].Coldkey == entry.Coldkey {
			entries[i] = entry
			replaced = true
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".wallet_consent-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	// make the rename durable
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// snConsentJournalDrop takes the journal lock and removes the entry.
func snConsentJournalDrop(path string, apiUrl string, coldkey string) error {
	release, err := snConsentJournalLockFn(path)
	if err != nil {
		return err
	}
	defer release()
	return snConsentJournalDropLocked(path, apiUrl, coldkey)
}

// snConsentJournalDropLocked removes the entry for this api and coldkey. The
// caller holds the journal lock.
func snConsentJournalDropLocked(path string, apiUrl string, coldkey string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var entries []snConsentJournalEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	kept := entries[:0]
	for _, entry := range entries {
		if snConsentApiKey(entry.ApiUrl) == snConsentApiKey(apiUrl) && entry.Coldkey == coldkey {
			continue
		}
		kept = append(kept, entry)
	}
	raw, err = json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".wallet_consent-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// snConsentInterval is the interval to ask for: the explicit one, else the next
// earning epoch through the protocol's finite span.
func snConsentInterval(proof *snWalletProof, epoch *connect.SnEpochResult) (uint64, uint64, error) {
	if epoch == nil {
		return 0, 0, errors.New("wallet consent: the epoch state is unavailable")
	}
	if proof != nil && proof.fromEpoch != nil {
		from, through := uint64(*proof.fromEpoch), uint64(*proof.throughEpoch)
		// The server refuses any later network consent that does not start
		// after this one, and settlement does not fall back to an earlier
		// generation, so a mistaken interval cannot be undone: epochs before a
		// too-late start, or after a too-short end, stay unpaid.
		if os.Getenv("URNETWORK_WALLET_ALLOW_ODD_INTERVAL") != "1" {
			if from > epoch.Epoch+2 {
				return 0, 0, fmt.Errorf("wallet consent: --wallet-from-epoch=%d is far beyond the current epoch %d; epochs before it would be unpaid and the consent cannot be moved earlier (URNETWORK_WALLET_ALLOW_ODD_INTERVAL=1 overrides)", from, epoch.Epoch)
			}
			if through-from != snConsentSpan {
				return 0, 0, fmt.Errorf("wallet consent: the interval covers %d epochs, not the full %d; epochs after it would be unpaid (URNETWORK_WALLET_ALLOW_ODD_INTERVAL=1 overrides)", through-from+1, snConsentSpan+1)
			}
		}
		return from, through, nil
	}
	from := epoch.Epoch + 1
	return from, from + snConsentSpan, nil
}

// snRequestNetworkConsent asks the server for the statement and verifies it.
func snRequestNetworkConsent(
	ctx context.Context,
	api *connect.BringYourApi,
	coldkeySs58 string,
	want snNetworkConsentExpect,
) (string, *protocol.NetworkWalletMappingStatement, error) {
	result, err := api.SnNetworkWalletConsentSync(&connect.SnNetworkWalletConsentArgs{
		ColdkeySs58:  coldkeySs58,
		FromEpoch:    want.FromEpoch,
		ThroughEpoch: want.ThroughEpoch,
	})
	if err != nil {
		return "", nil, fmt.Errorf("wallet consent: %w", err)
	}
	if result.Error != nil {
		return "", nil, fmt.Errorf("wallet consent: %s", snPlain(result.Error.Message))
	}
	statement, err := snVerifyNetworkConsentMessage(result.Message, want)
	if err != nil {
		return "", nil, err
	}
	return result.Message, statement, nil
}

// snConsentRefused is a definite answer of the server: it looked at the consent
// and said no. A transport error is not one, and leaves the retained consent
// replayable.
type snConsentRefused struct{ message string }

func (e *snConsentRefused) Error() string { return e.message }

// snConsentRetainedMismatch says why a retained unacknowledged consent must not
// be replayed for this request, or "" when it is the same request.
func snConsentRetainedMismatch(entry *snConsentJournalEntry, want snNetworkConsentExpect, proof *snWalletProof) string {
	statement, err := protocol.DecodeNetworkWalletMappingStatement(entry.Message)
	if err != nil {
		return "it does not parse"
	}
	if statement.Coldkey != want.Coldkey || statement.UserId != want.Identity.UserId || statement.NetworkId != want.Identity.NetworkId {
		return "it is not for the saved login or this coldkey"
	}
	if proof != nil && proof.fromEpoch != nil && (statement.FromEpoch != uint64(*proof.fromEpoch) || statement.ThroughEpoch != uint64(*proof.throughEpoch)) {
		return "this request asks for a different interval"
	}
	if proof != nil && proof.Message != "" && snUnescapeMessage(proof.Message) != entry.Message {
		return "this request brings a different message"
	}
	return ""
}

// snConsentStoredRefusal says why this run must not sign a new consent: the
// journal already holds the identical consent, acknowledged by the server, and
// a second signature for it is never made. "" leaves signing open, and an
// unacknowledged entry is not a refusal here: snResolveRetainedConsent replays
// it instead.
func snConsentStoredRefusal(entry *snConsentJournalEntry, want snNetworkConsentExpect, proof *snWalletProof) string {
	if entry == nil || !entry.Acked {
		return ""
	}
	if snConsentRetainedMismatch(entry, want, proof) != "" {
		// a consent for another login, coldkey or interval is replaced, not reused
		return ""
	}
	statement, err := protocol.DecodeNetworkWalletMappingStatement(entry.Message)
	if err != nil || statement.FromEpoch != want.FromEpoch || statement.ThroughEpoch != want.ThroughEpoch {
		return ""
	}
	return fmt.Sprintf("the identical consent for epochs %d through %d is already stored and acknowledged; not signing a second one",
		statement.FromEpoch, statement.ThroughEpoch)
}

// snSubmitNetworkConsent verifies the signed original locally, sends it, and
// accepts the answer only when the server's receipt names the same content.
func snSubmitNetworkConsent(
	ctx context.Context,
	api *connect.BringYourApi,
	coldkeySs58 string,
	entry snConsentJournalEntry,
) (*connect.SnSetWalletResult, error) {
	signature, err := hex.DecodeString(strings.TrimPrefix(entry.Signature, "0x"))
	if err != nil || len(signature) != 64 {
		return nil, errors.New("wallet consent: the signature is not 64 bytes of hex")
	}
	original := protocol.WalletMappingConsent{Message: entry.Message, Signature: [64]byte(signature)}
	if _, _, err := protocol.VerifyNetworkWalletMappingConsent(ctx, original); err != nil {
		return nil, fmt.Errorf("wallet consent: the signature does not verify for the message: %w", err)
	}
	result, err := api.SnSetWalletSync(&connect.SnSetWalletArgs{
		ColdkeySs58: coldkeySs58,
		Message:     entry.Message,
		Signature:   entry.Signature,
	})
	if err != nil {
		return nil, fmt.Errorf("wallet consent: %w", err)
	}
	if result.Error != nil {
		code := ""
		if result.Error.Code != "" {
			code = " (" + snPlain(result.Error.Code) + ")"
		}
		return nil, &snConsentRefused{fmt.Sprintf("wallet consent: the server refused it%s: %s", code, snPlain(result.Error.Message))}
	}
	// a 200 with no receipt proves nothing: the receipt must name this consent
	want := hex.EncodeToString(entry.Hash[:])
	if !strings.EqualFold(strings.TrimPrefix(result.MappingHash, "0x"), want) {
		return nil, fmt.Errorf("wallet consent: the server's receipt %q does not name this consent; it may not be stored", snPlain(result.MappingHash))
	}
	if result.MappingGeneration != entry.Generation {
		return nil, fmt.Errorf("wallet consent: the server stored generation %d, expected %d", result.MappingGeneration, entry.Generation)
	}
	return result, nil
}

// snWalletNetworkConsentChallenge is `provider wallet challenge`: it prints the
// verified statement and how to sign it elsewhere, then submit the signature.
func snWalletNetworkConsentChallenge(ctx context.Context, out io.Writer, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, proof *snWalletProof) error {
	api, want, err := snConsentSetup(ctx, clientStrategy, apiUrl, coldkeySs58, proof)
	if err != nil {
		return err
	}
	defer api.Close()
	message, statement, err := snRequestNetworkConsent(ctx, api, coldkeySs58, want)
	if err != nil {
		return err
	}
	remaining := time.Until(time.Unix(statement.ExpiresAt, 0)).Round(time.Second)
	fmt.Fprintf(out, "Sign this message with the coldkey %s: sr25519, \"substrate\" signing context.\n", coldkeySs58)
	fmt.Fprintf(out, "It consents that earnings of every provider client of this network are paid to that coldkey for epochs %d through %d.\n", statement.FromEpoch, statement.ThroughEpoch)
	fmt.Fprintf(out, "It replaces any earlier network consent from epoch %d. A consent made for one provider client, if one exists, still takes precedence for that client.\n", statement.FromEpoch)
	fmt.Fprintf(out, "The bytes to sign are the UTF-8 text between the markers exactly as printed; a Polkadot signRaw of type \"bytes\", which wraps them in <Bytes>...</Bytes>, is accepted too.\n")
	fmt.Fprintf(out, "It expires in %s.\n", remaining)
	fmt.Fprintf(out, "\n----- message -----\n%s\n----- end -----\n", message)
	fmt.Fprintf(out, "message bytes (hex, for a signer that takes hex): 0x%s\n", hex.EncodeToString([]byte(message)))
	fmt.Fprintf(out, "\nThen, within that window, submit the 64-byte signature as hex:\n")
	fmt.Fprintf(out, "  provider wallet set %s --wallet-from-epoch=%d --wallet-through-epoch=%d --message='%s' --signature=0x<128 hex chars>\n",
		coldkeySs58, statement.FromEpoch, statement.ThroughEpoch, snEscapeMessage(message))
	return nil
}

// snConsentSetup resolves the pieces both commands need.
func snConsentSetup(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, proof *snWalletProof) (*connect.BringYourApi, snNetworkConsentExpect, error) {
	api, want, err := snConsentBase(ctx, clientStrategy, apiUrl)
	if err != nil {
		return nil, want, err
	}
	if err := snConsentBind(&want, apiUrl, coldkeySs58, proof); err != nil {
		api.Close()
		return nil, want, err
	}
	return api, want, nil
}

// snConsentBase resolves what does not depend on the coldkey: the saved
// network login and the epoch state.
func snConsentBase(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string) (*connect.BringYourApi, snNetworkConsentExpect, error) {
	var want snNetworkConsentExpect
	byJwt, err := readNetworkJwt()
	if err != nil {
		return nil, want, err
	}
	if want.Identity, err = snNetworkJwtIdentity(byJwt); err != nil {
		return nil, want, err
	}
	api := connect.NewBringYourApi(ctx, clientStrategy, apiUrl)
	api.SetByJwt(byJwt)
	if want.Epoch, err = api.SnEpochSync(); err != nil {
		api.Close()
		return nil, want, fmt.Errorf("wallet consent: the epoch state is unavailable: %w", err)
	}
	return api, want, nil
}

// snConsentBind adds the coldkey, its journal entry and the interval.
func snConsentBind(want *snNetworkConsentExpect, apiUrl string, coldkeySs58 string, proof *snWalletProof) error {
	pubkey, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("invalid ss58 coldkey %q: %s", snPlain(coldkeySs58), err)
	}
	want.Coldkey = pubkey
	path, err := snConsentJournalPath()
	if err != nil {
		return err
	}
	if want.Previous, err = snConsentJournalLoad(path, apiUrl, coldkeySs58); err != nil {
		return err
	}
	if want.FromEpoch, want.ThroughEpoch, err = snConsentInterval(proof, want.Epoch); err != nil {
		return err
	}
	want.Now = time.Now()
	return nil
}

// snResolveRetainedConsent takes the journal lock and hands the retained
// consent decision to snResolveRetainedConsentLocked; the browser signing page
// calls this directly.
func snResolveRetainedConsent(
	ctx context.Context,
	api *connect.BringYourApi,
	apiUrl string,
	coldkeySs58 string,
	journalPath string,
	want *snNetworkConsentExpect,
	proof *snWalletProof,
) (done bool, err error) {
	release, err := snConsentJournalLockFn(journalPath)
	if err != nil {
		return false, err
	}
	defer release()
	return snResolveRetainedConsentLocked(ctx, api, apiUrl, coldkeySs58, journalPath, want, proof)
}

// snResolveRetainedConsentLocked handles a consent that was sent but never
// acknowledged. It replays it exactly, but only when this request is the same
// one; a different interval, message or login replaces it, and a retained
// consent that no longer matches the saved login is dropped, so a stale entry
// can never take over a later request. done is true when the retained consent
// was replayed and acknowledged, so the caller has nothing left to do. The
// caller holds the journal lock.
func snResolveRetainedConsentLocked(
	ctx context.Context,
	api *connect.BringYourApi,
	apiUrl string,
	coldkeySs58 string,
	journalPath string,
	want *snNetworkConsentExpect,
	proof *snWalletProof,
) (done bool, err error) {
	previous := want.Previous
	if previous == nil || previous.Acked {
		return false, nil
	}
	if reason := snConsentRetainedMismatch(previous, *want, proof); reason != "" {
		fmt.Printf("discarding the retained, unacknowledged wallet consent: %s\n", reason)
		if err := snConsentJournalDropLocked(journalPath, apiUrl, coldkeySs58); err != nil {
			return false, err
		}
		want.Previous = nil
		return false, nil
	}
	fmt.Printf("replaying the retained, unacknowledged wallet consent for epochs from %d\n", previous.FromEpoch)
	if _, err := snSubmitNetworkConsent(ctx, api, coldkeySs58, *previous); err != nil {
		var refused *snConsentRefused
		if errors.As(err, &refused) {
			// a definite refusal: the retained original cannot succeed
			if dropErr := snConsentJournalDropLocked(journalPath, apiUrl, coldkeySs58); dropErr != nil {
				return false, errors.Join(err, dropErr)
			}
			return false, fmt.Errorf("%w; the retained consent was discarded, run the command again to sign a new one", err)
		}
		return false, err
	}
	previous.Acked = true
	if err := snConsentJournalStoreLocked(journalPath, *previous); err != nil {
		return false, err
	}
	fmt.Printf("subnet wallet consent for %s acknowledged\n", coldkeySs58)
	return true, nil
}

// snSignWalletChallengeFn is a test seam: the concurrency test counts the
// signatures two racing `wallet set` runs make, which must be exactly one for
// one consent.
var snSignWalletChallengeFn = snSignWalletChallenge

// snSetNetworkWalletConsent is `provider wallet set`: it replays an
// unacknowledged consent if one is retained, else requests, verifies, signs
// (from a seed file or an external signature), retains and submits a new one.
// It holds the journal lock across all of that, so a concurrent run for the
// same consent waits here and then replays or refuses the retained consent
// instead of signing a second one.
func snSetNetworkWalletConsent(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, proof *snWalletProof) error {
	if proof == nil || proof.unsigned() {
		return errors.New("wallet consent needs the coldkey's signature: use --coldkey_seed_file on your own machine, or `provider wallet challenge` and then --message with --signature")
	}
	if proof.SeedFile != "" {
		if err := snCheckSigningApiUrl(apiUrl); err != nil {
			return err
		}
	}
	api, want, err := snConsentSetup(ctx, clientStrategy, apiUrl, coldkeySs58, proof)
	if err != nil {
		return err
	}
	defer api.Close()
	journalPath, err := snConsentJournalPath()
	if err != nil {
		return err
	}
	// From the first journal read to the stored consent this run holds the
	// journal lock: a concurrent `wallet set` for the same consent waits here,
	// re-reads what this run retained, and replays or refuses it instead of
	// signing a second consent.
	releaseJournal, err := snConsentJournalLockFn(journalPath)
	if err != nil {
		return fmt.Errorf("wallet consent: cannot lock the journal: %w", err)
	}
	defer releaseJournal()
	// re-read under the lock: the load in the setup ran before it
	if want.Previous, err = snConsentJournalLoad(journalPath, apiUrl, coldkeySs58); err != nil {
		return err
	}

	if done, err := snResolveRetainedConsentLocked(ctx, api, apiUrl, coldkeySs58, journalPath, &want, proof); done || err != nil {
		return err
	}
	if refusal := snConsentStoredRefusal(want.Previous, want, proof); refusal != "" {
		return errors.New("wallet consent: " + refusal)
	}

	var message string
	var statement *protocol.NetworkWalletMappingStatement
	var signature string
	if proof.SeedFile != "" {
		// the statement is requested and verified first; the key is loaded only
		// for the signature itself
		message, statement, err = snRequestNetworkConsent(ctx, api, coldkeySs58, want)
		if err != nil {
			return err
		}
		keypair, err := snLoadColdkey(proof.SeedFile, coldkeySs58, want.Coldkey)
		if err != nil {
			return err
		}
		if signature, err = snSignWalletChallengeFn(keypair, message); err != nil {
			return err
		}
	} else {
		// an external signature: the message passed in must itself verify
		message = snUnescapeMessage(proof.Message)
		if statement, err = snVerifyNetworkConsentMessage(message, want); err != nil {
			return err
		}
		if signature, err = snNormalizeSignatureHex(proof.Signature); err != nil {
			return err
		}
	}

	result, err := snStoreNetworkConsentLocked(ctx, api, apiUrl, coldkeySs58, journalPath, message, signature, statement)
	if err != nil {
		return err
	}
	fmt.Printf("subnet wallet consent for %s stored: epochs %d through %d, generation %d, receipt %s\n",
		coldkeySs58, statement.FromEpoch, statement.ThroughEpoch, result.MappingGeneration, snPlain(result.MappingHash))
	fmt.Print(snConsentNextSteps(statement, want.Epoch.Epoch))
	return nil
}

// snStoreNetworkConsent takes the journal lock and hands the store to
// snStoreNetworkConsentLocked; the browser signing page calls this directly.
func snStoreNetworkConsent(
	ctx context.Context,
	api *connect.BringYourApi,
	apiUrl string,
	coldkeySs58 string,
	journalPath string,
	message string,
	signature string,
	statement *protocol.NetworkWalletMappingStatement,
) (*connect.SnSetWalletResult, error) {
	release, err := snConsentJournalLockFn(journalPath)
	if err != nil {
		return nil, fmt.Errorf("wallet consent: %w", err)
	}
	defer release()
	return snStoreNetworkConsentLocked(ctx, api, apiUrl, coldkeySs58, journalPath, message, signature, statement)
}

// snStoreNetworkConsentLocked verifies the signed original locally, retains it
// in the journal BEFORE sending, submits it, checks the server's receipt and
// marks it acknowledged. Both `wallet set` and the browser signing page end
// here. The caller holds the journal lock.
func snStoreNetworkConsentLocked(
	ctx context.Context,
	api *connect.BringYourApi,
	apiUrl string,
	coldkeySs58 string,
	journalPath string,
	message string,
	signature string,
	statement *protocol.NetworkWalletMappingStatement,
) (*connect.SnSetWalletResult, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil || len(raw) != 64 {
		return nil, errors.New("wallet consent: the signature is not 64 bytes of hex")
	}
	_, hash, err := protocol.VerifyNetworkWalletMappingConsent(ctx, protocol.WalletMappingConsent{Message: message, Signature: [64]byte(raw)})
	if err != nil {
		return nil, fmt.Errorf("wallet consent: the signature does not verify for the message: %w", err)
	}
	entry := snConsentJournalEntry{
		ApiUrl: apiUrl, Coldkey: coldkeySs58, Message: message, Signature: signature,
		Generation: statement.Generation, Hash: hash, FromEpoch: statement.FromEpoch,
	}
	// retain before sending: a lost acknowledgement is answered by this exact
	// original, never by a second signature
	if err := snConsentJournalStoreLocked(journalPath, entry); err != nil {
		return nil, fmt.Errorf("wallet consent: cannot retain the consent before sending it: %w", err)
	}
	result, err := snSubmitNetworkConsent(ctx, api, coldkeySs58, entry)
	if err != nil {
		return nil, err
	}
	entry.Acked = true
	if err := snConsentJournalStoreLocked(journalPath, entry); err != nil {
		return nil, err
	}
	return result, nil
}

// snWalletStatus is `provider wallet status`: what the server holds for this
// network, with the consent that pays settlement shown apart from a
// login-proof wallet, which release settlement never pays.
func snWalletStatus(ctx context.Context, out io.Writer, clientStrategy *connect.ClientStrategy, apiUrl string) error {
	byJwt, err := readNetworkJwt()
	if err != nil {
		return err
	}
	api := connect.NewBringYourApi(ctx, clientStrategy, apiUrl)
	defer api.Close()
	api.SetByJwt(byJwt)
	result, err := api.SnGetWalletSync()
	if err != nil {
		return fmt.Errorf("wallet status: %w", err)
	}
	if result.Error != nil {
		return fmt.Errorf("wallet status: %s", snPlain(result.Error.Message))
	}
	var epoch *connect.SnEpochResult
	if e, err := api.SnEpochSync(); err == nil {
		epoch = e
	}
	entries := result.Wallets
	if len(entries) == 0 && result.Wallet != nil {
		entries = []*connect.SnWalletEntry{result.Wallet}
	}
	if len(entries) == 0 {
		fmt.Fprintf(out, "no wallet is set for this network\n")
		return nil
	}
	paying := 0
	for _, entry := range entries {
		scope := entry.ConsentScope
		subject := "the network"
		if entry.ClientId != "" {
			subject = "client " + snPlain(entry.ClientId)
		}
		switch scope {
		case "":
			fmt.Fprintf(out, "login-proof wallet %s for %s (NOT a consent: release settlement does not pay it)\n", snPlain(entry.ColdkeySs58), subject)
		default:
			paying++
			current := ""
			if epoch != nil && entry.FromEpoch > 0 {
				switch {
				case int64(epoch.Epoch) < entry.FromEpoch:
					current = fmt.Sprintf(", starts in %d epoch(s)", entry.FromEpoch-int64(epoch.Epoch))
				case entry.ThroughEpoch > 0 && int64(epoch.Epoch) > entry.ThroughEpoch:
					current = ", ended"
				default:
					current = ", in effect now"
				}
			}
			fmt.Fprintf(out, "%s consent for %s: coldkey %s, epochs %d through %d (generation %d)%s\n",
				snPlain(scope), subject, snPlain(entry.ColdkeySs58), entry.FromEpoch, entry.ThroughEpoch, entry.MappingGeneration, current)
			if entry.MappingHash != "" {
				fmt.Fprintf(out, "  receipt %s\n", snPlain(entry.MappingHash))
			}
		}
	}
	if paying == 0 {
		fmt.Fprintf(out, "no consent is stored: sign one with `provider wallet challenge` and `provider wallet set`\n")
	}
	return nil
}

// snConsentNextSteps is what the operator does and does not have to do after a
// consent is stored, printed by `wallet set` and `wallet sign`.
func snConsentNextSteps(statement *protocol.NetworkWalletMappingStatement, currentEpoch uint64) string {
	var b strings.Builder
	b.WriteString("\nWhat happens next:\n")
	b.WriteString("  - You are done. Nothing changes on your provider machines; the consent covers every provider client on your network, including ones you add later.\n")
	if statement.FromEpoch > currentEpoch {
		gap := statement.FromEpoch - currentEpoch
		fmt.Fprintf(&b, "  - It applies from epoch %d, which starts in %d epoch(s) (the network is in epoch %d now). Earlier epochs are not covered by this consent.\n", statement.FromEpoch, gap, currentEpoch)
	} else {
		fmt.Fprintf(&b, "  - It applies from epoch %d.\n", statement.FromEpoch)
	}
	b.WriteString("  - Check it any time with: provider wallet status\n")
	fmt.Fprintf(&b, "  - Sign again only to change the wallet or after epoch %d. A later consent must start at a later epoch than this one.\n", statement.ThroughEpoch)
	return b.String()
}
