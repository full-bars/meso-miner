You are reviewing a Go code change in a Git worktree at /home/klets/ur/.worktmp/wt-la7-fixes (branch fix/proxy-rotation-and-pool, base origin/main) of the URNetwork provider (github.com/urnetwork/connect). You have FULL local bash access — use it: `git -C /home/klets/ur/.worktmp/wt-la7-fixes diff origin/main`, read files, run TARGETED tests (never the full suite — it takes >5 min and your budget is tight).

## HARD ORDER OF OPERATIONS (write first, then verify)
1. IMMEDIATELY write your analysis-in-progress findings to /home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md as you inspect — do not wait until the end.
2. Read `git -C /home/klets/ur/.worktmp/wt-la7-fixes diff origin/main` in full.
3. Run ONLY bounded, targeted checks: `git diff --stat`, `go vet` on the changed packages, and specific `go test -run` filters for the changed code (e.g. TestSameAuth, TestProxyAddRotates, TestFrameCodecAllocs, TestMessagePoolLeakHint, the contract_metrics tests). Do NOT run `go test -short ./...` or the full provider suite.
4. Write the final findings file (overwrite) and END. Budget discipline matters — you have ~300s.

## What the change does
1. Proxy credential rotation (LA7 incident): `proxyAdd` purges same-host:port different-cred Servers keys before adding; the reloader gained a `runningAuth` map + `sameAuth()`; reload() detects "running but auth differs" and cancels+relaunches in one pass ("rotating credentials for <addr>"). Goal: re-paste with new creds actually rotates the running proxy instead of silently keeping old auth. Boot proxies are seeded into runningAuth at startup (provider/main.go ~line 3935-3939).
2. Message-pool leak attribution ON BY DEFAULT: debugTags was const false -> var true, and debugTag() rewritten allocation-free (runtime.Callers stack array + maphash; caller string lazily resolved for new tags only). Read-mostly gate with atomic.Bool avoids global mutex contention on the packet path. New MessagePoolLeakHint / MessagePoolTotals. Heartbeat prints [health][pool][leak] tag=.. caller=file:line on leak/watch verdict. Signed int64 math prevents uint64 underflow on double-returns.
3. Test determinism: new pool_quiesce_test.go waitForPoolBalance() delta helper; 5 leak-contract assertions converted to delta form (equality checks catch leak AND double-return); TestMessagePoolShardTagConcurrent resets first; contract_metrics_test reset helper (fixes -count=2 determinism on pristine); listenSocks5Once now does a liveness greeting exchange.

## Focus (verify against the code, don't trust the brief)
- Rotation add+remove-in-one-pass race conditions: draining-proxy path, cancelMap timing, state.Proxies deletion, runningAuth bookkeeping. Is adding to BOTH `added` and `removed` in one reload pass correct? Could the removal pass's drain-then-skip break the relaunch?
- Boot proxy seeding: does runningAuth capture all startup-launched proxies before reloader starts?
- Allocation-freedom of debugTag(): run `go test -run TestFrameCodecAllocs`; reason about runtime.Callers cost per Get/Put on the packet path; is on-by-default acceptable?
- waitForPoolBalance delta logic: can stragglers from other tests cause false failures? Is `takenDelta != returnedDelta` the right leak signal (catches both leak AND double-return)? The 2s stability window — could a slow legit flow exceed it?
- Cross-test global-state bleed: orderedMessagePools (sync.OnceValue), tagCallers, ResetMessagePoolStats, contract metrics registry.
- On-by-default [health][pool][leak] line: info-leak / log-noise on untrusted fleet boxes.
- Any modified test now failing for a reason OTHER than the intended change.

## Output
Findings file /home/klets/ur/.worktmp/wt-la7-fixes/gemini_findings_v2.md with severity (CRITICAL/HIGH/MEDIUM/LOW/NIT), file:line (from the actual diff/code), why-it-matters, concrete suggested fix. Include test-coverage gap analysis + suggested high-quality tests (required). End with verdict: approve / approve-with-fixes / needs-work.

## CRITICAL: Run go test synchronously in the foreground with a bounded timeout; NEVER start background tasks; never end your turn while a task runs.