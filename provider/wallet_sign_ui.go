package main

// wallet_sign_ui.go -- `provider wallet sign`: one local page where a browser
// wallet extension signs the network wallet consent.
//
// The page is served on 127.0.0.1 only, for a bounded time, with a visible
// countdown. It hands the verified statement to the extension and receives the
// signature (a public value). It never sees a seed phrase or a private key; the
// extension signs. The server holds the statement itself and never takes the
// message back from the page, so the page cannot get a different message stored.

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/protocol"

	"github.com/urnetwork/connect"
)

//go:embed wallet_sign_page.html
var snSignPageHtml string

// the logo is the URnetwork wordmark from the URnetwork web repository
// (MPL-2.0, https://github.com/urnetwork/web), inlined so the page needs no
// network and no font or image downloads
//
//go:embed wallet_sign_logo.svg
var snSignLogoSvg string

const (
	// how long the page is served; the statement itself lives five minutes and
	// can be renewed from the page
	snSignPageLifetime = 15 * time.Minute
	// the server keeps a few unused challenges per network; do not flood it
	snSignMaxRefreshes   = 8
	snSignRefreshSpacing = 3 * time.Second
	// time the result stays on screen after a successful store
	snSignLingerAfterDone = 20 * time.Second
	// the page can be given another full lifetime a few times; it is still
	// bounded, and local-only the whole while
	snSignMaxExtensions = 3
	snSignDefaultPort   = 8765
)

type snSignUi struct {
	ctx         context.Context
	api         *connect.BringYourApi
	apiUrl      string
	coldkey     string
	journalPath string
	token       string
	deadline    time.Time
	proof       *snWalletProof
	// preview marks example data (never set by the command)
	preview bool

	mutex       sync.Mutex
	want        snNetworkConsentExpect
	message     string
	statement   *protocol.NetworkWalletMappingStatement
	refreshes   int
	extensions  int
	lastRefresh time.Time
	stored      bool
	// stored text when the retained consent was acknowledged on select
	alreadyStored string

	done chan struct{}
}

func snSignToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// bindWithLock binds the run to the account the wallet extension offered (or the
// one named on the command line), finishes a retained unacknowledged consent
// for it, and gets the first statement. The address is checked as a Bittensor
// ss58 address before anything is requested.
func (self *snSignUi) bindWithLock(address string) error {
	if self.statement != nil && self.coldkey == address {
		return nil
	}
	want := self.want
	if err := snConsentBind(&want, self.apiUrl, address, self.proof); err != nil {
		return err
	}
	if done, err := snResolveRetainedConsent(self.ctx, self.api, self.apiUrl, address, self.journalPath, &want, self.proof); err != nil {
		return err
	} else if done {
		self.coldkey = address
		self.stored = true
		self.alreadyStored = "A consent you signed earlier was sent again and is now acknowledged."
		return nil
	}
	self.want, self.coldkey = want, address
	return self.newMessageWithLock()
}

// newMessageWithLock asks the server for a fresh statement and verifies it.
func (self *snSignUi) newMessageWithLock() error {
	self.want.Now = time.Now()
	message, statement, err := snRequestNetworkConsent(self.ctx, self.api, self.coldkey, self.want)
	if err != nil {
		return err
	}
	self.message, self.statement = message, statement
	return nil
}

type snSignState struct {
	Message   string `json:"message"`
	Hex       string `json:"hex"`
	ExpiresAt int64  `json:"expiresAt"`
	From      uint64 `json:"from"`
	Through   uint64 `json:"through"`
	// the epoch the network is in now, so the page can say when the consent starts
	CurrentEpoch uint64 `json:"currentEpoch"`
}

func (self *snSignUi) stateWithLock() snSignState {
	if self.statement == nil {
		return snSignState{}
	}
	return snSignState{
		Message:      self.message,
		Hex:          "0x" + hex.EncodeToString([]byte(self.message)),
		ExpiresAt:    self.statement.ExpiresAt,
		From:         self.statement.FromEpoch,
		Through:      self.statement.ThroughEpoch,
		CurrentEpoch: self.want.Epoch.Epoch,
	}
}

