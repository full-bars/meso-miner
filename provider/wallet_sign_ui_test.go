package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// newSignUiForTest wires the signing page to the fake platform of the consent
// tests and serves its handler.
func newSignUiForTest(t *testing.T, seed []byte) (*snSignUi, *consentServer, *httptest.Server, string) {
	t.Helper()
	return newSignUiWithBinding(t, seed, true)
}

// newSignUiWithBinding builds the page; with bind false no account is chosen
// yet, as when the command is run without an address.
func newSignUiWithBinding(t *testing.T, seed []byte, bind bool) (*snSignUi, *consentServer, *httptest.Server, string) {
	t.Helper()
	consentTestHome(t)
	address := walletTestAddress(t, seed)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now())}
	platform := httptest.NewServer(fake.handler())
	t.Cleanup(platform.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api := connect.NewBringYourApi(ctx, connect.NewClientStrategyWithDefaults(ctx), platform.URL)
	user, network := connect.Id(consentTestUser), connect.Id(consentTestNetwork)
	api.SetByJwt(createFakeJWTWithClaims(map[string]interface{}{"user_id": user.String(), "network_id": network.String()}))
	path, err := snConsentJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	ui := &snSignUi{
		ctx: ctx, api: api, apiUrl: platform.URL, journalPath: path,
		token: "test-token", deadline: time.Now().Add(snSignPageLifetime), want: fake.want, done: make(chan struct{}),
	}
	if bind {
		ui.mutex.Lock()
		if err := ui.bindWithLock(address); err != nil {
			t.Fatal(err)
		}
		ui.mutex.Unlock()
	}
	page := httptest.NewServer(ui.handler())
	t.Cleanup(page.Close)
	return ui, fake, page, address
}

func signUiPost(t *testing.T, base string, path string, token string, host string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-Session", token)
	}
	if host != "" {
		request.Host = host
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return response.StatusCode, out
}

func TestSignPageServesTheVerifiedMessageAndTheLogo(t *testing.T) {
	_, _, page, address := newSignUiForTest(t, walletTestSeed(0x11))
	response, err := http.Get(page.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	text := string(body)
	for _, want := range []string{address, "Approve URnetwork network wallet mapping", "<svg", "This message expires in", "This page closes in"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the page lacks %q", want)
		}
	}
	if strings.Contains(text, "{{") {
		t.Fatal("an unfilled placeholder is left in the page")
	}
	if csp := response.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("no content security policy: %q", csp)
	}
	// other paths and a rebound host are not served
	if other, _ := http.Get(page.URL + "/state"); other.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown path answered %d", other.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, page.URL+"/", nil)
	request.Host = "evil.example"
	rebound, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.StatusCode != http.StatusNotFound {
		t.Fatalf("a rebound host was served the page (%d)", rebound.StatusCode)
	}
}

func TestSignPageSignatureStoresTheConsentTheServerHolds(t *testing.T) {
	seed := walletTestSeed(0x11)
	ui, fake, page, _ := newSignUiForTest(t, seed)

	// the extension signs the message the page showed
	keypair := walletTestKeypair(t, seed)
	signature, err := snSignWalletChallenge(keypair, ui.message)
	if err != nil {
		t.Fatal(err)
	}

	// a forged session, another host and an expired page are refused
	if status, _ := signUiPost(t, page.URL, "/signature", "wrong", "", map[string]string{"signature": signature}); status != http.StatusForbidden {
		t.Fatalf("a wrong session token answered %d", status)
	}
	if status, _ := signUiPost(t, page.URL, "/signature", "test-token", "evil.example", map[string]string{"signature": signature}); status != http.StatusForbidden {
		t.Fatalf("a foreign host answered %d", status)
	}
	if status, _ := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]string{"signature": "0x12"}); status != http.StatusBadRequest {
		t.Fatalf("a short signature answered %d", status)
	}
	if len(fake.submissions) != 0 {
		t.Fatal("a refused request reached the platform")
	}

	// a message sent by the page is ignored: the server stores what it verified
	status, out := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]any{"signature": signature, "message": "Sign in to URnetwork"})
	if status != http.StatusOK || out["ok"] != true {
		t.Fatalf("the signature was not stored: %d %v", status, out)
	}
	if len(fake.submissions) != 1 || fake.submissions[0].Message != ui.message {
		t.Fatal("the platform did not receive the verified message")
	}
	var statement *protocol.NetworkWalletMappingStatement
	statement, err = protocol.DecodeNetworkWalletMappingStatement(fake.submissions[0].Message)
	if err != nil || out["receipt"] == "" || uint64(out["from"].(float64)) != statement.FromEpoch {
		t.Fatalf("unexpected result %v %v", out, err)
	}
	path, _ := snConsentJournalPath()
	if entry, _ := snConsentJournalLoad(path, ui.apiUrl, ui.coldkey); entry == nil || !entry.Acked {
		t.Fatal("the stored consent is not journaled as acknowledged")
	}
	// a second signature is refused
	if status, _ := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]string{"signature": signature}); status != http.StatusConflict {
		t.Fatalf("a second signature answered %d", status)
	}
}

