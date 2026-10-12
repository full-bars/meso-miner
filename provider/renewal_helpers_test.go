package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// --- jwtClientId ---------------------------------------------------------

func TestJwtClientId_Valid(t *testing.T) {
	jwt := createFakeJWTWithClaims(map[string]interface{}{
		"client_id": "client-abc-123",
		"exp":       float64(time.Now().Add(time.Hour).Unix()),
	})
	if got := jwtClientId(jwt); got != "client-abc-123" {
		t.Fatalf("jwtClientId = %q, want %q", got, "client-abc-123")
	}
}

func TestJwtClientId_MissingClaim(t *testing.T) {
	// A structurally valid JWT that simply has no client_id claim.
	jwt := createFakeJWTWithClaims(map[string]interface{}{
		"network_id": "net-1",
	})
	if got := jwtClientId(jwt); got != "" {
		t.Fatalf("jwtClientId with no client_id claim = %q, want empty string", got)
	}
}

func TestJwtClientId_Garbage(t *testing.T) {
	if got := jwtClientId("not-a-jwt"); got != "" {
		t.Fatalf("jwtClientId(garbage) = %q, want empty string", got)
	}
	if got := jwtClientId(""); got != "" {
		t.Fatalf("jwtClientId(\"\") = %q, want empty string", got)
	}
}

func TestJwtClientId_NonStringClaim(t *testing.T) {
	// A client_id claim that isn't a string must not be type-asserted into a
	// panic; jwtClientId should just report it as absent.
	jwt := createFakeJWTWithClaims(map[string]interface{}{
		"client_id": 12345,
	})
	if got := jwtClientId(jwt); got != "" {
		t.Fatalf("jwtClientId with non-string client_id claim = %q, want empty string", got)
	}
}

// --- accountNetworkId -----------------------------------------------------

func TestAccountNetworkId_Valid(t *testing.T) {
	jwt := createFakeJWTWithClaims(map[string]interface{}{
		"network_id": "net-xyz",
	})
	if got := accountNetworkId(jwt); got != "net-xyz" {
		t.Fatalf("accountNetworkId = %q, want %q", got, "net-xyz")
	}
}

func TestAccountNetworkId_MalformedTokenReturnsEmpty(t *testing.T) {
	// The store treats an empty NetworkID as "mint-fresh", so a malformed
	// account JWT must degrade to "" rather than panicking or erroring.
	if got := accountNetworkId("not-a-jwt"); got != "" {
		t.Fatalf("accountNetworkId(garbage) = %q, want empty string", got)
	}
	if got := accountNetworkId(""); got != "" {
		t.Fatalf("accountNetworkId(\"\") = %q, want empty string", got)
	}
}

func TestAccountNetworkId_MissingClaimReturnsEmpty(t *testing.T) {
	jwt := createFakeJWTWithClaims(map[string]interface{}{
		"client_id": "some-client",
	})
	if got := accountNetworkId(jwt); got != "" {
		t.Fatalf("accountNetworkId with no network_id claim = %q, want empty string", got)
	}
}

// --- readAccountJWT --------------------------------------------------------

func TestReadAccountJWT_Success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	urnetworkDir := filepath.Join(home, ".urnetwork")
	if err := os.MkdirAll(urnetworkDir, 0700); err != nil {
		t.Fatal(err)
	}
	const token = "account.jwt.token"
	if err := os.WriteFile(filepath.Join(urnetworkDir, "jwt"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readAccountJWT()
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("readAccountJWT = %q, want %q", got, token)
	}
}

func TestReadAccountJWT_TrimsWhitespace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	urnetworkDir := filepath.Join(home, ".urnetwork")
	if err := os.MkdirAll(urnetworkDir, 0700); err != nil {
		t.Fatal(err)
	}
	const token = "account.jwt.token"
	if err := os.WriteFile(filepath.Join(urnetworkDir, "jwt"), []byte("\n  "+token+"  \n"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readAccountJWT()
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("readAccountJWT did not trim whitespace: got %q, want %q", got, token)
	}
}

func TestReadAccountJWT_MissingFile(t *testing.T) {
	home := t.TempDir() // no .urnetwork/jwt inside
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if _, err := readAccountJWT(); err == nil {
		t.Fatal("expected an error when ~/.urnetwork/jwt does not exist")
	}
}

// --- providerDescription ---------------------------------------------------

func TestProviderDescription_UsesNodeName(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "")
	t.Setenv("URNETWORK_PUBLIC_IP", "")

	got := providerDescription("mybox")
	want := "mybox [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

func TestProviderDescription_FallsBackToHostHostnameWhenNodeNameEmpty(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "docker-host")
	t.Setenv("URNETWORK_PUBLIC_IP", "")

	got := providerDescription("")
	want := "docker-host [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

