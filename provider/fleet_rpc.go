package main

// fleet_rpc.go — the read-only JSON-RPC eth_call view path behind
// `provider fleet status/bind/revoke` on the testnet compatibility path (the
// mainnet path uses the EVM client from sn/miner/onchain directly).
//
// Port of the sn miner's sn_rpc.go read chain (the read-only generation with
// bounded retries, response admission and structured retry classification),
// plus sn_rpc_retry.go and sn_read_retry_cause.go in full. The three names
// that already exist in this package — ethRpcHexResult (provider/sn_rpc.go,
// the claim read path) and its parseEthHexQuantity/parseEthHexBytes helpers —
// are reused, not redeclared; the retry entry points below are the
// upstream-named additions.
//
// Family note (#3693 split): ethRpcHexView, retryEthRpcRead,
// retryableEthRpcError and the cause-budget helpers are shared with the
// claim family's sn_rpc port; if both branches carry them, keep one copy at
// merge time.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/urfoundation/sn/protocol"
)

// Bound each individual request and the complete decoded response body.
const (
	ethRpcResponseLimit = 1024 * 1024
)

// Read-only calls recover transient transport failures within one finite
// budget. Both allowed methods return canonical 0x-prefixed hex strings.
func ethRpcHexResultWithClient(ctx context.Context, client *http.Client, rpcUrl string, method string, params []any) (string, error) {
	return ethRpcHexResultWithRetry(ctx, client, rpcUrl, method, params, ethRpcRetryHooks{})
}

// Authenticate the configured chain before asking it for contract state. A
// wrong-chain node never gains a view request or transient failover authority.
func ethRpcHexView(ctx context.Context, rpcUrl string, expectedChainId uint64, params []any) ([]byte, error) {
	chainIdHex, err := ethRpcHexResultWithClient(ctx, http.DefaultClient, rpcUrl, "eth_chainId", []any{})
	if err != nil {
		return nil, err
	}
	chainId, err := parseEthHexQuantity(chainIdHex)
	if err != nil {
		return nil, err
	}
	if chainId != expectedChainId {
		return nil, fmt.Errorf("%s chain id %d differs from expected %d", rpcUrl, chainId, expectedChainId)
	}
	result, err := ethRpcHexResultWithClient(ctx, http.DefaultClient, rpcUrl, "eth_call", params)
	if err != nil {
		return nil, err
	}
	return parseEthHexBytes(result)
}