func TestSignPageRefreshesWithinLimitsAndStops(t *testing.T) {
	ui, fake, page, _ := newSignUiForTest(t, walletTestSeed(0x11))
	before := fake.challenges
	// the first refresh is rate limited right after the page was made
	ui.mutex.Lock()
	ui.lastRefresh = time.Now().Add(-time.Minute)
	ui.mutex.Unlock()
	if status, _ := signUiPost(t, page.URL, "/refresh", "test-token", "", nil); status != http.StatusOK {
		t.Fatalf("a refresh answered %d", status)
	}
	if fake.challenges != before+1 {
		t.Fatal("a refresh did not ask the platform for a new message")
	}
	if status, _ := signUiPost(t, page.URL, "/refresh", "test-token", "", nil); status != http.StatusTooManyRequests {
		t.Fatalf("an immediate second refresh answered %d", status)
	}
	// the page stops at its deadline
	ui.mutex.Lock()
	ui.deadline = time.Now().Add(-time.Second)
	ui.mutex.Unlock()
	if status, _ := signUiPost(t, page.URL, "/refresh", "test-token", "", nil); status != http.StatusGone {
		t.Fatalf("a stopped page answered %d", status)
	}
}

func TestSignPageRefusesAnExpiredMessage(t *testing.T) {
	seed := walletTestSeed(0x11)
	ui, fake, page, _ := newSignUiForTest(t, seed)
	signature, err := snSignWalletChallenge(walletTestKeypair(t, seed), ui.message)
	if err != nil {
		t.Fatal(err)
	}
	ui.mutex.Lock()
	ui.statement.ExpiresAt = time.Now().Add(-time.Second).Unix()
	ui.mutex.Unlock()
	if status, _ := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]string{"signature": signature}); status != http.StatusGone {
		t.Fatalf("an expired message answered %d", status)
	}
	if len(fake.submissions) != 0 {
		t.Fatal("an expired message reached the platform")
	}
}

var _ = hex.EncodeToString

// Serves the page with a fake platform so its look can be checked by eye:
//
//	URNETWORK_SIGN_PREVIEW=8770 go test -run TestSignPagePreview ./provider/
func TestSignPagePreview(t *testing.T) {
	port := os.Getenv("URNETWORK_SIGN_PREVIEW")
	if port == "" {
		t.Skip("set URNETWORK_SIGN_PREVIEW=<port> to serve the page for 15 minutes")
	}
	ui, _, _, _ := newSignUiWithBinding(t, walletTestSeed(0x11), os.Getenv("URNETWORK_SIGN_PREVIEW_UNBOUND") == "")
	ui.token = "preview"
	ui.preview = true
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: ui.handler()}
	go func() { _ = server.ListenAndServe() }()
	defer server.Close()
	t.Logf("preview at http://127.0.0.1:%s/ (session token: preview)", port)
	time.Sleep(15 * time.Minute)
}