func TestProviderDescription_FallsBackToOSHostnameWhenNoOverridesSet(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "")
	t.Setenv("URNETWORK_PUBLIC_IP", "")

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("cannot determine os.Hostname() in this environment")
	}

	// Passing the real hostname explicitly as nodeName takes the exact same
	// downstream path (isContainerID / IP formatting) that providerDescription
	// takes internally when it falls back to os.Hostname(), so the two calls
	// must agree without this test having to reimplement that logic itself.
	got := providerDescription("")
	want := providerDescription(hostname)
	if got != want {
		t.Fatalf("providerDescription(\"\") = %q, want %q (matching providerDescription(%q))", got, want, hostname)
	}
}

func TestProviderDescription_ContainerIdNameWithoutPublicIpBecomesGenericProvider(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "abcdef012345") // 12-char hex -> matches containerIDRe
	t.Setenv("URNETWORK_PUBLIC_IP", "")

	got := providerDescription("")
	want := "provider [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

func TestProviderDescription_ContainerIdNameWithPublicIpUsesRedactedIpOnly(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "abcdef012345") // container id gibberish
	t.Setenv("URNETWORK_PUBLIC_IP", "203.0.113.42")

	got := providerDescription("")
	want := "203.x.x.42 [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

func TestProviderDescription_NodeNameWithPublicIpAppendsRedactedIp(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "")
	t.Setenv("URNETWORK_PUBLIC_IP", "203.0.113.42")

	got := providerDescription("mybox")
	want := "mybox @ 203.x.x.42 [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

func TestProviderDescription_NonIPv4PublicIpIsIgnored(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "")
	// Neither a valid IPv4 literal nor empty: the IPv4 parse must fail and
	// fall through to the no-IP branch instead of panicking on nil parts.
	for _, badIP := range []string{"not-an-ip", "::1", "2001:db8::1"} {
		t.Run(badIP, func(t *testing.T) {
			t.Setenv("URNETWORK_PUBLIC_IP", badIP)
			got := providerDescription("mybox")
			want := "mybox [" + RequireVersion() + "]"
			if got != want {
				t.Fatalf("providerDescription with public IP %q = %q, want %q", badIP, got, want)
			}
		})
	}
}

func TestProviderDescription_NodeNameItselfIsContainerId(t *testing.T) {
	// When nodeName is non-empty, the HOST_HOSTNAME/os.Hostname() fallback
	// branch is skipped entirely and displayName is set directly from
	// nodeName — this exercises container-id detection on that direct path
	// rather than on the fallback path (covered by the HOST_HOSTNAME tests
	// above).
	t.Setenv("HOST_HOSTNAME", "should-be-ignored")
	t.Setenv("URNETWORK_PUBLIC_IP", "")

	got := providerDescription("0123456789ab") // 12-char hex, non-empty nodeName
	want := "provider [" + RequireVersion() + "]"
	if got != want {
		t.Fatalf("providerDescription = %q, want %q", got, want)
	}
}

// --- watchReusedIdentityForRevocation / revocationDone ---------------------

func withTestClientJWTStore(t *testing.T) *clientJWTStore {
	t.Helper()
	storePath := t.TempDir() + "/client_jwts.json"
	store := newClientJWTStore(storePath)
	old := globalClientJWTStore
	globalClientJWTStore = store
	t.Cleanup(func() { globalClientJWTStore = old })
	return store
}

