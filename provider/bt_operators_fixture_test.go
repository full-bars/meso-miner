package main

// Fixtures for the operators tests, ported from the sn miner's
// operators_fixture_test.go (upstream 4ca8316e) minus the wallet sign-in,
// auth-code and password routes: the v33 operators surface has no sign-in, so
// the synthetic operator here only serves the published list.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/operatorlist"
)

// operatorTestHome points HOME at an empty directory and clears
// URNETWORK_STATE_DIR, as the other provider tests do, so the state directory
// is the test's own ~/.urnetwork. It returns the home directory.
func operatorTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("URNETWORK_STATE_DIR", "")
	return home
}

// parseOperatorsArgsForTest parses argv against the real usage.
func parseOperatorsArgsForTest(t *testing.T, argv []string) docopt.Opts {
	t.Helper()
	opts, err := docopt.ParseArgs(fmt.Sprintf(mainUsage(), DefaultApiUrl, DefaultConnectUrl), argv, "synthetic")
	if err != nil {
		t.Fatalf("parse %v: %v", argv, err)
	}
	return opts
}

// A synthetic operator that serves the published list.
type testOperatorListServer struct {
	server *httptest.Server

	stateLock  sync.Mutex
	listYaml   string
	listOnline bool
}

func newTestOperatorListServer(t *testing.T) *testOperatorListServer {
	t.Helper()
	self := &testOperatorListServer{listOnline: true}
	self.server = httptest.NewServer(self)
	t.Cleanup(self.server.Close)
	return self
}

func (self *testOperatorListServer) listUrl() string {
	return self.server.URL + "/operators.yml"
}

// Serves operators as the published list and returns its digest.
func (self *testOperatorListServer) setList(t *testing.T, operators ...operatorlist.Operator) string {
	t.Helper()
	raw := testOperatorListYaml(t, operators...)
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.listYaml = raw
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (self *testOperatorListServer) setListOnline(online bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.listOnline = online
}

func (self *testOperatorListServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if request.URL.Path != "/operators.yml" {
		http.NotFound(writer, request)
		return
	}
	if !self.listOnline || self.listYaml == "" {
		http.Error(writer, "503 synthetic outage", http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/yaml")
	_, _ = writer.Write([]byte(self.listYaml))
}

// Synthetic operators under .example, with the urls the published list
// would carry.
func testListedOperator(domain string) operatorlist.Operator {
	return operatorlist.Operator{Domain: domain, ApiUrl: "https://api." + domain, ConnectUrl: "wss://connect." + domain}
}

// A valid published list, so every fixture passes the client's own checks.
func testOperatorListYaml(t *testing.T, operators ...operatorlist.Operator) string {
	t.Helper()
	var raw strings.Builder
	raw.WriteString("schema: " + operatorlist.Schema + "\noperators:\n")
	for _, operator := range operators {
		fmt.Fprintf(&raw, "  - domain: %s\n    api_url: %s\n    connect_url: %s\n", operator.Domain, operator.ApiUrl, operator.ConnectUrl)
	}
	if _, err := operatorlist.Parse([]byte(raw.String())); err != nil {
		t.Fatal(err)
	}
	return raw.String()
}

// writeOperatorJwt writes one operator's jwt the way the operator would:
// owner-only, under the operator's state directory.
func writeOperatorJwt(t *testing.T, base string, domain string, token string) {
	t.Helper()
	path := operatorJwtPath(base, domain)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
}

func mustReadOperatorFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
