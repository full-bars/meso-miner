You are reviewing a Go code change in the URNetwork provider (urnetwork-3.23-fix, a fork of github.com/urnetwork/connect). You do NOT have local file access — everything you need is in this prompt. Review the diff appended below.

## What the change does

Two independent fixes:

### Fix 1: proxy credential rotation (LA7 incident)
`urnet-tools proxy paste` pasted 100 proxies with NEW credentials for host:port addresses that already existed with OLD credentials. The tool reported "added 100" but the daemon's reload() diffs the desired set by ADDRESS only (`desiredSet[s.Address] = s`), so:
- the new-cred entries silently collapsed into the old-cred entries in the address-keyed map
- running proxies kept the OLD credentials forever (no restart, no audit, no log)
- the operator saw "added 100" on the terminal but zero confirmation and zero new client IDs

Fix: `proxyAdd` (provider/main.go) now purges any existing Servers-map key with the same host:port but different creds before adding (rotation semantics); the reloader (provider/proxy_reload.go) gained a `runningAuth map[string]*connect.ProxySettings` tracking what each running proxy was launched with; reload() detects "running but auth differs from desired" and cancels+relaunches with the new creds in one pass, logging "rotating credentials for <addr>". New `sameAuth()` comparator compares user+password.

### Fix 2: message-pool leak attribution (on by default)
LA7 has a real leak: 4.19M buffers taken and never returned, climbing ~47K/hour. The per-call-site tag accounting that could name the leak was compiled out (`const debugTags = false`). The change:
- `debugTags` is now `var debugTags = true` (on by default, permanent)
- `debugTag()` was rewritten to be ALLOCATION-FREE: `runtime.Callers` into a stack array + maphash of the PCs; the human-readable caller string is resolved only when a NEW tag is first seen. This matters because debugTag() runs on every pool Get/Put on the packet path — the pool exists to protect that path's allocation budget (TestFrameCodecAllocs asserts <=1 alloc/frame).
- New `MessagePoolLeakHint(limit)` returns per-tag [(tag, caller file:line, leaked, returned_pct, allocated_pct)] sorted by leaked desc
- New `MessagePoolTotals(size)` sums taken/returned across shards+tags
- provider/main.go heartbeat: when pool verdict is leak/watch, prints `[health][pool][leak] tag=N caller=file:line leaked=N returned=P%` (or a hint that env attribution is on by default)

### Test determinism work (the tests were flaky once real tags got stamped)
- `debugTag` allocating broke TestFrameCodecAllocs (fixed by allocation-free version)
- leak-contract tests asserted absolute tag-0 counts; with real tags they now use a new `waitForPoolBalance(t, size)` helper (pool_quiesce_test.go) that snapshots, waits for counter stability, and asserts DELTA (taken-base <= returned-base)
- TestMessagePoolShardTagConcurrent now calls ResetMessagePoolStats() first
- provider/contract_metrics_test.go: two callback-lifecycle tests failed deterministically on pristine with `-count=2` because the global contract-metrics registry entry was reused across in-process iterations — added resetContractMetricsForIndex() helper
- provider/proxy_probe_test.go: listenSocks5Once now performs a full greeting exchange before returning (liveness barrier) — kills a rare reaper-blacklist flake in TestFetchAndMergeProxyURLs

## Files changed and their new content

See `full.diff` (609 lines total; 10 files, +326/-50): message_pool.go, provider/main.go, provider/proxy_reload.go, message_pool_test.go, ip_send_backpressure_leak_test.go, ip_udp_write_pipeline_leak_test.go, transfer_test.go, transport_pt_queue_test.go, provider/contract_metrics_test.go, provider/proxy_probe_test.go.

## What to look for

1. **Correctness of the rotation** — is the proxyAdd purge loop right (parseProxyAddress splits host:port:user:pass; the purge iterates Servers map keys and deletes same-address-different-keys)? Any case where it deletes a legitimately distinct entry? What if the old key has the same host:port but the NEW one fails to add (partial update)?
2. **reload() rotation path** — the rotation adds the address to BOTH `added` and `removed` in one pass. Trace: removal pass cancels + deletes from state.Proxies + runningAuth; add pass relaunches with new creds. Race: is [proxy] skip-add-still-draining a problem? Is the cancelMap deletion before the add loop guaranteed?
3. **Allocation-freedom claim** — does the new debugTag() really allocate nothing on the hot path (maphash.Bytes, runtime.Callers, binary.LittleEndian into a stack array)? Any hidden allocation (e.g. map access creating entries, interface conversions, the `fmt.Sprintf` in the cold path only)?
4. **On-by-default cost** — what's the real per-Get/Put overhead now? runtime.Callers is not free (stack walk ~100ns). Is that acceptable on the packet path? Any way to make the common case cheaper?
5. **waitForPoolBalance delta logic** — is `takenDelta > returnedDelta` the right leak signal? Can stragglers from OTHER tests cause false fails? The 2s stability window — could a legitimately slow flow exceed it?
6. **Test determinism** — any remaining cross-test global-state bleed? (orderedMessagePools is sync.OnceValue global; ResetMessagePoolStats zeroes per-tag; tagCallers map is global — does TestMessagePoolLeakHintAttribution's leaked buffer leave the tag-1..16 counts dirty for TestMessagePoolShardTagConcurrent?)
7. **Security/audit** — the heartbeat prints `caller=file:line` — any info leak? The provider runs on untrusted boxes; paths are local.

Give findings with severity (CRITICAL/HIGH/MEDIUM/LOW/NIT), file:line-ish refs (from the diff), why-it-matters, and a concrete suggested fix. Verify claims against the diff — do not invent line numbers that aren't in the diff. If a test that the diff modifies would now fail for a reason OTHER than the intended behavior change, call it out.

Output: a markdown findings list, then a one-paragraph verdict (approve / approve-with-fixes / needs-work).