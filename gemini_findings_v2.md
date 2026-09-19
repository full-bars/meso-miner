The code review for branch `fix/proxy-rotation-and-pool` against `origin/main` is complete.

All targeted tests (`go vet`, `TestSameAuth`, `TestProxyAddRotates`, `TestFrameCodecAllocs`, `TestMessagePoolLeakHint`, `contract_metrics`, and the 5 pool leak regressions) were run synchronously and passed cleanly, including multi-iteration `-count=3` runs.

The findings file has been written to [/home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md](file:///home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md).

### Summary of Key Findings

1. **[HIGH] Collision Blindness in Fast-Path Gate Drops Colliding Call Sites From Leak Attribution** ([`message_pool.go:549-578`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L549-L578)):
   `debugTagsSeen [256]atomic.Bool` gates by 8-bit tag instead of call-site PC. With ~85 `MessagePoolGet*` sites, tag collisions are mathematically certain (>99.99%). When a second site hashes to an existing tag, `debugTagsSeen[tag].Load()` returns immediately without registering the new caller in `tagCallers[tag]`. If that second site leaks, the report will falsely attribute the leak exclusively to the first site.
   *Fix:* Track registered sites by PC pair via an `atomic.Pointer[map[[2]uintptr]uint8]` updated with Copy-On-Write on cold misses.

2. **[HIGH] Untracked Running Proxies Cause Rotation Dead-Code in `reload()`** ([`provider/proxy_reload.go:501-510`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L501-L510)):
   `reload()` gates rotation on `previous, ok := r.runningAuthFor(addr); ok && !sameAuth(previous, s)`. If a proxy is running in `cancelMap` but missing from `runningAuth` (`ok == false`), it falls through to `continue`. It neither rotates nor updates `runningAuth`, leaving it untracked indefinitely and rendering `sameAuth`'s nil-mismatch handling dead code.
   *Fix:* Check `!ok || !sameAuth(previous, s)`.

3. **[MEDIUM] State Deletion on Rotation Wipes Stable ID and Health/Probe History** ([`provider/proxy_reload.go:635`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L635)):
   Because rotating proxies are passed through `removed`, `delete(r.state.Proxies, addr)` deletes their `ProxyEntry`. The subsequent addition creates a new ID (`nextProxyID()`) and discards stage-1 probe scores and contract metrics history.
   *Fix:* Preserve the `ProxyEntry` on rotation, resetting only `AuthFailures` and connection health.

4. **[MEDIUM] Integration Test Coverage Gap for `ProxyReloader.reload()` Rotation Flow**:
   Unit tests exist for `sameAuth` and `proxyAdd` CLI persistence, but no test exercises `ProxyReloader.reload()` credential rotation, context cancellation, or active draining behavior.

5. **[LOW/NIT] Additional Cleanups**:
   - The 20ms stability window (`stable >= 2`) in `waitForPoolBalance()` can trigger false failures under CI CPU starvation.
   - Global `debugTags` mutation in tests creates a race condition hazard under `go test -race`.
   - Vestigial comments cite nonexistent `URNETWORK_POOL_DEBUG_TAGS` env var.
   - Redundant lock/unlock on `r.cancelMapMu` during removal loop.
   - Missing `resetContractMetricsForIndex` in `TestRegisterContractCallbackWiresAndRetires`.

**Verdict:** `approve-with-fixes`
The code review for branch `fix/proxy-rotation-and-pool` against `origin/main` is complete.

All targeted tests (`go vet`, `TestSameAuth`, `TestProxyAddRotates`, `TestFrameCodecAllocs`, `TestMessagePoolLeakHint`, `contract_metrics`, and the 5 pool leak regressions) were run synchronously and passed cleanly, including multi-iteration `-count=3` runs.

The findings file has been written to [/home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md](file:///home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md).

### Summary of Key Findings

1. **[HIGH] Collision Blindness in Fast-Path Gate Drops Colliding Call Sites From Leak Attribution** ([`message_pool.go:549-578`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L549-L578)):
   `debugTagsSeen [256]atomic.Bool` gates by 8-bit tag instead of call-site PC. With ~85 `MessagePoolGet*` sites, tag collisions are mathematically certain (>99.99%). When a second site hashes to an existing tag, `debugTagsSeen[tag].Load()` returns immediately without registering the new caller in `tagCallers[tag]`. If that second site leaks, the report will falsely attribute the leak exclusively to the first site.
   *Fix:* Track registered sites by PC pair via an `atomic.Pointer[map[[2]uintptr]uint8]` updated with Copy-On-Write on cold misses.

2. **[HIGH] Untracked Running Proxies Cause Rotation Dead-Code in `reload()`** ([`provider/proxy_reload.go:501-510`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L501-L510)):
   `reload()` gates rotation on `previous, ok := r.runningAuthFor(addr); ok && !sameAuth(previous, s)`. If a proxy is running in `cancelMap` but missing from `runningAuth` (`ok == false`), it falls through to `continue`. It neither rotates nor updates `runningAuth`, leaving it untracked indefinitely and rendering `sameAuth`'s nil-mismatch handling dead code.
   *Fix:* Check `!ok || !sameAuth(previous, s)`.

3. **[MEDIUM] State Deletion on Rotation Wipes Stable ID and Health/Probe History** ([`provider/proxy_reload.go:635`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L635)):
   Because rotating proxies are passed through `removed`, `delete(r.state.Proxies, addr)` deletes their `ProxyEntry`. The subsequent addition creates a new ID (`nextProxyID()`) and discards stage-1 probe scores and contract metrics history.
   *Fix:* Preserve the `ProxyEntry` on rotation, resetting only `AuthFailures` and connection health.

4. **[MEDIUM] Integration Test Coverage Gap for `ProxyReloader.reload()` Rotation Flow**:
   Unit tests exist for `sameAuth` and `proxyAdd` CLI persistence, but no test exercises `ProxyReloader.reload()` credential rotation, context cancellation, or active draining behavior.

5. **[LOW/NIT] Additional Cleanups**:
   - The 20ms stability window (`stable >= 2`) in `waitForPoolBalance()` can trigger false failures under CI CPU starvation.
   - Global `debugTags` mutation in tests creates a race condition hazard under `go test -race`.
   - Vestigial comments cite nonexistent `URNETWORK_POOL_DEBUG_TAGS` env var.
   - Redundant lock/unlock on `r.cancelMapMu` during removal loop.
   - Missing `resetContractMetricsForIndex` in `TestRegisterContractCallbackWiresAndRetires`.

**Verdict:** `approve-with-fixes`
proxy_reload.go:721-724`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L721-L724)
- **Why It Matters:**  
  When a proxy rotates, it is added to `removed`. In the removal loop:
  ```go
  delete(r.state.Proxies, addr)
  ```
  When the add loop runs later in the same reload pass:
  ```go
  stableID := resolveProxyID(r.state, settings.Address)
  ```
  Because `r.state.Proxies[addr]` was deleted, `resolveProxyID` generates a new ID via `nextProxyID()`.  
  This churns the proxy's stable ID and deletes its `Score`, `Graded` flag, `LastGraded` timestamp, and contract metrics association (`index`). While resetting `AuthFailures` on credential change is desired, discarding stage-1 probe results causes the proxy to be treated as un-graded, altering trim-cap shed rankings unexpectedly.
- **Concrete Suggested Fix:**  
  Distinguish between complete removal and rotation in the removal loop. If `addr` is undergoing rotation, retain the `ProxyEntry` (preserving `ID`, `Source`, and probe `Score`), and only reset `AuthFailures` and connection health.

---

### [LOW] 20ms Stability Window in `waitForPoolBalance` Risk Under CI CPU Starvation
- **File & Line:** [`pool_quiesce_test.go:25-38`](file:///home/klets/ur/.worktmp/wt-la7-fixes/pool_quiesce_test.go#L25-L38)
- **Why It Matters:**  
  `waitForPoolBalance()` declares stability after `stable >= 2` (two consecutive samples 10ms apart = 20ms). In heavily-loaded, virtualized CI runners, a goroutine can easily stall for >20ms between taking a buffer and returning it. If `waitForPoolBalance()` samples twice during this pause, it exits early. The subsequent assertion `takenDelta != returnedDelta` will then fail intermittently.
- **Concrete Suggested Fix:**  
  Increase the stability requirement to 5 ticks (50ms), or in tests testing synchronous return paths, poll until `takenDelta == returnedDelta` (or timeout):
  ```go
  const requiredStable = 5
  ```

---

### [LOW] Data Race Hazard on `debugTags` During Tests
- **File & Line:** [`message_pool.go:125`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L125), [`message_pool_leak_hint_test.go:58-61`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool_leak_hint_test.go#L58-L61)
- **Why It Matters:**  
  `debugTags` is a plain `var debugTags = true`. `TestMessagePoolLeakHintRequiresDebugTags` mutates it directly:
  ```go
  debugTags = false
  t.Cleanup(func() { debugTags = true })
  ```
  If any background goroutine in the test binary executes `MessagePoolGetDetailed()` concurrently, `go test -race` will flag a data race.
- **Concrete Suggested Fix:**  
  Make `debugTags` an `atomic.Bool`, or guard it with an atomic load on the get path.

---

### [NIT] Vestigial Comments Reference Non-Existent `URNETWORK_POOL_DEBUG_TAGS`
- **File & Line:** [`message_pool.go:349`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L349), [`provider/main.go:2028`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L2028), [`message_pool_leak_hint_test.go:10`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool_leak_hint_test.go#L10)
- **Why It Matters:**  
  Comments state that leak attribution is active "with URNETWORK_POOL_DEBUG_TAGS=1". In reality, the environment variable is never checked; `debugTags` defaults to true unconditionally.
- **Concrete Suggested Fix:**  
  Update comments to reflect that tagging is on by default and permanent.

---

### [NIT] Redundant Mutex Lock/Unlock on `r.cancelMapMu`
- **File & Line:** [`provider/proxy_reload.go:626-640`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L626-L640)
- **Why It Matters:**  
  `r.cancelMapMu` is acquired and released twice within 10 lines during removal (once for `cancelMap`, once for `runningAuth`).
- **Concrete Suggested Fix:**  
  Consolidate into a single critical section:
  ```go
  r.cancelMapMu.Lock()
  cancel, ok := r.cancelMap[addr]
  if ok {
      delete(r.cancelMap, addr)
      delete(r.runningAuth, addr)
  }
  r.cancelMapMu.Unlock()
  ```

---

### [NIT] Omitted Reset in `TestRegisterContractCallbackWiresAndRetires`
- **File & Line:** [`provider/contract_metrics_test.go:279-283`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/contract_metrics_test.go#L279-L283)
- **Why It Matters:**  
  The reset helper `resetContractMetricsForIndex(t, index)` was added to two tests in `contract_metrics_test.go` to ensure determinism under `-count=N`, but omitted from `TestRegisterContractCallbackWiresAndRetires`.
- **Concrete Suggested Fix:**  
  Add `resetContractMetricsForIndex(t, index)` at the start of `TestRegisterContractCallbackWiresAndRetires`.

---

## Test Coverage Gap Analysis

While the PR includes unit tests for `sameAuth` and `proxyAdd`, it lacks integration verification for the actual rotation mechanics inside `ProxyReloader.reload()`.

### Missing Test Cases:
1. **`TestProxyReloader_RotatesCredentials_Idle`:**  
   Verify that when an existing running proxy has its credentials changed in `desiredSet`:
   - The old context is cancelled.
   - The new proxy goroutine is launched with the new `ProxySettings`.
   - `runningAuth[addr]` reflects the new credentials.
2. **`TestProxyReloader_RotatesCredentials_ActiveDrain`:**  
   Verify that when a rotating proxy has active clients (`bw.Clients.Load() > 0`):
   - It is added to `drainingProxies`.
   - Relaunch in the same pass is skipped (`isDraining`).
   - When drain finishes and triggers reload, the new credentials launch.
3. **`TestDebugTag_CollisionAttribution`:**  
   Test that two synthetic functions hashing to the same tag both appear in `MessagePoolLeakHint()`.

---

## Verdict

**Verdict:** `approve-with-fixes`

The core rotation logic and allocation-free stack unwinding solve the incident root causes effectively. Before merging to `main`, the collision blindness in `debugTag()` (Finding 1) and the untracked proxy check in `reload()` (Finding 2) should be resolved, and an end-to-end `reload()` rotation test should be added.