// Marshal once before retry admission so every attempt retains the same
// method, selector, contract and calldata, even if caller data later changes.
func ethRpcHexResultWithRetry(ctx context.Context, client *http.Client, rpcUrl string, method string, params []any, hooks ethRpcRetryHooks) (string, error) {
	if ctx == nil || client == nil || (method != "eth_chainId" && method != "eth_call") {
		return "", errors.New("EVM read-only RPC owner or method is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	requestBodyBytes, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return "", err
	}
	return retryEthRpcRead(ctx, hooks, func(attemptCtx context.Context) (string, error) {
		return ethRpcHexRequest(attemptCtx, client, rpcUrl, method, requestBodyBytes)
	})
}

// Each attempt owns its complete response and closes it before retrying or
// returning any result. Only physical transport errors receive retry origin.
func ethRpcHexRequest(ctx context.Context, client *http.Client, rpcUrl string, method string, requestBodyBytes []byte) (string, error) {
	requestCtx, requestCancel := context.WithTimeout(ctx, ethRpcTimeout)
	defer requestCancel()
	request, err := http.NewRequestWithContext(requestCtx, "POST", rpcUrl, bytes.NewReader(requestBodyBytes))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	// A configured endpoint is read authority; redirects must not substitute
	// another node even when it returns a plausible chain id and result.
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := requestClient.Do(request)
	if err != nil {
		return "", &ethRpcTransportError{cause: err}
	}
	// Close belongs to the same physical response as Read. Its transient
	// failures can retry, while every joined hard cause still defeats recovery.
	closeResponse := func() error {
		if err := response.Body.Close(); err != nil {
			return &ethRpcTransportError{cause: err}
		}
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return "", errors.Join(&ethRpcStatusError{method: method, status: response.StatusCode}, closeResponse(), requestCtx.Err())
	}
	if response.ContentLength > ethRpcResponseLimit {
		return "", errors.Join(fmt.Errorf("%s: RPC response exceeds %d bytes", method, ethRpcResponseLimit), closeResponse(), requestCtx.Err())
	}
	responseBodyBytes, readErr := io.ReadAll(io.LimitReader(response.Body, ethRpcResponseLimit+1))
	if readErr != nil {
		readErr = &ethRpcTransportError{cause: readErr}
	}
	if len(responseBodyBytes) > ethRpcResponseLimit {
		readErr = errors.Join(readErr, fmt.Errorf("%s: RPC response exceeds %d bytes", method, ethRpcResponseLimit))
	}
	responseErr := errors.Join(readErr, closeResponse(), requestCtx.Err())
	if readErr != nil || responseErr != nil && !retryableEthRpcError(responseErr, false) {
		return "", responseErr
	}
	// A complete contradictory response stays hard even when close also
	// reports a retryable transport failure. No refused result is returned.
	refuse := func(err error) (string, error) { return "", errors.Join(err, responseErr) }
	if err := protocol.ValidateUniqueJsonKeys(responseBodyBytes); err != nil {
		return refuse(fmt.Errorf("%s: malformed JSON-RPC response: %v", method, err))
	}
	var rpcResponse struct {
		Version string          `json:"jsonrpc"`
		Id      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(responseBodyBytes, &rpcResponse); err != nil {
		return refuse(fmt.Errorf("%s: bad json-rpc response: %w", method, err))
	}
	if rpcResponse.Version != "2.0" || !bytes.Equal(rpcResponse.Id, []byte("1")) || (len(rpcResponse.Result) == 0) == (len(rpcResponse.Error) == 0) {
		return refuse(fmt.Errorf("%s: JSON-RPC response identity or result/error envelope differs from the request", method))
	}
	if len(rpcResponse.Error) != 0 {
		var rpcError *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rpcResponse.Error, &rpcError); err != nil || rpcError == nil {
			return refuse(fmt.Errorf("%s: malformed JSON-RPC error", method))
		}
		return refuse(fmt.Errorf("%s: rpc error %d: %s", method, rpcError.Code, rpcError.Message))
	}
	var hexResult string
	if err := json.Unmarshal(rpcResponse.Result, &hexResult); err != nil {
		return refuse(fmt.Errorf("%s: non-string result", method))
	}
	// The package's ethRpcHexResult path owns lenient quantity/data parsing;
	// this generation requires the canonical 0x prefix before handing bytes to
	// it, so a prefixless result stays refused exactly as upstream refuses it.
	if !strings.HasPrefix(hexResult, "0x") {
		err = hexutil.ErrMissingPrefix
	} else if method == "eth_chainId" {
		_, err = parseEthHexQuantity(hexResult)
	} else {
		_, err = parseEthHexBytes(hexResult)
	}
	if err != nil {
		return refuse(fmt.Errorf("%s: malformed hexadecimal result: %w", method, err))
	}
	if err := errors.Join(responseErr, requestCtx.Err()); err != nil {
		return "", err
	}
	return hexResult, nil
}

const (
	ethRpcOperationTimeout = 300 * time.Second
	ethRpcRetryDelay       = 2 * time.Second
)

// Per-operation hooks allow deadline and cancellation tests without clock
// races. Production supplies neither hook; there is no mutable global state.
type ethRpcRetryHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) error
}

// The operation has time to recover a minute-long outage, while each attempt
// retains its shorter request timeout. Cancellation always defeats recovery.
func retryEthRpcRead(ctx context.Context, hooks ethRpcRetryHooks, read func(context.Context) (string, error)) (string, error) {
	withTimeout := hooks.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(ctx, ethRpcOperationTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return "", errors.Join(lastErr, err)
		}
		value, err := read(ctx)
		if ownerErr := ctx.Err(); ownerErr != nil {
			return "", errors.Join(err, ownerErr)
		}
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !retryableEthRpcError(err, false) {
			return "", err
		}
		delay := ethRpcRetryDelay + time.Duration(rand.Int64N(int64(ethRpcRetryDelay/2)))
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
			return "", errors.Join(lastErr, err)
		}
	}
}
