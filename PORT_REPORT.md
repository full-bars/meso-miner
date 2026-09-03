# Parity Port Report: urnetwork-3.23-fix to meso-miner (dev/hub)

## Overview

- **Target Lane:** `dev/hub` (hub-enabled lane on meso-miner)
- **Branch:** `fix/review-findings-parity-devhub`
- **Base Commit:** `a635350` (`origin/dev/hub`)
- **Upstream Source:** `urnetwork-3.23-fix` (`origin/main` at `8c99874c`, commit range `8ec9a1f3..8c99874c`)
- **Parity Status:** Direct byte-parity achieved across all 31 touched files (10 new files, 21 modified files). All hub-specific integrations and call sites (`bootstrapHubCA`, `runBandwidthReporter`, `runHeartbeatReporter`) remain active and intact.

---

## Upstream PR Mapping

| PR / Commit Range | Component | Description | Files Affected |
| :--- | :--- | :--- | :--- |
| **PR #522** (`5ce5ff1e`, `b3761894`) | Provider / Security | SSRF hardening on proxy-source URL fetches; loopback/private RFC1918/ULA isolation | `provider/ssrf_guard.go`, `provider/ssrf_guard_test.go`, `provider/proxy_url.go`, `provider/proxy_url_test.go` |
| **PR #514 / #516 / #517** (`5907e66d`, `f1cf0223`, `4540ac3c`) | Provider / Tools | Direct transport runtime toggle, cancelMap compare-and-delete cleanup, docopt `<state>`, toggle read error handling | `provider/direct.go`, `provider/direct_test.go`, `provider/proxy_reload.go`, `internal/urnettools/cmd_direct.go` |
| **PR #502 / #517 / #520** (`f3256c88..e600d366`, `4540ac3c`, `a12ebd2f`, `6b56765b`) | Provider / Tools | Interactive and piped proxy-paste normalizer with temp file cleanup on SIGINT/SIGTERM; root PATH sanitization for unprivileged child exec | `provider/proxy_paste.go`, `provider/proxy_paste_test.go`, `internal/urnettools/proxy.go`, `provider/main.go` |
| **PR #521** (`8f1e3b3b`, `6e6f2bc2`, `c30845df`, `e6911722`, etc.) | Core / Provider / Tools | H1 billing unack fix on dropped frames, M1-M8 usage/dns/path/pool fixes, shutdown retention event flushing, API probe URL parsing | `transfer.go`, `message_pool.go`, `net.go`, `provider/proxy_health_log.go`, `provider/network.go`, `provider/proxy_benchmark.go`, `internal/urnettools/usage.go`, `internal/urnettools/usage_cmd.go`, `internal/urnettools/usage_graph.go`, `internal/urnettools/update.go`, `provider/client_jwt_hotrestart_test.go` |
| **PR #521 Test Suite** (`110bafe7`, `0dd13022`, `da7f4cea`, `140008d3`, `1d5d8c23`) | Tests | Regression tests for unack rounding, retain cap denials, backstop billing, negative DNS cache bounding, refcount overflow handling | `transfer_drop_billing_test.go`, `transfer_receiveack_desync_test.go`, `transfer_retain_cap_test.go`, `message_pool_test.go`, `net_cache_test.go`, `provider/network_test.go`, `provider/proxy_health_log_retention_test.go`, `internal/urnettools/usage_test.go` |

---

## Per-File Parity Status

All 31 files touched during this port are **100% byte-identical** to upstream `origin/main` at `8c99874c`:

