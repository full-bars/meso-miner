# Code Review Findings: `fix/proxy-rotation-and-pool`

**Worktree:** `/home/klets/ur/.worktmp/wt-la7-fixes`  
**Base:** `origin/main`  
**Branch:** `fix/proxy-rotation-and-pool`  

---

## Executive Summary

The changes introduce proxy credential rotation to resolve the LA7 incident, enable allocation-free message-pool leak attribution by default, and adjust test assertions to delta forms for determinism.

However, several critical and high-severity defects prevent this branch from being merged:
1. **Initial startup proxies never rotate:** `runningAuth` is initialized as empty in [`provide()`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L3916). Proxies launched at daemon boot are never recorded in `runningAuth`, so credential rotation silently skips them on re-paste, leaving the exact LA7 bug alive for boot-loaded proxies.
2. **Build failure:** [`ip_send_backpressure_leak_test.go:149`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L149) re-declares `baseTaken, baseReturned` in `TestTcp4BufferSendNonSynDropDoesNotDoubleReturn`, failing compilation with `no new variables on left side of :=`.
3. **Vacuous leak tests:** In [`transfer_test.go`](file:///home/klets/ur/.worktmp/wt-la7-fixes/transfer_test.go#L777), [`transport_pt_queue_test.go`](file:///home/klets/ur/.worktmp/wt-la7-fixes/transport_pt_queue_test.go#L215), and [`ip_udp_write_pipeline_leak_test.go`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_udp_write_pipeline_leak_test.go#L104), `baseTaken` is captured *after* the exercising flow has already executed. `takenDelta` and `returnedDelta` are always 0; the tests pass even if the code leaks 100% of pooled buffers.
4. **Double-return assertion flaw:** [`TestTcp4BufferSendNonSynDropDoesNotDoubleReturn`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L153) asserts `takenDelta > returnedDelta` (leak check), which will never trigger on double-returns (`returnedDelta > takenDelta`).
5. **`uint64` underflow in leak attribution:** [`MessagePoolLeakHint`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L403) computes `a.taken - a.returned` on `uint64`. If `returned > taken`, it underflows to $\approx 18.4 \times 10^{18}$, rocketing benign tags to the top of the worst-offenders list.
6. **Global lock contention on the packet hot path:** [`debugTag()`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L529) takes `debugStateLock.Lock()` (a global mutex) on *every single* buffer acquisition across all cores, defeating the pool's sharded design.

---

## Detailed Findings

### 1. [CRITICAL] Proxies Launched at Boot Are Never Added to `runningAuth`
- **File & Line:** [`provider/main.go:3916`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L3916), [`provider/proxy_reload.go:234-245, 496`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L234-L245)
- **Why it matters:**
  In [`provide()`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L3859-L3865), startup proxies are launched and populated into `proxyCancelMap`. However, [`reloader`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L3913) is constructed with `runningAuth: make(map[string]*connect.ProxySettings)` with no startup entries.
  When an operator updates or re-pastes credentials for any boot-loaded proxy, [`reload()`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L496) calls `r.runningAuthFor(addr)`. Because `addr` is missing from `runningAuth`, it returns `ok = false`. The rotation branch is skipped, hitting `continue`.
  The running proxy continues dialing with stale credentials indefinitely. This preserves the exact LA7 bug for any proxy started at boot.
- **Concrete suggested fix:**
  Populate `runningAuth` at startup in [`provider/main.go`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L3916):
  ```go
  runningAuth := make(map[string]*connect.ProxySettings, len(proxySchedules))
  for _, sched := range proxySchedules {
      if sched.Settings != nil {
          runningAuth[sched.Settings.Address] = sched.Settings
      }
  }
  reloader := &ProxyReloader{
      cancelMap:   proxyCancelMap,
      cancelMapMu: &proxyCancelMu,
      runningAuth: runningAuth,
      // ...
  }
  ```

---

### 2. [CRITICAL] Compilation Failure in `ip_send_backpressure_leak_test.go`
- **File & Line:** [`ip_send_backpressure_leak_test.go:149`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L149)
- **Why it matters:**
  In [`TestTcp4BufferSendNonSynDropDoesNotDoubleReturn`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L108-L156), line 114 declares `baseTaken, baseReturned := waitForPoolBalance(t, 2048)`.
  Line 149 repeats `baseTaken, baseReturned := waitForPoolBalance(t, 2048)`.
  Go compiler fails:
  ```
  ./ip_send_backpressure_leak_test.go:149:26: no new variables on left side of :=
  FAIL github.com/urnetwork/connect [build failed]
  ```
- **Concrete suggested fix:**
  Remove the duplicate `baseTaken, baseReturned :=` declaration at line 149 and reuse the variables:
  ```go
  taken, returned := waitForPoolBalance(t, 2048)
  takenDelta := taken - baseTaken
  returnedDelta := returned - baseReturned
  ```

---

### 3. [HIGH] Vacuous Leak Assertions: Baseline Captured After Flow Execution
- **File & Line:**
  - [`transfer_test.go:777-780`](file:///home/klets/ur/.worktmp/wt-la7-fixes/transfer_test.go#L777-L780)
  - [`transport_pt_queue_test.go:215-217`](file:///home/klets/ur/.worktmp/wt-la7-fixes/transport_pt_queue_test.go#L215-L217)
  - [`transport_pt_queue_test.go:248-251`](file:///home/klets/ur/.worktmp/wt-la7-fixes/transport_pt_queue_test.go#L248-L251)
  - [`ip_udp_write_pipeline_leak_test.go:104-107`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_udp_write_pipeline_leak_test.go#L104-L107)
- **Why it matters:**
  In all four tests, `baseTaken, baseReturned := waitForPoolBalance(t, 2048)` is called *after* the operations under test have completed, followed immediately by `taken, returned := waitForPoolBalance(t, 2048)`.
  Between `baseTaken` and `taken`, zero buffers are allocated or returned. Thus `takenDelta == 0` and `returnedDelta == 0`.
  The assertions `takenDelta > returnedDelta` and `taken - baseTaken == returned - baseReturned` are completely vacuous. If the underlying code leaks every single buffer, the tests will still pass.
- **Concrete suggested fix:**
  Move the baseline snapshot before the exercising code in each test:
  ```go
  // Capture baseline BEFORE the flow:
  baseTaken, baseReturned := waitForPoolBalance(t, 2048)

  for i := 0; i < attempts; i++ {
      // ... test logic ...
  }

  taken, returned := waitForPoolBalance(t, 2048)
  takenDelta := taken - baseTaken
  returnedDelta := returned - baseReturned
  if takenDelta > returnedDelta {
      t.Fatalf(...)
  }
  ```

---

### 4. [HIGH] Double-Return Test Does Not Detect Double Returns
- **File & Line:** [`ip_send_backpressure_leak_test.go:153`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L153)
- **Why it matters:**
  [`TestTcp4BufferSendNonSynDropDoesNotDoubleReturn`](file:///home/klets/ur/.worktmp/wt-la7-fixes/ip_send_backpressure_leak_test.go#L108) tests that a dropped non-SYN packet is not double-returned.
  A double-return means `returnedDelta > takenDelta`.
  The assertion checks `if takenDelta > returnedDelta`.
  If a double-return occurs, `takenDelta > returnedDelta` is false, and the test silently passes.
- **Concrete suggested fix:**
  Check for imbalance in both directions, identical to line 87:
  ```go
  if takenDelta != returnedDelta {
      t.Fatalf("non-SYN drop corrupted pool balance: taken_delta=%d returned_delta=%d", takenDelta, returnedDelta)
  }
  ```

---

### 5. [HIGH] `uint64` Underflow in `MessagePoolLeakHint` Corrupts Leak Rankings
- **File & Line:** [`message_pool.go:403, 408`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L403)
- **Why it matters:**
  `Leaked` is computed as `a.taken - a.returned` on `uint64`.
  If straggler buffers from previous tests or pre-reset routines are returned late, `a.returned > a.taken` for that tag.
  `a.taken - a.returned` underflows `uint64` to $\approx 18.4 \times 10^{18}$ (`^uint64(0)`).
  Because `sort.Slice` sorts by `out[i].Leaked > out[j].Leaked`, an underflowed tag becomes the #1 reported leak, flooding logs with bogus numbers and masking real leaks.
- **Concrete suggested fix:**
  ```go
  var leaked uint64
  if a.taken > a.returned {
      leaked = a.taken - a.returned
  }
  out = append(out, MessagePoolLeakTag{
      Tag:          tag,
      Caller:       callers,
      Leaked:       leaked,
      ReturnedPct:  ratio,
      AllocatedPct: 100 * float64(a.taken-a.created) / float64(a.taken),
  })
  ```

---

### 6. [HIGH] Global Mutex Contention in `debugTag()` on the Packet Hot Path
- **File & Line:** [`message_pool.go:529-546`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L529-L546)
- **Why it matters:**
  `debugTag()` is called on every `MessagePoolGetDetailed` on the packet path with `debugTags = true`.
  It unconditionally acquires `debugStateLock.Lock()` (a global package-level `sync.Mutex`) even when `tag` has already been resolved (`ok == true`).
  The message pool was explicitly sharded (`pool.shards`) to eliminate lock contention across CPU cores. Adding an un-sharded global mutex on every packet allocation reintroduces cross-core serialization.
- **Concrete suggested fix:**
  Cache seen tags in a lock-free table (`tag` is `uint8`, only 256 possible values):
  ```go
  var tagSeen [256]atomic.Bool

  func debugTag() uint8 {
      // ... compute tag ...
      if tagSeen[tag].Load() {
          return tag
      }

      debugStateLock.Lock()
      if !tagSeen[tag].Load() {
          callers := map[string]bool{}
          if n >= 1 {
              if f := runtime.FuncForPC(pcs[0]); f != nil {
                  file, line := f.FileLine(pcs[0])
                  callers[fmt.Sprintf("%s:%d", file, line)] = true
              }
          }
          tagCallers[tag] = callers
          tagSeen[tag].Store(true)
      }
      debugStateLock.Unlock()
      return tag
  }
  ```

---

### 7. [MEDIUM] Off-by-One Caller Attribution in `debugTag()`
- **File & Line:** [`message_pool.go:509-539`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L509-L539)
- **Why it matters:**
  `debugTag()` calls `runtime.Callers(3, pcs[:])` and resolves `pcs[0]`.
  When buffers are fetched via `MessagePoolGet(n)`:
  - Frame 0: `runtime.Callers`
  - Frame 1: `debugTag`
  - Frame 2: `MessagePoolGetDetailed`
  - Frame 3: `MessagePoolGet`
  `pcs[0]` is `MessagePoolGet` itself. If `MessagePoolGet` is not inlined, `f.FileLine(pcs[0])` resolves to `message_pool.go:693`.
  Every external caller using `MessagePoolGet` is attributed to `message_pool.go` instead of the call site that requested the buffer.
- **Concrete suggested fix:**
  Iterate `pcs` and pick the first frame whose function is outside `connect.MessagePool*`:
  ```go
  for _, pc := range pcs[:n] {
      f := runtime.FuncForPC(pc)
      if f == nil {
          continue
      }
      name := f.Name()
      if !strings.Contains(name, "MessagePool") && !strings.Contains(name, "debugTag") {
          file, line := f.FileLine(pc)
          callers[fmt.Sprintf("%s:%d", file, line)] = true
          break
      }
  }
  ```

---

### 8. [MEDIUM] `state.Proxies` Deletion and Stable ID Churn on Credential Rotation
- **File & Line:** [`provider/proxy_reload.go:630`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L630)
- **Why it matters:**
  When credentials rotate, `addr` is added to `removed`.
  Line 630 deletes `r.state.Proxies[addr]`.
  When the addition loop runs immediately after in the same `reload()` pass, [`resolveProxyID`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_state.go#L139) finds no existing entry and calls `nextProxyID()`.
  This increments the proxy ID counter and wipes out all historical state stored in `ProxyEntry` (`Score`, `Graded`, `Health`, `AuthFailures`, and `Source`).
- **Concrete suggested fix:**
  Only delete from `r.state.Proxies` if the address is not in `desiredSet` (or if it is not in `rotated`):
  ```go
  if _, stillDesired := desiredSet[addr]; !stillDesired {
      delete(r.state.Proxies, addr)
  }
  ```
  *(Note: when preserving the ID, ensure `UnregisterProxy` uses a generation token so the exiting goroutine does not cancel the replacement's registration).*

---

### 9. [MEDIUM] Contradictory In-Code Comments in `proxy_reload.go`
- **File & Line:** [`provider/proxy_reload.go:497-501`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L497-L501) vs [`provider/proxy_reload.go:521-525`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_reload.go#L521-L525)
- **Why it matters:**
  Line 497 states: `"Add it to BOTH sets: the removal pass cancels the running goroutine... and the add pass relaunches it"`.
  Line 521 states: `"those addresses were deliberately kept OUT of added above, so they must be cancelled in the removal pass and will be re-added on the next reload pass"`.
  The comments directly contradict each other and confuse maintainers regarding single-pass vs multi-pass rotation.
- **Concrete suggested fix:**
  Update the comment at line 521 to match the actual single-pass implementation.

---

### 10. [LOW / NIT] Misleading `URNETWORK_POOL_DEBUG_TAGS=1` Log & Dead Branch
- **File & Line:** [`provider/main.go:2039-2041`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L2039-L2041), [`message_pool.go:348`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L348)
- **Why it matters:**
  The heartbeat logs: `per-call-site attribution off — run with URNETWORK_POOL_DEBUG_TAGS=1 to identify the leaking caller`.
  However, `debugTags` is hardcoded to `var debugTags = true`, and `URNETWORK_POOL_DEBUG_TAGS` is never read from the environment anywhere in the codebase.
  The log instruction is misleading and useless to operators.
- **Concrete suggested fix:**
  Either bind `debugTags` to `os.Getenv("URNETWORK_POOL_DEBUG_TAGS")` in an `init()` function, or adjust the log to state that attribution is active.

---

### 11. [LOW] Full Internal File Paths Exposed in Heartbeat Logs
- **File & Line:** [`provider/main.go:2035`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/main.go#L2035), [`message_pool.go:539`](file:///home/klets/ur/.worktmp/wt-la7-fixes/message_pool.go#L539)
- **Why it matters:**
  `h.Caller` logs the full absolute path from `FileLine`. On production or customer-facing nodes, this leaks developer usernames, repository structures, and host build paths (e.g. `/home/runner/work/...`).
- **Concrete suggested fix:**
  Store and log relative paths or package-relative paths (e.g. `filepath.Base(file)`).

---

## Test Coverage Gap Analysis & Suggested High-Quality Tests

1. **Rotation of Boot-Loaded Proxies (Regression test for Finding 1):**
   - *Current state:* [`TestProxyAddRotatesCredentials`](file:///home/klets/ur/.worktmp/wt-la7-fixes/provider/proxy_rotation_test.go#L67) only tests `proxyAdd`'s config file modification. No test verifies `reload()` when proxies are loaded at boot time.
   - *Suggested test:* `TestProxyReloadRotatesStartupProxies(t *testing.T)`: Construct `ProxyReloader` simulating startup (proxies in `cancelMap` with populated `runningAuth`), trigger `reload()` with modified credentials, and assert the old context is cancelled and the new proxy is spawned with new auth.

2. **Non-Vacuous Leak Regression Tests (Regression test for Finding 3):**
   - *Current state:* Delta calculations in `transfer_test.go`, `transport_pt_queue_test.go`, and `ip_udp_write_pipeline_leak_test.go` are no-ops because `baseTaken` is measured after the operations.
   - *Suggested test:* Ensure `baseTaken` is captured before the test flow. Add a deliberately leaking test to verify that `takenDelta > returnedDelta` correctly fails when a buffer is leaked.

3. **Double-Return Detection Test (Regression test for Finding 4):**
   - *Current state:* `TestTcp4BufferSendNonSynDropDoesNotDoubleReturn` passes even on double returns.
   - *Suggested test:* Ensure assertions fail when `returnedDelta != takenDelta`.

4. **Concurrent `debugTag` Benchmark / Contention Test:**
   - *Suggested test:* `BenchmarkMessagePoolGetConcurrent(b *testing.B)`: Run parallel goroutines acquiring and returning buffers under `debugTags = true` to measure lock contention on `debugStateLock` and verify the lock-free caching fix.

---

## Final Verdict

**needs-work**

While the core concepts (credential rotation on re-paste, lock-free PC hashing, and pool quiescence waiting) are well-motivated:
1. `ip_send_backpressure_leak_test.go` currently fails compilation.
2. The credential rotation fails to rotate proxies launched at initial boot, leaving the primary LA7 issue open.
3. Four leak contract tests were rendered completely vacuous.
4. `debugTag` introduces an un-sharded global mutex on the hot packet path.
5. `MessagePoolLeakHint` suffers from integer underflow.

The branch requires addressing findings 1 through 6 before it is ready for merge.