func TestWatchReusedIdentityForRevocation_ContextCancelReturnsPromptly(t *testing.T) {
	store := withTestClientJWTStore(t)
	if err := store.Put("proxy-ctx-cancel", clientJWTEntry{
		ByClientJWT: "some-token",
		ClientID:    "client-1",
		NetworkID:   "net-1",
		MintedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the watcher even starts

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchReusedIdentityForRevocation(ctx, "proxy-ctx-cancel", 987654, nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchReusedIdentityForRevocation did not return promptly on context cancellation")
	}

	if _, ok := store.Get("proxy-ctx-cancel"); !ok {
		t.Fatal("identity must not be evicted when the context is canceled before any tick fires")
	}
}

func TestWatchReusedIdentityForRevocation_RevocationDoneStandsDownImmediately(t *testing.T) {
	store := withTestClientJWTStore(t)
	if err := store.Put("proxy-revocation-done", clientJWTEntry{
		ByClientJWT: "some-token",
		ClientID:    "client-2",
		NetworkID:   "net-1",
		MintedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Simulates the renewal watcher having already closed revocationDone
	// (i.e. a successful renewal happened) before this watcher's select even
	// runs.
	revocationDone := make(chan struct{})
	close(revocationDone)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchReusedIdentityForRevocation(ctx, "proxy-revocation-done", 987655, revocationDone)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchReusedIdentityForRevocation did not stand down promptly when revocationDone was already closed")
	}

	if _, ok := store.Get("proxy-revocation-done"); !ok {
		t.Fatal("identity must not be evicted once the renewal watcher reported a successful renewal")
	}
}

func TestWatchReusedIdentityForRevocation_NilRevocationDoneDoesNotBlockContextCancel(t *testing.T) {
	// A nil revocationDone channel must behave like "never signaled" (receive
	// from nil blocks forever in the select) without panicking or otherwise
	// preventing the ctx.Done() case from firing.
	store := withTestClientJWTStore(t)
	if err := store.Put("proxy-nil-revocation", clientJWTEntry{
		ByClientJWT: "some-token",
		ClientID:    "client-3",
		NetworkID:   "net-1",
		MintedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchReusedIdentityForRevocation(ctx, "proxy-nil-revocation", 987656, nil)
	}()

	// Give the goroutine a moment to enter its select loop, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchReusedIdentityForRevocation with a nil revocationDone did not return on context cancellation")
	}

	if _, ok := store.Get("proxy-nil-revocation"); !ok {
		t.Fatal("identity must not be evicted on plain context cancellation")
	}
}

// --- renewClientJWT: context cancellation ----------------------------------

// TestRenewClientJWTContextCanceled pins the ctx.Done() early-return path:
// renewClientJWT must not block waiting on a slow/hanging backend once its
// context is canceled.
func TestRenewClientJWTContextCanceled(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // never responds until the test releases it
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before renewClientJWT is even called

	errCh := make(chan error, 1)
	jwtCh := make(chan string, 1)
	go func() {
		byJwt, err := renewClientJWT(ctx, server.URL, "account-jwt", connect.NewId(), "test", nil)
		jwtCh <- byJwt
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error when renewClientJWT is called with an already-canceled context")
		}
		if got := <-jwtCh; got != "" {
			t.Fatalf("renewClientJWT returned a non-empty JWT on context cancellation: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("renewClientJWT did not return promptly on context cancellation")
	}
}

// --- identity eviction: only an explicit rejection, never during an outage ---

type rejectedAuthError struct{}

func (rejectedAuthError) Error() string      { return "websocket: bad handshake (http 401)" }
func (rejectedAuthError) AuthRejected() bool { return true }

func shortRevocationCheck(t *testing.T) {
	t.Helper()
	prev := revocationCheckInterval
	revocationCheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { revocationCheckInterval = prev })
}

func runRevocationWatcher(ctx context.Context, key string, index int) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchReusedIdentityForRevocation(ctx, key, index, nil)
	}()
	return done
}

func seedIdentity(t *testing.T, store *clientJWTStore, key string) {
	t.Helper()
	if err := store.Put(key, clientJWTEntry{ByClientJWT: "tok", ClientID: "client-" + key, NetworkID: "net", MintedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

// The incident: the backend is slow, every auth times out, nothing has come up
// yet. That is not a revoked identity and the saved identity must survive.
func TestRevocationWatcher_TimeoutsNeverEvictAnIdentity(t *testing.T) {
	shortRevocationCheck(t)
	store := withTestClientJWTStore(t)
	connect.ResetProxyHealthForTesting()
	connect.RegisterProxy(701, "to.example:1080", "to.example:1080")
	seedIdentity(t, store, "to.example:1080")

	for i := 0; i < 50; i++ {
		connect.RecordProxyAuthFailure(701, errors.New("Timeout. 3 dial(s) failed over 1 round(s) in 15s, last: i/o timeout"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := runRevocationWatcher(ctx, "to.example:1080", 701)
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if _, ok := store.Get("to.example:1080"); !ok {
		t.Fatal("an identity was evicted because the backend was slow")
	}
}

func TestRevocationWatcher_StillEvictsAnIdentityThePlatformRejects(t *testing.T) {
	shortRevocationCheck(t)
	prev := identityEvictionBlocked
	identityEvictionBlocked = func() bool { return false }
	defer func() { identityEvictionBlocked = prev }()
	prevOn := identityEvictionEnabled
	identityEvictionEnabled = func() bool { return true } // the operator opted in
	defer func() { identityEvictionEnabled = prevOn }()

	store := withTestClientJWTStore(t)
	connect.ResetProxyHealthForTesting()
	connect.RegisterProxy(702, "rev.example:1080", "rev.example:1080")
	seedIdentity(t, store, "rev.example:1080")

	for i := 0; i < revokedIdentityAuthFailureThreshold; i++ {
		connect.RecordProxyAuthFailure(702, rejectedAuthError{})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runRevocationWatcher(ctx, "rev.example:1080", 702)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a genuinely rejected identity was never evicted")
	}
	if _, ok := store.Get("rev.example:1080"); ok {
		t.Fatal("a rejected identity should be evicted so it can be re-minted")
	}
}

// Even a real-looking rejection is held while the backend is degraded: during
// an outage the platform's answers cannot be trusted to mean "revoked".
func TestRevocationWatcher_NeverEvictsWhileTheBackendIsDegraded(t *testing.T) {
	shortRevocationCheck(t)
	var degraded atomic.Bool
	degraded.Store(true)
	prevOn := identityEvictionEnabled
	identityEvictionEnabled = func() bool { return true }
	defer func() { identityEvictionEnabled = prevOn }()
	prev := identityEvictionBlocked
	identityEvictionBlocked = func() bool { return degraded.Load() }
	defer func() { identityEvictionBlocked = prev }()

	store := withTestClientJWTStore(t)
	connect.ResetProxyHealthForTesting()
	connect.RegisterProxy(703, "deg.example:1080", "deg.example:1080")
	seedIdentity(t, store, "deg.example:1080")
	for i := 0; i < 20; i++ {
		connect.RecordProxyAuthFailure(703, rejectedAuthError{})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runRevocationWatcher(ctx, "deg.example:1080", 703)

	time.Sleep(250 * time.Millisecond)
	if _, ok := store.Get("deg.example:1080"); !ok {
		t.Fatal("an identity was evicted during a backend outage")
	}

	// once the backend is healthy again the rejection stands and the identity goes
	degraded.Store(false)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watcher never resumed after the outage ended")
	}
	if _, ok := store.Get("deg.example:1080"); ok {
		t.Fatal("after the outage a persistent explicit rejection should still evict")
	}
}

// Deleting a saved identity loses its history and mints a new one, and the cases
// where the platform really refuses a revoked token cannot be told apart from a
// WAF or a gateway saying no. So eviction is something an operator turns on, not
// something a bad hour does to them.
func TestRevocationWatcher_NeverEvictsByDefault(t *testing.T) {
	shortRevocationCheck(t)
	prev := identityEvictionBlocked
	identityEvictionBlocked = func() bool { return false }
	defer func() { identityEvictionBlocked = prev }()

	store := withTestClientJWTStore(t)
	connect.ResetProxyHealthForTesting()
	connect.RegisterProxy(704, "dflt.example:1080", "dflt.example:1080")
	seedIdentity(t, store, "dflt.example:1080")
	for i := 0; i < 50; i++ {
		connect.RecordProxyAuthFailure(704, rejectedAuthError{})
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := runRevocationWatcher(ctx, "dflt.example:1080", 704)
	time.Sleep(250 * time.Millisecond)
	cancel()
	<-done

	if _, ok := store.Get("dflt.example:1080"); !ok {
		t.Fatal("an identity was deleted although automatic eviction is off by default")
	}
}

func TestIdentityEvictionEnabled_IsAnExplicitOptIn(t *testing.T) {
	t.Setenv("URNETWORK_EVICT_REVOKED_IDENTITIES", "")
	if identityEvictionEnabled() {
		t.Fatal("eviction must be off unless asked for")
	}
	t.Setenv("URNETWORK_EVICT_REVOKED_IDENTITIES", "1")
	if !identityEvictionEnabled() {
		t.Fatal("URNETWORK_EVICT_REVOKED_IDENTITIES=1 turns it on")
	}
}

func TestRenewalHTTPRefusal_OverloadAndGatewaysAreNotARefusal(t *testing.T) {
	prev := renewalBackendDegraded
	renewalBackendDegraded = func() bool { return false }
	defer func() { renewalBackendDegraded = prev }()

	cases := []struct {
		msg  string
		want bool
	}{
		{"401 Unauthorized: bad token", true},
		{"403 Forbidden: nope", true},
		{"404 Not Found: x", true},
		{"408 Request Timeout: slow", false},
		{"429 Too Many Requests: slow down", false},
		{"499 Client Closed Request: load balancer", false}, // an nginx gateway, not an answer about the token
		{"500 Internal Server Error: boom", false},
		{"503 Service Unavailable: db", false},
	}
	for _, c := range cases {
		if got := renewalHTTPRefusal(errors.New(c.msg)); got != c.want {
			t.Errorf("%q: refusal=%v, want %v", c.msg, got, c.want)
		}
	}

	// during an outage even a 401 or 403 is not trusted: minting a fresh identity
	// off a refusal the degraded backend (or its gateway) produced loses the old one
	renewalBackendDegraded = func() bool { return true }
	for _, msg := range []string{"401 Unauthorized: x", "403 Forbidden: x", "404 Not Found: x"} {
		if renewalHTTPRefusal(errors.New(msg)) {
			t.Errorf("%q counted as a refusal while the backend is degraded", msg)
		}
	}
}