func TestSignPageExtendResetsThePageTimerWithinLimits(t *testing.T) {
	ui, _, page, _ := newSignUiForTest(t, walletTestSeed(0x11))
	ui.mutex.Lock()
	ui.deadline = time.Now().Add(30 * time.Second)
	ui.mutex.Unlock()
	for i := 0; i < snSignMaxExtensions; i++ {
		status, out := signUiPost(t, page.URL, "/extend", "test-token", "", nil)
		if status != http.StatusOK {
			t.Fatalf("extension %d answered %d", i+1, status)
		}
		if left := time.Until(time.Unix(int64(out["pageDeadline"].(float64)), 0)); left < snSignPageLifetime-5*time.Second {
			t.Fatalf("the page timer was reset to only %s", left)
		}
	}
	if status, _ := signUiPost(t, page.URL, "/extend", "test-token", "", nil); status != http.StatusTooManyRequests {
		t.Fatalf("an extension beyond the limit answered %d", status)
	}
}

// Without an address on the command line the account is chosen on the page from
// the wallet extension's accounts, and everything waits for that choice.
func TestSignPageChoosesTheAccountOnThePage(t *testing.T) {
	seed := walletTestSeed(0x11)
	ui, fake, page, address := newSignUiWithBinding(t, seed, false)
	if status, _ := signUiPost(t, page.URL, "/refresh", "test-token", "", nil); status != http.StatusConflict {
		t.Fatalf("a refresh before an account was chosen answered %d", status)
	}
	if status, _ := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]string{"signature": "0x" + strings.Repeat("ab", 64)}); status != http.StatusConflict {
		t.Fatalf("a signature before an account was chosen answered %d", status)
	}
	if status, _ := signUiPost(t, page.URL, "/select", "test-token", "", map[string]string{"address": "not-an-address"}); status != http.StatusBadRequest {
		t.Fatalf("a malformed address answered %d", status)
	}
	if fake.challenges != 0 {
		t.Fatal("a statement was requested before a valid account was chosen")
	}
	status, out := signUiPost(t, page.URL, "/select", "test-token", "", map[string]string{"address": address})
	if status != http.StatusOK || out["address"] != address {
		t.Fatalf("the account was not accepted: %d %v", status, out)
	}
	if fake.challenges != 1 || ui.coldkey != address {
		t.Fatal("choosing the account did not request its statement")
	}
	// another account cannot be chosen afterwards in the same run
	other := walletTestAddress(t, walletTestSeed(0x55))
	if status, _ := signUiPost(t, page.URL, "/select", "test-token", "", map[string]string{"address": other}); status != http.StatusConflict {
		t.Fatalf("a second account answered %d", status)
	}
	// and the chosen account can now sign
	signature, err := snSignWalletChallenge(walletTestKeypair(t, seed), ui.message)
	if err != nil {
		t.Fatal(err)
	}
	if status, out := signUiPost(t, page.URL, "/signature", "test-token", "", map[string]string{"signature": signature}); status != http.StatusOK || out["ok"] != true {
		t.Fatalf("the chosen account could not sign: %d %v", status, out)
	}
}

// A new message is a fresh start: both the message and the page timers restart,
// so the operator never has to also extend the page.
func TestSignPageNewMessageRestartsBothTimers(t *testing.T) {
	ui, _, page, _ := newSignUiForTest(t, walletTestSeed(0x11))
	ui.mutex.Lock()
	ui.deadline = time.Now().Add(20 * time.Second)
	ui.lastRefresh = time.Now().Add(-time.Minute)
	oldExpiry := ui.statement.ExpiresAt
	ui.mutex.Unlock()
	time.Sleep(1100 * time.Millisecond)
	status, out := signUiPost(t, page.URL, "/refresh", "test-token", "", nil)
	if status != http.StatusOK {
		t.Fatalf("a new message answered %d", status)
	}
	if int64(out["expiresAt"].(float64)) <= oldExpiry {
		t.Fatal("the message timer did not move forward")
	}
	if left := time.Until(time.Unix(int64(out["pageDeadline"].(float64)), 0)); left < snSignPageLifetime-5*time.Second {
		t.Fatalf("the page timer was left at %s", left)
	}
	if out["currentEpoch"].(float64) <= 0 {
		t.Fatal("the current epoch is missing, the page cannot say when the consent starts")
	}
}
