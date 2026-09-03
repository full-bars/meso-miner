# Port Report: Upstream Parity Port to meso-miner main (hub-stripped)

## Executive Summary

This report documents the parity port of all unported upstream work from `urnetwork-3.23-fix` (`8ec9a1f3..origin/main`, commit `8c99874c`) into the `meso-miner` `main` lane on branch `fix/review-findings-parity`.

`meso-miner` is a hub-stripped distribution: it has no `hub/` directory, no `bandwidth_reporter.go`, no `internal/urnettools/hub_cmds.go`, and does not register hub reporters in `provider/main.go`. All other features, core fixes, reliability improvements, SSRF guards, and test suites from upstream `3.23-fix` main have been ported to direct parity.

All 26 non-hub shared and new files are **100% byte-identical** to upstream `3.23-fix` `origin/main`. The only two files with differences are `provider/main.go` and `internal/urnettools/cobra.go`, which contain only documented, intentional hub-stripping divergences.

---

## Upstream Feature Chains Ported

1. **PR #507 chain: urnet-tools usage feature & provider usage history**
   - Added `internal/urnettools/usage.go`, `usage_cmd.go`, `usage_graph.go`, `usage_test.go`.
   - Wired `usage` subcommand and cards/graphs help into `internal/urnettools/cobra.go`.
   - Added persistent `usage_history.jsonl` writer (`writeUsageHistory`) and segment-based lifetime summation in `provider/proxy_health_log.go` and `provider/main.go`.
2. **PR #502 & #520 chain: proxy-paste feature & temp-file signal cleanup**
   - Added `provider/proxy_paste.go` and `provider/proxy_paste_test.go`.
   - Implemented cross-platform SIGINT/SIGTERM temp-file cleanup and stdin/file input parsing.
   - Wired `provider proxy paste [--file=<file>]` into `provider/main.go`.
3. **PR #510 / #514 / #516 / #517: direct IP providing toggle & reload fixes**
   - Added `internal/urnettools/cmd_direct.go` and `provider/direct.go`.
   - Updated `provider/proxy_reload.go` to handle `directProxyKey` hot-toggling, clean up cancel map entries, and ensure correct defer ordering.
   - Updated `provider/main.go` for `provider direct <state>` and `opts.Bool("direct")`.
   - Hardened `sanitizeRootPath()` to check ancestor directory permissions.
4. **PR #521: review findings across transfer, usage, and network subsystems**
   - `transfer.go`: Settle dropped item contracts via `unack()` without crediting `ackedByteCount` in `dropItem()` and `Run()` teardown drain; fire `RetentionEventCallback` when the retention cap is exceeded; log errors on non-retained items missing from `resendQueue` during cumulative ack processing.
   - `message_pool.go`: Refuse shares on refcount overflow with `MessagePoolFlagNoReturn`, handling subsequent returns as no-op to prevent under-decrement; protect `sizeDistMu` with `sync.RWMutex`.
   - `net.go`: Coalesce concurrent DNS lookups using `singleflight.Group`; sweep expired negative-cache entries and bound negative cache with `pruneDNSCacheNegLocked()`.
   - `provider/proxy_health_log.go`: Added non-blocking asynchronous retention event writer goroutine and thread-safe `flushRetentionEvents()`.
   - `provider/proxy_benchmark.go`: Lazily resolve benchmark probe host using chosen network connect endpoint.
   - `provider/network.go`: Parse API probe endpoints with `net/url` to prevent URL path/query bleed into port extraction.
   - Added full regression test suites.
5. **PR #522: SSRF source URL hardening**
   - Added `provider/ssrf_guard.go` and `provider/ssrf_guard_test.go`.
   - Wired SSRF checks into `provider/proxy_url.go` and `provider/proxy_paste.go` to block dial and redirect access to private, loopback, multicast, and link-local IP addresses.

---

## Per-File Status

