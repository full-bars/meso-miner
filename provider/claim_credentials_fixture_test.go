// Synthetic local identity fixtures for the daemon tests. The token is minted
// with an unsecured JWT header and never leaves the test process. Ported from
// the sn miner's sn_credentials_test.go (financialTestJwt) and
// sn_rpc_retry_test.go (ethRpcTestDeadline): both helpers are used by daemon
// tests but their defining files live outside this family's scope.
package main

import (
	"context"
	"sync"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const financialTestClientId = "00000000-0000-0000-0000-000000000201"

// This is local identity syntax, never a production credential or key.
func financialTestJwt(t testing.TB, clientId, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"provider"}, "principal": "synthetic-financial-owner", "exp": time.Now().Add(24 * time.Hour).Unix(), "marker": marker}
	if clientId != "" {
		claims["client_id"], claims["device_id"] = clientId, "00000000-0000-0000-0000-000000000501"
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Logical time advances only at chosen attempt and wait boundaries. The real
// request context still observes a finite deadline and terminal cancellation.
type ethRpcTestDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	close    sync.Once
}

// Preserve the operation boundary in every real request context.
func (self *ethRpcTestDeadline) Deadline() (time.Time, bool) { return self.deadline, true }

// Context propagation can observe the explicitly triggered end of the budget.
func (self *ethRpcTestDeadline) Done() <-chan struct{} { return self.done }

// Parent cancellation remains distinguishable from a spent operation budget.
func (self *ethRpcTestDeadline) Err() error {
	if err := self.Context.Err(); err != nil {
		return err
	}
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// Deferred owner cleanup and logical expiry can each close the owner safely.
func (self *ethRpcTestDeadline) finish() { self.close.Do(func() { close(self.done) }) }
