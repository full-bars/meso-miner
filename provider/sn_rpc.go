package main

// sn_rpc.go — the minimal read-only JSON-RPC eth_call client behind
// `provider claim` and `provider bind-head` (sn/PLAN.md 7.3, decision D-6).
// The ABI encoding of the calls this sends is built with sn/stabi; only the
// http transport lives here. Reads (noCommit payout root, headBindDigest) go
// through this stdlib client; signing and submission go through
// sn/miner/onchain (go-ethereum). This is deliberately the read path only —
// it is not the duplication target the stabi/merkle/ss58 packages replaced.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ethRpcTimeout bounds each individual json-rpc request.
const ethRpcTimeout = 15 * time.Second

// ethRpcHexResult performs one json-rpc 2.0 request against an EVM endpoint
// over http and returns the string-typed result. Both methods used here
// (eth_chainId, eth_call) return 0x-hex strings.
// rpcHTTPClient refuses redirects: a configured endpoint is read authority,
// and a redirect must not substitute another node even when it returns a
// plausible chain id and result (mirrors the reference's CheckRedirect).
var rpcHTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func ethRpcHexResult(ctx context.Context, rpcUrl string, method string, params []any) (string, error) {
	requestBodyBytes, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return "", err
	}
	requestCtx, requestCancel := context.WithTimeout(ctx, ethRpcTimeout)
	defer requestCancel()
	request, err := http.NewRequestWithContext(requestCtx, "POST", rpcUrl, bytes.NewReader(requestBodyBytes))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := rpcHTTPClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("%s: http %d", method, response.StatusCode)
	}
	responseBodyBytes, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var rpcResponse struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(responseBodyBytes, &rpcResponse); err != nil {
		return "", fmt.Errorf("%s: bad json-rpc response: %w", method, err)
	}
	if rpcResponse.Error != nil {
		return "", fmt.Errorf("%s: rpc error %d: %s", method, rpcResponse.Error.Code, rpcResponse.Error.Message)
	}
	var hexResult string
	if err := json.Unmarshal(rpcResponse.Result, &hexResult); err != nil {
		return "", fmt.Errorf("%s: non-string result", method)
	}
	return hexResult, nil
}

// parseEthHexQuantity parses a 0x-prefixed json-rpc quantity such as the
// eth_chainId result. The 0x prefix is required: an unprefixed value is not
// a valid json-rpc quantity and accepting one would let a sloppy or hostile
// endpoint smuggle data past prefix-aware checks.
func parseEthHexQuantity(hexQuantity string) (uint64, error) {
	if !strings.HasPrefix(hexQuantity, "0x") {
		return 0, fmt.Errorf("hex quantity lacks the 0x prefix: %q", hexQuantity)
	}
	s := hexQuantity[2:]
	if s == "" {
		return 0, fmt.Errorf("empty hex quantity")
	}
	return strconv.ParseUint(s, 16, 64)
}

// parseEthHexBytes parses 0x-prefixed hex data such as the eth_call return
// data. The 0x prefix is required (see parseEthHexQuantity).
func parseEthHexBytes(hexData string) ([]byte, error) {
	if !strings.HasPrefix(hexData, "0x") {
		return nil, fmt.Errorf("hex data lacks the 0x prefix: %q", hexData)
	}
	return hex.DecodeString(hexData[2:])
}
