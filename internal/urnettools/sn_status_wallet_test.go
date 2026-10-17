package urnettools

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// The sn-status wallet consent: the network consent the platform reports is
// shown apart from a legacy login-proof wallet, and when no consent exists
// the output says how to sign one.

func snStatusWalletTestProvider(t *testing.T) Provider {
	t.Helper()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "jwt"), []byte("test-jwt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "api_url"), []byte("http://unused"), 0600); err != nil {
		t.Fatal(err)
	}
	return Provider{Unit: "urnetwork-test.service", User: "testuser", StateDir: tmpDir, NetworkID: "net-uuid"}
}

func snStatusWalletTestMock(t *testing.T, wallet *connect.SnGetWalletResult) *mockSnStatusAPI {
	t.Helper()
	mock := &mockSnStatusAPI{
		ranking: &connect.NetworkRankingResult{
			NetworkRanking: &connect.NetworkRanking{NetMibCount: 1, LeaderboardRank: 3},
		},
		epoch: &connect.SnEpochResult{
			Epoch:           100,
			StartBlock:      100000,
			FinalizeBlock:   100720,
			ContractAddress: "0x1111222233334444555566667777888899990000",
			ChainId:         964,
		},
		claim:  &connect.SnPoolClaimResult{Epoch: 99, ShareBps: 100, PayoutRoot: make([]byte, 32)},
		wallet: wallet,
	}
	origAPI := newSnStatusAPI
	newSnStatusAPI = func(_ context.Context, _ string, _ string) snStatusAPI { return mock }
	t.Cleanup(func() { newSnStatusAPI = origAPI })
	return mock
}

func captureSnStatusRender(t *testing.T, info *SnStatusInfo) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	renderSnStatusDashboard(info)
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestFetchSnStatusShowsTheNetworkConsentApartFromLoginProof(t *testing.T) {
	loginProofColdkey := "5FjfHgd4K3H5Vge2igPtBYyWRbRKdgH84roTCnWwwtNgAhU5"
	var consentPubkey [32]byte
	consentPubkey[0] = 7
	consentColdkey, err := ss58.Encode(consentPubkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}

	p := snStatusWalletTestProvider(t)
	snStatusWalletTestMock(t, &connect.SnGetWalletResult{
		Wallets: []*connect.SnWalletEntry{
			{ColdkeySs58: loginProofColdkey}, // no consent scope: a login proof
			{
				ColdkeySs58:       consentColdkey,
				ConsentScope:      "network",
				FromEpoch:         101,
				ThroughEpoch:      65636,
				MappingGeneration: 3,
			},
		},
	})

	info, err := FetchSnStatus(p)
	if err != nil {
		t.Fatalf("FetchSnStatus: %v", err)
	}
	if info.WalletConsent == nil {
		t.Fatal("the network consent entry must be summarized")
	}
	if info.WalletConsent.ColdkeySs58 != consentColdkey || info.WalletConsent.FromEpoch != 101 || info.WalletConsent.ThroughEpoch != 65636 {
		t.Fatalf("consent = %+v", info.WalletConsent)
	}
	if info.WalletConsent.State != "starts in 1 epoch(s)" {
		t.Fatalf("state = %q, want starts in 1 epoch(s) at epoch 100", info.WalletConsent.State)
	}
	if len(info.WalletLoginProofColdkeys) != 1 || info.WalletLoginProofColdkeys[0] != loginProofColdkey {
		t.Fatalf("login proofs = %v", info.WalletLoginProofColdkeys)
	}

	out := captureSnStatusRender(t, info)
	if !strings.Contains(out, "Wallet consent:") || !strings.Contains(out, consentColdkey) || !strings.Contains(out, "starts in 1 epoch(s)") {
		t.Fatalf("render misses the consent: %q", out)
	}
	if !strings.Contains(out, "not a consent") || !strings.Contains(out, loginProofColdkey) {
		t.Fatalf("render must mark the login-proof wallet apart: %q", out)
	}
}

func TestFetchSnStatusWithoutConsentSaysHowToSignOne(t *testing.T) {
	loginProofColdkey := "5FjfHgd4K3H5Vge2igPtBYyWRbRKdgH84roTCnWwwtNgAhU5"
	p := snStatusWalletTestProvider(t)
	snStatusWalletTestMock(t, &connect.SnGetWalletResult{
		Wallets: []*connect.SnWalletEntry{{ColdkeySs58: loginProofColdkey}},
	})
	info, err := FetchSnStatus(p)
	if err != nil {
		t.Fatalf("FetchSnStatus: %v", err)
	}
	if info.WalletConsent != nil {
		t.Fatal("a login-proof wallet is not a consent")
	}
	out := captureSnStatusRender(t, info)
	if !strings.Contains(out, "urnet-tools wallet sign") {
		t.Fatalf("no-consent output must say how to sign one: %q", out)
	}

	// and with no wallet entries at all the hint stands alone
	p2 := snStatusWalletTestProvider(t)
	snStatusWalletTestMock(t, &connect.SnGetWalletResult{})
	info2, err := FetchSnStatus(p2)
	if err != nil {
		t.Fatalf("FetchSnStatus: %v", err)
	}
	out2 := captureSnStatusRender(t, info2)
	if !strings.Contains(out2, "no consent stored") || !strings.Contains(out2, "urnet-tools wallet sign") {
		t.Fatalf("empty wallet output must say how to sign one: %q", out2)
	}
}

func TestFetchSnStatusWalletFetchFailureDegradesOnlyTheWallet(t *testing.T) {
	p := snStatusWalletTestProvider(t)
	snStatusWalletTestMock(t, nil) // the mock returns an error for a nil wallet
	info, err := FetchSnStatus(p)
	if err != nil {
		t.Fatalf("a wallet-fetch failure must not fail the whole status: %v", err)
	}
	if info.WalletConsentUnavailable == "" {
		t.Fatal("the wallet failure must surface as an unavailable state")
	}
	if info.Error != "" {
		t.Fatalf("the wallet failure must not feed the ranking/epoch error tally: %q", info.Error)
	}
	out := captureSnStatusRender(t, info)
	if !strings.Contains(out, "Wallet consent:") || !strings.Contains(out, "unavailable") {
		t.Fatalf("render must show the unavailable state: %q", out)
	}
}