| File | Status | Notes |
| :--- | :--- | :--- |
| `internal/urnettools/cmd_direct.go` | Byte-identical | Surfaces toggle file read error on non-NotExist |
| `internal/urnettools/proxy.go` | Byte-identical | Wires `proxy paste` subcommand and stdin inheritance |
| `internal/urnettools/update.go` | Byte-identical | Cleaned trailing spaces before period in comment |
| `internal/urnettools/usage.go` | Byte-identical | History reader covers `.1` rotated files; chronological sort stability |
| `internal/urnettools/usage_cmd.go` | Byte-identical | Warns when billable > total accounting mismatch occurs |
| `internal/urnettools/usage_graph.go` | Byte-identical | Uses `orderChronological` for stable bucket bucketing |
| `internal/urnettools/usage_test.go` | Byte-identical | Tests for `TestBillableExceedsTotal` and out-of-order snapshot feeds |
| `message_pool.go` | Byte-identical | `MessagePoolFlagNoReturn` on refused read-only share overflow |
| `message_pool_test.go` | Byte-identical | Regression test for refused share reference accounting |
| `net.go` | Byte-identical | Negative DNS cache bounded and pruned unconditionally on lookup failure |
| `net_cache_test.go` | Byte-identical | Regression test for negative cache expiry, capping, and empty cache safety |
| `provider/client_jwt_hotrestart_test.go` | Byte-identical | Dead test variable cleanup |
| `provider/direct.go` | Byte-identical (New) | Native direct transport runtime toggle management (`provider direct <state>`) |
| `provider/direct_test.go` | Byte-identical (New) | Direct toggle persistence and precedence test suite |
| `provider/main.go` | Byte-identical | Wires direct command, proxy paste, root PATH filter, shutdown retention flush, and API probe host/port resolution |
| `provider/network.go` | Byte-identical | `apiProbeHostPort` and `resolveAPIProbeHostPort` using `url.Parse` |
| `provider/network_test.go` | Byte-identical | Tests for `apiProbeHostPort` host/port extraction |
| `provider/proxy_benchmark.go` | Byte-identical | Resolves benchmark endpoint following configured network connect URL |
| `provider/proxy_health_log.go` | Byte-identical | Asynchronous buffered retention event writer + thread-safe `flushRetentionEvents` |
| `provider/proxy_health_log_retention_test.go` | Byte-identical (New) | Tests retention event writer lifecycle and flush on shutdown |
| `provider/proxy_paste.go` | Byte-identical (New) | Interactive/piped proxy paste normalizer with format detection and SIGINT/SIGTERM temp file cleanup |
| `provider/proxy_paste_test.go` | Byte-identical (New) | Comprehensive proxy paste format parsing and cleanup tests |
| `provider/proxy_reload.go` | Byte-identical | Direct transport hot-toggle integration, `directDone` channel, trim exclusions |
| `provider/proxy_url.go` | Byte-identical | Hardened HTTP client transport with SSRF protection and redirect destination check |
| `provider/proxy_url_test.go` | Byte-identical | Configures `ssrfAllowLoopback` for test fixtures |
| `provider/ssrf_guard.go` | Byte-identical (New) | Network address validator blocking loopback, link-local, RFC1918, ULA, and multicast |
| `provider/ssrf_guard_test.go` | Byte-identical (New) | SSRF guard isolation test suite |
| `transfer.go` | Byte-identical | H1 billing unack fix on dropped packets, M3 retain cap denial telemetry, dead `ackItemWithErr` removal |
| `transfer_drop_billing_test.go` | Byte-identical (New) | Regression tests for backstop-drop contract billing settlement |
| `transfer_receiveack_desync_test.go` | Byte-identical (New) | Tests ack-receive desync gating |
| `transfer_retain_cap_test.go` | Byte-identical (New) | Tests retention buffer cap denial behavior |

### Pre-existing Intentional Divergences (Untouched)
- `internal/urnettools/cobra_regression_test.go`: Meso lane retains doc comment cleanup from PR #54 removing review-process references.
- `internal/urnettools/lifecycle_candidates_test.go`: Whitespace comment formatting difference preserved from PR #54.

---

## Verification Results

### 1. Code Formatting (`gofmt -l .`)
```bash
$ gofmt -l .
# (empty - all files cleanly formatted)
```

### 2. Standard Build & Vet
```bash
$ go build ./...
# Exit code: 0

$ go vet ./...
# Exit code: 0
```

### 3. Cross-Compilation
```bash
$ GOOS=windows go build ./...
# Exit code: 0

$ GOOS=darwin go build ./...
# Exit code: 0
```

### 4. Regression & Subsystem Test Suites
- **Root Regression Tests:**
  ```bash
  $ go test ./ -run 'TestBackstop|TestUnack|TestRetainCap|TestReceiveAck|TestMessagePool|TestPruneDNSCache|TestContract' -count=1
  ok  	github.com/urnetwork/connect	0.374s
  ```

- **Provider Package Tests:**
  ```bash
  $ go test ./provider/ -count=1 -timeout 120s
  ok  	github.com/urnetwork/connect/provider	26.307s
  ```

- **Urnettools Package Tests:**
  ```bash
  $ go test ./internal/urnettools/ -count=1 -timeout 120s
  ok  	github.com/urnetwork/connect/internal/urnettools	40.844s
  ```

---

## Git Commit History on Branch

All commits are signed with GPG key `26E294357EFD5035E1FBC3D162648FBF49471559`:

1. `de22155` - `fix(security): harden proxy-source URL fetches against SSRF (PR 522)`
2. `0118206` - `feat(direct): port direct transport runtime toggle and management (PRs 514, 516, 517)`
3. `d0af43d` - `feat(provider): port proxy-paste feature and root path sanitization (PRs 502, 517, 520)`
4. `803f6e7` - `fix(provider): port review findings across transfer, usage, and network subsystems (PR 521)`
5. `494755b` - `test: port regression tests for transfer, memory pool, dns cache, and usage (PR 521)`
