package connect

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dialDeadlineNoise is how close to the request deadline a dial error must land
// to be treated as the deadline's own doing rather than a cause.
const dialDeadlineNoise = 25 * time.Millisecond

// dialTimeoutError is what a dial returns when no dialer connected before the
// request deadline. It used to be a bare "Timeout." whatever had gone wrong, so
// a refused connection, a dead SOCKS proxy, a rejected upgrade and a platform
// that never answered all read the same in the logs and to every caller.
//
// The message keeps its leading "Timeout." on purpose: the auth rate limiter
// and the auth-failure cause classifier match on it, and a failed dial must
// keep reading as an overload signal to them. The real cause is appended, which
// also lets those classifiers see "proxy unreachable" and friends that the old
// message hid. last is nil when no dialer failed in time, which means the
// platform (or the proxy path) simply did not answer.
type dialTimeoutError struct {
	// failures counts dials that returned an error before the deadline
	failures int
	// rounds counts passes over the dialers: parallelEval keeps retrying all of
	// them for as long as there is time left
	rounds int
	waited time.Duration
	// last is the most recent real dial error, nil if none
	last error
	// platform is the most recent error from a dial that got past the proxy (it
	// reached the platform or the tunnel to it), nil if every failure was the
	// proxy's. Kept apart from last because the proxy path of a LATER round must
	// not erase evidence that an earlier one reached the platform.
	platform error
	// rejected is true if any dial in the call was explicitly refused by the
	// platform (see wsStatusError). It is kept apart from last because a later
	// timeout in the same call must not hide it.
	rejected bool
}

// AuthRejected reports whether the platform explicitly refused this identity.
func (self *dialTimeoutError) AuthRejected() bool { return self.rejected }

func (self *dialTimeoutError) Error() string {
	if self.last == nil {
		return fmt.Sprintf("Timeout. no dialer connected within %s", self.waited.Round(time.Millisecond))
	}
	return fmt.Sprintf(
		"Timeout. %d dial(s) failed over %d round(s) in %s, last: %s",
		self.failures, self.rounds, self.waited.Round(time.Millisecond), self.last,
	)
}

func (self *dialTimeoutError) Unwrap() error { return self.last }

// Timeout and Temporary make this a net.Error, so isTimeoutError counts a failed
// dial under TimeoutFailures rather than AuthFailures. An outage must not look
// like a run of auth failures to the renewal fast path.
func (self *dialTimeoutError) Timeout() bool { return true }

func (self *dialTimeoutError) Temporary() bool { return true }

// evalErrorRecorder collects what the dialers of ONE parallelEval call returned
// while the call was still live. Errors that arrive after the deadline are the
// deadline's own doing (a cancelled dial), not causes, and are not recorded.
type evalErrorRecorder struct {
	mu       sync.Mutex
	failures int
	rounds   int
	last     error
	platform error
	rejected bool
}

func (self *evalErrorRecorder) round() {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.rounds += 1
}

func (self *evalErrorRecorder) note(err error) {
	if err == nil {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	self.failures += 1
	self.last = err
	if !isProxyLevelError(err) {
		self.platform = err
	}
	if explicitRejection(err) {
		self.rejected = true
	}
}

func (self *evalErrorRecorder) timeoutError(waited time.Duration) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	return &dialTimeoutError{
		failures: self.failures,
		rounds:   self.rounds,
		waited:   waited,
		last:     self.last,
		platform: self.platform,
		rejected: self.rejected,
	}
}

// wsStatusError is a failed websocket upgrade that carries the HTTP status the
// platform answered with. gorilla reports every non-101 answer as the same
// "websocket: bad handshake", which hides whether the platform refused the
// token (401, 403) or was merely overloaded (429, 5xx, a load balancer's 499).
type wsStatusError struct {
	status int
	err    error
}

func (self *wsStatusError) Error() string { return fmt.Sprintf("%s (http %d)", self.err, self.status) }

func (self *wsStatusError) Unwrap() error { return self.err }

// AuthRejected: only 401 and 403 are the platform saying no to this identity.
// Overload (429, 5xx, a load balancer's 499) and routing errors say nothing about
// whether the token is good.
func (self *wsStatusError) AuthRejected() bool {
	return self.status == http.StatusUnauthorized || self.status == http.StatusForbidden
}

type authRejector interface {
	AuthRejected() bool
}

// explicitRejection reports whether err, or anything it wraps, is the platform
// explicitly refusing the identity. It is structural on purpose: the text of an
// error ("401 Unauthorized") is not evidence, and a timeout is never one.
func explicitRejection(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if r, ok := e.(authRejector); ok && r.AuthRejected() {
			return true
		}
	}
	return false
}

// isProxyLevelError reports whether err is a failure of the proxy path itself (a
// SOCKS handshake that failed or timed out, a proxy that cannot be reached)
// rather than of the platform behind it.
func isProxyLevelError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "socks connect") || (strings.Contains(msg, "proxy") && strings.Contains(msg, "unreachable"))
}