| File | Status | Upstream Parity | Description |
| :--- | :--- | :--- | :--- |
| `internal/urnettools/cmd_direct.go` | **new-file** | Byte-identical | Direct IP toggle CLI subcommand (`urnet-tools direct <on\|off>`). |
| `internal/urnettools/cobra.go` | **ported-adapted** | Intentional divergence | Added `direct` and `usage` commands and help text; omitted `report` and `hub` subcommands. |
| `internal/urnettools/usage.go` | **new-file** | Byte-identical | Usage snapshot parsing, lifetime segment math, and window aggregation. |
| `internal/urnettools/usage_cmd.go` | **new-file** | Byte-identical | `urnet-tools usage` CLI summary cards rendering and routing. |
| `internal/urnettools/usage_graph.go` | **new-file** | Byte-identical | Time-series ASCII bar charts for day, hour, and month traffic. |
| `internal/urnettools/usage_test.go` | **new-file** | Byte-identical | Unit tests covering restart handling, window boundaries, and sorting. |
| `message_pool.go` | **ported-clean** | Byte-identical | `MessagePoolFlagNoReturn` flag and `sizeDistMu sync.RWMutex`. |
| `message_pool_test.go` | **ported-clean** | Byte-identical | Test coverage for `MessagePoolFlagNoReturn` on refused share. |
| `net.go` | **ported-clean** | Byte-identical | DNS `singleflight` resolver and negative cache pruning. |
| `net_cache_test.go` | **ported-clean** | Byte-identical | Unit tests for `pruneDNSCacheNegLocked` expiry and bounds. |
| `provider/direct.go` | **new-file** | Byte-identical | Runtime direct IP providing toggle and state persistence. |
| `provider/main.go` | **ported-adapted** | Intentional divergence | Wired `direct`, `proxy paste`, `writeUsageHistory`, `sanitizeRootPath`, `flushRetentionEvents`, and `apiProbeHostPort`; stripped hub reporting. |
| `provider/network.go` | **ported-clean** | Byte-identical | `apiProbeHostPort` using `url.Parse` and dynamic API host resolution. |
| `provider/network_test.go` | **ported-clean** | Byte-identical | Unit tests for API probe URL parsing. |
| `provider/proxy_benchmark.go` | **ported-clean** | Byte-identical | Chosen network connect URL fallback for benchmark egress probes. |
| `provider/proxy_health_log.go` | **ported-clean** | Byte-identical | Asynchronous buffered retention event logging and `writeUsageHistory`. |
| `provider/proxy_health_log_retention_test.go` | **new-file** | Byte-identical | Tests verifying race-freedom between retention event flush and appends. |
| `provider/proxy_paste.go` | **new-file** | Byte-identical | Interactive and piped proxy normalization with SSRF guard and signal cleanup. |
| `provider/proxy_paste_test.go` | **new-file** | Byte-identical | Unit tests for proxy paste parsing, formatting, and temp file cleanup. |
| `provider/proxy_reload.go` | **ported-clean** | Byte-identical | Hot-toggle for direct IP transport, cancelMap cleanup, and trim logic. |
| `provider/proxy_url.go` | **ported-clean** | Byte-identical | SSRF transport and redirect guard on proxy-source URL fetches. |
| `provider/proxy_url_test.go` | **ported-clean** | Byte-identical | Test hook enabling loopback for local fixture servers. |
| `provider/ssrf_guard.go` | **new-file** | Byte-identical | IP range validation blocking non-global destinations. |
| `provider/ssrf_guard_test.go` | **new-file** | Byte-identical | Unit tests for SSRF address validation and redirect checks. |
| `transfer.go` | **ported-clean** | Byte-identical | `unack()` contract settlement for dropped items and retain cap denial callback. |
| `transfer_drop_billing_test.go` | **new-file** | Byte-identical | Tests verifying dropped items do not credit `ackedByteCount`. |
| `transfer_receiveack_desync_test.go` | **new-file** | Byte-identical | Tests asserting desync error logging on missing non-retained items. |
| `transfer_retain_cap_test.go` | **new-file** | Byte-identical | Tests verifying retention cap denial event callbacks. |

---

## Hub-Stripping Decisions

1. **`provider/main.go`:**
   - In upstream `urnetwork-3.23-fix`, `provide()` calls `bootstrapHubCA()`, `runBandwidthReporter()`, and `runHeartbeatReporter()`.
   - In `meso-miner` main, these calls are omitted and replaced with `_ = nodeName // hub reporter display name; hub reporting is stripped on meso-miner`.
   - All other logic in `provider/main.go` matches upstream byte-for-byte.
2. **`internal/urnettools/cobra.go`:**
   - Upstream includes `newReportCmd()` (`report [<url>|off]`) and `newHubCmd()` (`hub <init|link|unlink|...>`).
   - In `meso-miner` main, `report` and `hub` subcommands are stripped from `buildRootCmd()` and the help menu.