func snSignWriteJson(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func snSignError(w http.ResponseWriter, status int, message string) {
	snSignWriteJson(w, status, map[string]string{"error": snPlain(message)})
}

// local reports whether the request names this machine: a page that a remote
// site loaded through DNS rebinding carries another Host or Origin.
func snSignLocalRequest(r *http.Request) bool {
	hostOk := func(hostport string) bool {
		host, _, err := net.SplitHostPort(hostport)
		if err != nil {
			host = hostport
		}
		return host == "127.0.0.1" || host == "localhost"
	}
	if !hostOk(r.Host) {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
		if !hostOk(trimmed) {
			return false
		}
	}
	return true
}

func (self *snSignUi) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodGet || !snSignLocalRequest(r) {
			http.NotFound(w, r)
			return
		}
		self.mutex.Lock()
		state := self.stateWithLock()
		deadline, extensions := self.deadline, self.extensions
		self.mutex.Unlock()
		boot := map[string]any{
			"preview":        self.preview,
			"address":        self.coldkey,
			"token":          self.token,
			"message":        state.Message,
			"hex":            state.Hex,
			"expiresAt":      state.ExpiresAt,
			"from":           state.From,
			"through":        state.Through,
			"pageDeadline":   deadline.Unix(),
			"extensionsLeft": snSignMaxExtensions - extensions,
		}
		bootJson, _ := json.Marshal(boot)
		logo := snSignLogoSvg
		if i := strings.Index(logo, "<svg"); i > 0 {
			logo = logo[i:]
		}
		page := strings.Replace(snSignPageHtml, "{{LOGO}}", logo, 1)
		// "</" cannot end the script element
		page = strings.Replace(page, "{{BOOT}}", strings.ReplaceAll(string(bootJson), "</", "<\\/"), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:")
		_, _ = io.WriteString(w, page)
	})
	post := func(handle func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !snSignLocalRequest(r) || r.Header.Get("X-Session") != self.token {
				snSignError(w, http.StatusForbidden, "forbidden")
				return
			}
			self.mutex.Lock()
			stopped := time.Now().After(self.deadline)
			self.mutex.Unlock()
			if stopped {
				snSignError(w, http.StatusGone, "this page has stopped; run the command again")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			handle(w, r)
		}
	}
	mux.HandleFunc("/select", post(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			snSignError(w, http.StatusBadRequest, "bad request")
			return
		}
		address := strings.TrimSpace(body.Address)
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if self.stored {
			snSignError(w, http.StatusConflict, "the consent is already stored")
			return
		}
		if self.coldkey != "" && self.coldkey != address {
			snSignError(w, http.StatusConflict, "this run is for "+self.coldkey)
			return
		}
		if err := self.bindWithLock(address); err != nil {
			snSignError(w, http.StatusBadRequest, err.Error())
			return
		}
		if self.alreadyStored != "" {
			snSignWriteJson(w, http.StatusOK, map[string]any{"address": self.coldkey, "already": self.alreadyStored})
			return
		}
		state := self.stateWithLock()
		snSignWriteJson(w, http.StatusOK, map[string]any{"address": self.coldkey, "state": state})
	}))
	mux.HandleFunc("/extend", post(func(w http.ResponseWriter, r *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if self.stored {
			snSignError(w, http.StatusConflict, "the consent is already stored")
			return
		}
		if self.extensions >= snSignMaxExtensions {
			snSignError(w, http.StatusTooManyRequests, "this page cannot be extended again; run the command again")
			return
		}
		self.extensions += 1
		self.deadline = time.Now().Add(snSignPageLifetime)
		snSignWriteJson(w, http.StatusOK, map[string]any{"pageDeadline": self.deadline.Unix(), "extensionsLeft": snSignMaxExtensions - self.extensions})
	}))
	mux.HandleFunc("/refresh", post(func(w http.ResponseWriter, r *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if self.statement == nil {
			snSignError(w, http.StatusConflict, "choose your account first")
			return
		}
		if self.stored {
			snSignError(w, http.StatusConflict, "the consent is already stored")
			return
		}
		if self.refreshes >= snSignMaxRefreshes || time.Since(self.lastRefresh) < snSignRefreshSpacing {
			snSignError(w, http.StatusTooManyRequests, fmt.Sprintf("wait %d seconds between new messages (at most %d per run); the message you have stays valid until its timer ends", int(snSignRefreshSpacing/time.Second), snSignMaxRefreshes))
			return
		}
		self.refreshes += 1
		self.lastRefresh = time.Now()
		if err := self.newMessageWithLock(); err != nil {
			snSignError(w, http.StatusBadGateway, err.Error())
			return
		}
		// a new message is a fresh start: the page lives another full lifetime
		// (bounded by the refresh limit above)
		self.deadline = time.Now().Add(snSignPageLifetime)
		state := self.stateWithLock()
		snSignWriteJson(w, http.StatusOK, map[string]any{
			"message": state.Message, "hex": state.Hex, "expiresAt": state.ExpiresAt,
			"from": state.From, "through": state.Through, "currentEpoch": state.CurrentEpoch,
			"pageDeadline": self.deadline.Unix(),
		})
	}))
	mux.HandleFunc("/signature", post(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Signature string `json:"signature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			snSignError(w, http.StatusBadRequest, "bad request")
			return
		}
		signature, err := snNormalizeSignatureHex(body.Signature)
		if err != nil {
			snSignError(w, http.StatusBadRequest, err.Error())
			return
		}
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if self.statement == nil {
			snSignError(w, http.StatusConflict, "choose your account first")
			return
		}
		if self.stored {
			snSignError(w, http.StatusConflict, "the consent is already stored")
			return
		}
		if time.Now().Unix() >= self.statement.ExpiresAt {
			snSignError(w, http.StatusGone, "the message expired; click New message")
			return
		}
		// the stored message is the one this server verified, never one the page sent
		result, err := snStoreNetworkConsent(self.ctx, self.api, self.apiUrl, self.coldkey, self.journalPath, self.message, signature, self.statement)
		if err != nil {
			snSignError(w, http.StatusBadGateway, err.Error())
			return
		}
		self.stored = true
		fmt.Printf("subnet wallet consent for %s stored: epochs %d through %d, generation %d, receipt %s\n",
			self.coldkey, self.statement.FromEpoch, self.statement.ThroughEpoch, result.MappingGeneration, snPlain(result.MappingHash))
		fmt.Print(snConsentNextSteps(self.statement, self.want.Epoch.Epoch))
		snSignWriteJson(w, http.StatusOK, map[string]any{
			"ok": true, "receipt": snPlain(result.MappingHash), "from": self.statement.FromEpoch,
			"through": self.statement.ThroughEpoch, "generation": result.MappingGeneration,
		})
		go func() {
			time.Sleep(snSignLingerAfterDone)
			close(self.done)
		}()
	}))
	return mux
}

// snOpenBrowser opens the url in the default browser, best effort. Nothing
// depends on it: the url is always printed.
func snOpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if raw, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(raw)), "microsoft") {
			cmd = exec.Command("cmd.exe", "/c", "start", "", url)
		} else {
			cmd = exec.Command("xdg-open", url)
		}
	}
	_ = cmd.Start()
}

// snWalletSignUi is `provider wallet sign`.
func snWalletSignUi(
	ctx context.Context,
	out io.Writer,
	clientStrategy *connect.ClientStrategy,
	apiUrl string,
	coldkeySs58 string,
	proof *snWalletProof,
	port int,
	openBrowser bool,
) error {
	if err := snCheckSigningApiUrl(apiUrl); err != nil {
		return err
	}
	api, want, err := snConsentBase(ctx, clientStrategy, apiUrl)
	if err != nil {
		return err
	}
	defer api.Close()
	journalPath, err := snConsentJournalPath()
	if err != nil {
		return err
	}

	token, err := snSignToken()
	if err != nil {
		return err
	}
	ui := &snSignUi{
		ctx: ctx, api: api, apiUrl: apiUrl, journalPath: journalPath, proof: proof,
		token: token, deadline: time.Now().Add(snSignPageLifetime), want: want, done: make(chan struct{}),
	}
	if coldkeySs58 != "" {
		ui.mutex.Lock()
		err = ui.bindWithLock(coldkeySs58)
		stored := ui.stored
		ui.mutex.Unlock()
		if err != nil {
			return err
		}
		if stored {
			return nil
		}
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil && port != 0 {
		// the default port is taken: any free one will do
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return fmt.Errorf("cannot listen on this computer: %w", err)
	}
	url := fmt.Sprintf("http://%s/", listener.Addr().String())
	server := &http.Server{Handler: ui.handler(), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer server.Close()

	if ui.statement != nil {
		fmt.Fprintf(out, "Sign the wallet consent for %s (epochs %d through %d).\n", ui.coldkey, ui.statement.FromEpoch, ui.statement.ThroughEpoch)
	} else {
		fmt.Fprintf(out, "Sign the wallet consent. You choose the account in your wallet extension on the page.\n")
	}
	fmt.Fprintf(out, "Open this address in the browser that has your wallet extension:\n\n    %s\n\n", url)
	fmt.Fprintf(out, "It stops serving in %d minutes (the page can extend it), or when you are done. Ctrl-C stops it now.\n", int(snSignPageLifetime/time.Minute))
	if openBrowser {
		snOpenBrowser(url)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ui.done:
			return nil
		case <-ticker.C:
			ui.mutex.Lock()
			stopped, stored := time.Now().After(ui.deadline), ui.stored
			ui.mutex.Unlock()
			if stopped {
				if !stored {
					return errors.New("the page timed out before a signature was stored; run the command again")
				}
				return nil
			}
		case err := <-serveErr:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
