// Claim discovery and preflight reads are retried under one finite budget.
// This is the v33 adaptation of the sn miner's sn_claim_retry.go, the cause
// budget of sn_read_retry_cause.go and the EVM cause classifier of
// sn_rpc_retry.go. The upstream classifier keys off the sdk's typed HTTP
// status error; this provider's HTTP layer reports a non-2xx status as
// "NNN reason: body" (httpErrorFromResponse), so the status is parsed from
// that producer's text instead of a typed field this module does not carry.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"syscall"
	"time"
)

const (
	claimReadOperationTimeout = 300 * time.Second
	claimReadRetryDelay       = 2 * time.Second
	claimReadMaximumAttempts  = 256
)

// Instance-only clock/wait seams preserve real requests and parsing. The
// executable has no setting that bypasses the finite production budget.
type claimReadRetryHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) error
}

// One read retains its original deadline across transient transport/status
// failures. Every attempt finishes before the next begins or the owner returns.
func retryClaimApiRead[T any](ctx context.Context, hooks claimReadRetryHooks, read func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	withTimeout := hooks.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(ctx, claimReadOperationTimeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= claimReadMaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, errors.Join(lastErr, err)
		}
		result, err := read(ctx)
		if ownerErr := ctx.Err(); ownerErr != nil {
			return zero, errors.Join(err, ownerErr)
		}
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryableClaimApiRead(err) || attempt == claimReadMaximumAttempts {
			return zero, err
		}
		delay := claimReadRetryDelay + time.Duration(rand.Int64N(int64(claimReadRetryDelay/2)))
		wait := hooks.wait
		if wait == nil {
			wait = func(ctx context.Context, delay time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					return nil
				}
			}
		}
		if err := wait(ctx, delay); err != nil {
			return zero, errors.Join(lastErr, err)
		}
	}
	return zero, lastErr
}

// v33's HTTP GET owner reports a non-2xx response as "NNN reason: body" with
// the status line it received. Parse that producer's leading status token.
func claimReadHttpStatus(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	text := err.Error()
	if len(text) < 4 || text[3] != ' ' {
		return 0, false
	}
	status, convertErr := strconv.Atoi(text[:3])
	if convertErr != nil {
		return 0, false
	}
	return status, true
}

// All joined causes must permit retry: an auth, parser or integrity failure
// cannot be hidden by a simultaneous timeout or gateway status.
func retryableClaimApiRead(err error) bool {
	budget := &minerReadCauseBudget{remaining: minerReadCauseMaximumNodes}
	return retryableClaimApiReadCause(err, 0, budget)
}

// HTTP status authority does not extend inside a physical transport subtree.
func retryableClaimApiReadCause(err error, depth int, budget *minerReadCauseBudget) bool {
	if status, ok := claimReadHttpStatus(err); ok {
		if !budget.admit(err, depth) {
			return false
		}
		return status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500 && status <= 599
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *url.Error, *net.OpError, *net.DNSError:
		return retryableEthRpcCause(err, false, depth, budget)
	case interface{ Unwrap() []error }:
		if !budget.admit(err, depth) {
			return false
		}
		causes := cause.Unwrap()
		if len(causes) == 0 || len(causes) > budget.remaining {
			return false
		}
		for _, child := range causes {
			if !retryableClaimApiReadCause(child, depth+1, budget) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		if !budget.admit(err, depth) {
			return false
		}
		return retryableClaimApiReadCause(cause.Unwrap(), depth+1, budget)
	default:
		return retryableEthRpcCause(err, false, depth, budget)
	}
}

const (
	minerReadCauseMaximumDepth = 32
	minerReadCauseMaximumNodes = 128
)

// Policies share traversal bounds without sharing status authority.
type minerReadCauseBudget struct {
	remaining int
}

// Reject nil receivers before invoking custom unwrap or network methods.
func (self *minerReadCauseBudget) admit(err error, depth int) bool {
	self.remaining--
	if err == nil || self.remaining < 0 || depth > minerReadCauseMaximumDepth {
		return false
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !value.IsNil()
	default:
		return true
	}
}

// HTTP status carries structured retry eligibility; diagnostic words do not.
type ethRpcStatusError struct {
	method string
	status int
}

// Retain the method and status for callers without inspecting response bodies.
func (self *ethRpcStatusError) Error() string {
	return fmt.Sprintf("%s: http %d", self.method, self.status)
}

// Only the request transport and bounded body reader may assign this origin.
type ethRpcTransportError struct {
	cause error
}

// Preserve the original diagnostic and structured cause.
func (self *ethRpcTransportError) Error() string { return self.cause.Error() }

// Expose timeout and cancellation while retaining transport provenance.
func (self *ethRpcTransportError) Unwrap() error { return self.cause }

// Every joined cause must permit retry. A timeout cannot hide malformed JSON,
// an identity conflict, a contract refusal, or a response-close integrity error.
func retryableEthRpcError(err error, transportOrigin bool) bool {
	budget := &minerReadCauseBudget{remaining: minerReadCauseMaximumNodes}
	return retryableEthRpcCause(err, transportOrigin, 0, budget)
}

// A delegated claim transport subtree consumes the same traversal allowance.
func retryableEthRpcCause(err error, transportOrigin bool, depth int, budget *minerReadCauseBudget) bool {
	if !budget.admit(err, depth) || err == context.Canceled {
		return false
	}
	switch err.(type) {
	case *os.PathError, *os.LinkError:
		return false
	}
	if err == context.DeadlineExceeded || err == net.ErrClosed || err == syscall.ECONNRESET || err == syscall.ECONNREFUSED || err == syscall.EPIPE || err == syscall.ETIMEDOUT || err == syscall.ENETUNREACH || err == syscall.EHOSTUNREACH {
		return true
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return transportOrigin
	}
	switch cause := err.(type) {
	case *ethRpcStatusError:
		return cause.status == http.StatusTooManyRequests || cause.status >= 500 && cause.status <= 599
	case *ethRpcTransportError:
		return retryableEthRpcCause(cause.cause, true, depth+1, budget)
	case *url.Error:
		return retryableEthRpcCause(cause.Err, true, depth+1, budget)
	case *net.OpError:
		return retryableEthRpcCause(cause.Err, true, depth+1, budget)
	case *net.DNSError:
		// Explicit absence stays hard. A wrapped cause retains its own
		// verdict; availability flags decide only a leaf without a child.
		if cause.IsNotFound {
			return false
		}
		if cause.UnwrapErr != nil {
			return retryableEthRpcCause(cause.UnwrapErr, transportOrigin, depth+1, budget)
		}
		return cause.IsTimeout || cause.IsTemporary
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > budget.remaining {
			return false
		}
		for _, cause := range causes {
			if !retryableEthRpcCause(cause, transportOrigin, depth+1, budget) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableEthRpcCause(wrapped.Unwrap(), transportOrigin, depth+1, budget)
	}
	if networkErr, ok := err.(net.Error); ok {
		return networkErr.Timeout() || networkErr.Temporary()
	}
	return false
}