3. **Excluded Files:**
   - Upstream files `internal/urnettools/hub_cmds.go` and `internal/urnettools/hub_cmds_test.go` are excluded entirely from this lane.
   - No `hub/` directory or `bandwidth_reporter.go` files are present.

---

## Byte-Verification Summary

Verified using `git -C /home/klets/ur/urnetwork-3.23-fix show origin/main:<path> | cmp -s - <path>`:

```
internal/urnettools/cmd_direct.go            BYTE IDENTICAL
internal/urnettools/usage.go                 BYTE IDENTICAL
internal/urnettools/usage_cmd.go             BYTE IDENTICAL
internal/urnettools/usage_graph.go           BYTE IDENTICAL
internal/urnettools/usage_test.go            BYTE IDENTICAL
message_pool.go                              BYTE IDENTICAL
message_pool_test.go                         BYTE IDENTICAL
net.go                                       BYTE IDENTICAL
net_cache_test.go                            BYTE IDENTICAL
provider/direct.go                           BYTE IDENTICAL
provider/network.go                          BYTE IDENTICAL
provider/network_test.go                     BYTE IDENTICAL
provider/proxy_benchmark.go                  BYTE IDENTICAL
provider/proxy_health_log.go                 BYTE IDENTICAL
provider/proxy_health_log_retention_test.go  BYTE IDENTICAL
provider/proxy_paste.go                      BYTE IDENTICAL
provider/proxy_paste_test.go                 BYTE IDENTICAL
provider/proxy_reload.go                     BYTE IDENTICAL
provider/proxy_url.go                        BYTE IDENTICAL
provider/proxy_url_test.go                   BYTE IDENTICAL
provider/ssrf_guard.go                       BYTE IDENTICAL
provider/ssrf_guard_test.go                  BYTE IDENTICAL
transfer.go                                  BYTE IDENTICAL
transfer_drop_billing_test.go                BYTE IDENTICAL
transfer_receiveack_desync_test.go           BYTE IDENTICAL
transfer_retain_cap_test.go                  BYTE IDENTICAL

internal/urnettools/cobra.go                 DIVERGES (hub-only subcommands omitted)
provider/main.go                             DIVERGES (hub reporter goroutines omitted)
```

**Result: 26 of 28 files are byte-identical (100% parity). The remaining 2 files contain only hub-stripping divergences.**

---

## Verification & Test Results

### 1. Formatting
```bash
gofmt -l .
# Output: (empty - 100% formatted)
```

### 2. Compilation
```bash
go build ./...
# Exit: 0

go vet ./...
# Exit: 0

GOOS=windows go build ./...
# Exit: 0

GOOS=darwin go build ./...
# Exit: 0
```

### 3. Test Suites
- **Core Targeted Suite:**
  ```bash
  go test ./ -run 'TestBackstop|TestUnack|TestRetainCap|TestReceiveAck|TestMessagePool|TestPruneDNSCache|TestContract' -count=1
  # ok  github.com/urnetwork/connect  0.359s
  ```
- **Provider Suite:**
  ```bash
  go test ./provider/ -count=1 -timeout 120s
  # ok  github.com/urnetwork/connect/provider  27.391s
  ```
- **urnet-tools Suite:**
  ```bash
  go test ./internal/urnettools/ -count=1 -timeout 120s
  # ok  github.com/urnetwork/connect/internal/urnettools  41.185s
  ```

---

## Commit History

All commits signed with GPG key `26E294357EFD5035E1FBC3D162648FBF49471559` (`full-bars <45684698+full-bars@users.noreply.github.com>`):

1. `19e5fb1` `feat(usage): port urnet-tools usage feature and provider usage history`
2. `fa5568f` `feat(provider): add proxy paste command for interactive and piped proxy list normalization`
3. `f2f69bc` `feat(provider): add runtime direct IP toggle, reload wiring, and CLI commands`
4. `19f543a` `fix: address review findings in transfer contract billing, memory pool, and network resolution`
5. `39d2bdc` `fix(provider): harden proxy-source URL fetches against SSRF`
6. `46b73a0` `test: add regression tests for contract drops, desync logging, retain cap, and negative DNS cache`
7. `(current)` `docs: add port report for upstream parity on meso-miner main`
