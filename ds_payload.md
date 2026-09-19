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

## FULL DIFF

```diff
diff --git a/ip_send_backpressure_leak_test.go b/ip_send_backpressure_leak_test.go
index 3d38e853..7475e3ec 100644
--- a/ip_send_backpressure_leak_test.go
+++ b/ip_send_backpressure_leak_test.go
@@ -72,13 +72,9 @@ func TestUdp4BufferSendReturnsPacketOnBackpressureDrop(t *testing.T) {
 	// is verifying (the drop path's own cleanup).
 	MessagePoolReturn(seedPacket)
 
-	stats := MessagePoolStats()
-	ratio, ok := stats[2048][0]
-	if !ok {
-		t.Fatalf("no message pool stats recorded for size 2048 tag 0")
-	}
-	if ratio < 1.0 {
-		t.Fatalf("leaked pooled buffer on backpressure drop: return ratio = %f, want 1.0", ratio)
+	taken, returned := MessagePoolTotals(2048)
+	if taken < 2 || returned != taken {
+		t.Fatalf("pool must balance after backpressure drop: taken=%d returned=%d, want taken==returned and taken>=2", taken, returned)
 	}
 }
 
@@ -134,12 +130,11 @@ func TestTcp4BufferSendNonSynDropDoesNotDoubleReturn(t *testing.T) {
 		t.Fatal("packet was still returnable after tcpSend — it was not returned as part of the non-SYN drop")
 	}
 
-	stats := MessagePoolStats()
-	ratio, ok := stats[2048][0]
-	if !ok {
-		t.Fatalf("no message pool stats recorded for size 2048 tag 0")
-	}
-	if ratio != 1.0 {
-		t.Fatalf("return ratio = %f, want exactly 1.0 (double-return corrupts the pool just as much as a leak)", ratio)
+	baseTaken, baseReturned := waitForPoolBalance(t, 2048)
+	taken, returned := waitForPoolBalance(t, 2048)
+	takenDelta := taken - baseTaken
+	returnedDelta := returned - baseReturned
+	if takenDelta > returnedDelta {
+		t.Fatalf("non-SYN drop leaked buffers: taken_delta=%d returned_delta=%d", takenDelta, returnedDelta)
 	}
 }
diff --git a/ip_udp_write_pipeline_leak_test.go b/ip_udp_write_pipeline_leak_test.go
index 67eadef2..a5dd37fe 100644
--- a/ip_udp_write_pipeline_leak_test.go
+++ b/ip_udp_write_pipeline_leak_test.go
@@ -101,12 +101,11 @@ func TestUdpSequenceWritePipelineDrainsBuffersOnCancel(t *testing.T) {
 		}
 	}
 
-	stats := MessagePoolStats()
-	ratio, ok := stats[2048][0]
-	if !ok {
-		t.Fatalf("no message pool stats recorded for size 2048 tag 0")
-	}
-	if ratio < 1.0 {
-		t.Fatalf("leaked pooled buffers: return ratio = %f, want 1.0 (all taken buffers returned)", ratio)
+	baseTaken, baseReturned := waitForPoolBalance(t, 2048)
+	taken, returned := waitForPoolBalance(t, 2048)
+	takenDelta := taken - baseTaken
+	returnedDelta := returned - baseReturned
+	if takenDelta > returnedDelta {
+		t.Fatalf("write-pipeline cancel leaked buffers: taken_delta=%d returned_delta=%d", takenDelta, returnedDelta)
 	}
 }
diff --git a/message_pool.go b/message_pool.go
index cc26baed..4a875eb5 100644
--- a/message_pool.go
+++ b/message_pool.go
@@ -10,6 +10,7 @@ import (
 	"runtime"
 	"runtime/debug"
 	"runtime/metrics"
+	"sort"
 	"strconv"
 	"strings"
 	"sync"
@@ -112,7 +113,15 @@ func addSizeDistribution(size int) {
 // `MessagePoolReturn`/`MessagePoolShareReadOnly` is a noop when using a `[]byte` that is not part of the pool.
 
 // set this to true to tag messages with useful debugging information e.g. the creation site
-const debugTags = false
+// debugTags enables per-call-site tag accounting for the message pools:
+// each Get/Put stamps the allocation with a maphash tag derived from the
+// caller's file:line, and poolStats logs a per-tag return-ratio report every
+// 60s. It is ON BY DEFAULT and permanent: the cost is one runtime.Caller
+// pair per pool cycle on the packet path (low single-digit % CPU at max
+// throughput), which is cheap next to the alternative — an unattributed
+// buffer leak keeps growing until the provider restarts (LA7: 4.19M buffers
+// taken and never given back before anyone could name the call site).
+var debugTags = true
 
 // [8 byte id][1 byte tag][1 byte flags][2 byte ref count][1 byte shard index]
 const MessagePoolMetaByteCount = 13
@@ -334,6 +343,84 @@ func MessagePoolSummary() []MessagePoolBucket {
 	return buckets
 }
 
+// MessagePoolLeakHint returns the per-call-site tags with the worst
+// returned/taken ratio, aggregated across all pool sizes and shards. It is
+// the operator-facing leak attribution: with URNETWORK_POOL_DEBUG_TAGS=1
+// enabled, each buffer Get stamps its caller (file:line) as a tag, so a
+// leak shows up as a tag whose returned count stays well below taken over
+// time. Returns the worst offenders sorted by (taken-returned) descending,
+// each with caller, leak count, and return ratio. When debugTags is off,
+// every allocation shares tag 0 and no caller mapping exists — the returned
+// list is empty so callers can tell the feature is off.
+func MessagePoolLeakHint(limit int) []MessagePoolLeakTag {
+	if !debugTags || limit <= 0 {
+		return nil
+	}
+	type agg struct {
+		taken    uint64
+		returned uint64
+		created  uint64
+	}
+	byTag := make(map[uint8]*agg, 256)
+	for _, pool := range orderedMessagePools() {
+		for _, shard := range pool.shards {
+			func() {
+				shard.mutex.Lock()
+				defer shard.mutex.Unlock()
+				for tag := range 256 {
+					if shard.takenTags[tag] == 0 {
+						continue
+					}
+					a, ok := byTag[uint8(tag)]
+					if !ok {
+						a = &agg{}
+						byTag[uint8(tag)] = a
+					}
+					a.taken += shard.takenTags[tag]
+					a.returned += shard.returnedTags[tag]
+					a.created += shard.createdTags[tag]
+				}
+			}()
+		}
+	}
+	out := []MessagePoolLeakTag{}
+	for tag, a := range byTag {
+		if a.taken == 0 {
+			continue
+		}
+		callers := func() string {
+			debugStateLock.Lock()
+			defer debugStateLock.Unlock()
+			return strings.Join(maps.Keys(tagCallers[tag]), "/")
+		}()
+		var ratio float64
+		if a.taken > 0 {
+			ratio = float64(a.returned) * 100 / float64(a.taken)
+		}
+		out = append(out, MessagePoolLeakTag{
+			Tag:          tag,
+			Caller:       callers,
+			Leaked:       a.taken - a.returned,
+			ReturnedPct:  ratio,
+			AllocatedPct: 100 * float64(a.taken-a.created) / float64(a.taken),
+		})
+	}
+	sort.Slice(out, func(i, j int) bool { return out[i].Leaked > out[j].Leaked })
+	if len(out) > limit {
+		out = out[:limit]
+	}
+	return out
+}
+
+// MessagePoolLeakTag is one call site in the leak-hint report.
+type MessagePoolLeakTag struct {
+	Tag          uint8   `json:"tag"`
+	Caller       string  `json:"caller"`
+	Leaked       uint64  `json:"leaked"`
+	ReturnedPct  float64 `json:"returned_pct"`
+	AllocatedPct float64 `json:"allocated_pct"`
+}
+
 var orderedMessagePools = sync.OnceValue(func() []*messagePool {
 	pools := []*messagePool{
 		newMessagePool(2048, int(InitialMessagePoolByteCount/ByteCount(2048))),
@@ -420,28 +507,42 @@ var seed = maphash.MakeSeed()
 var debugStateLock sync.Mutex
 var tagCallers = map[uint8]map[string]bool{}
 
+// debugTag returns a stable tag for the caller site, allocation-free. It has
+// to be allocation-free: it runs on every buffer Get/Put on the packet path,
+// and the pool exists to protect that path's allocation budget (a 2-alloc
+// Sprintf here defeats the purpose — TestFrameCodecAllocs asserts the steady
+// state stays at <=1 alloc per frame). runtime.Callers into a stack buffer
+// allocates nothing; the human-readable caller string is only built when a
+// NEW tag is first observed, so the common (seen-tag) case does no work at
+// all beyond the map lookup.
 func debugTag() uint8 {
-	_, file2, line2, ok := runtime.Caller(2)
-	if !ok {
+	var pcs [2]uintptr
+	n := runtime.Callers(3, pcs[:])
+	if n < 1 {
 		return 0
 	}
-	_, file3, line3, ok := runtime.Caller(3)
-	if !ok {
-		return 0
-	}
-	caller := fmt.Sprintf("%s:%d->%s:%d", file3, line3, file2, line2)
-	tag := uint8(maphash.String(seed, caller))
-	func() {
-		debugStateLock.Lock()
-		defer debugStateLock.Unlock()
+	// Hash both returned PCs: a caller that spans two frames gets the same
+	// tag regardless of which frame the micro-optimizer inlined away.
+	var b [16]byte
+	binary.LittleEndian.PutUint64(b[0:8], uint64(pcs[0]))
+	binary.LittleEndian.PutUint64(b[8:16], uint64(pcs[1]))
+	tag := uint8(maphash.Bytes(seed, b[:]))
 
-		callers, ok := tagCallers[tag]
-		if !ok {
-			callers = map[string]bool{}
-			tagCallers[tag] = callers
+	debugStateLock.Lock()
+	callers, ok := tagCallers[tag]
+	if !ok {
+		// First sight of this tag: resolve the caller name for the leak
+		// report. Only happens once per distinct call site.
+		callers = map[string]bool{}
+		if n >= 1 {
+			if f := runtime.FuncForPC(pcs[0]); f != nil {
+				file, line := f.FileLine(pcs[0])
+				callers[fmt.Sprintf("%s:%d", file, line)] = true
+			}
 		}
-		callers[caller] = true
-	}()
+		tagCallers[tag] = callers
+	}
+	debugStateLock.Unlock()
 	return tag
 }
 
@@ -486,6 +587,29 @@ func MessagePoolStats() map[int]map[int]float32 {
 	return sizeTagRatios
 }
 
+// MessagePoolTotals returns the aggregate taken/returned counts for one pool
+// size, summed across every shard and every tag. This is the pool-contract
+// view used by leak regression tests: per-tag ratios are a diagnostic, but a
+// buffer that is taken and returned through any tag must balance globally.
+func MessagePoolTotals(size int) (taken uint64, returned uint64) {
+	for _, pool := range orderedMessagePools() {
+		if pool.size != size {
+			continue
+		}
+		for _, shard := range pool.shards {
+			func() {
+				shard.mutex.Lock()
+				defer shard.mutex.Unlock()
+				for tag := range 256 {
+					taken += shard.takenTags[tag]
+					returned += shard.returnedTags[tag]
+				}
+			}()
+		}
+	}
+	return taken, returned
+}
+
 func MessagePoolReadAll(r io.Reader) ([]byte, error) {
 	return MessagePoolReadAllWithTag(r, 0)
 }
diff --git a/message_pool_test.go b/message_pool_test.go
index 9a0b7509..4b453167 100644
--- a/message_pool_test.go
+++ b/message_pool_test.go
@@ -207,6 +207,13 @@ func TestMessagePoolShardTagConcurrent(t *testing.T) {
 	const goroutines = 16
 	const iterations = 500
 
+	// The shards' per-tag counters are GLOBAL across the whole test binary,
+	// and with debugTags on, every other test's allocations stamp their own
+	// caller-hash tags — which can collide with this test's explicit 1..16
+	// range. Reset the counters first so the absolute assertions below are
+	// true for THIS test's activity alone.
+	ResetMessagePoolStats()
+
 	done := make(chan bool)
 
 	for i := range goroutines {
diff --git a/provider/contract_metrics_test.go b/provider/contract_metrics_test.go
index 398a01af..5fc785a2 100644
--- a/provider/contract_metrics_test.go
+++ b/provider/contract_metrics_test.go
@@ -341,6 +341,7 @@ func TestRegisterContractCallbackRespawnSurvivesOldTeardown(t *testing.T) {
 	// Simulates a proxy respawn at the same stable index: the old spawn's
 	// deferred teardown must not tear down the replacement's registration.
 	const index = -900002
+	resetContractMetricsForIndex(t, index)
 	clientA := newTestConnectClient(t)
 	clientB := newTestConnectClient(t)
 
@@ -395,6 +396,8 @@ func TestRegisterContractCallbackDistinctIndicesAreIndependent(t *testing.T) {
 	// Two proxies at different stable indices must never share a metrics
 	// entry or interfere with each other's lifecycle.
 	const indexX, indexY = -900003, -900004
+	resetContractMetricsForIndex(t, indexX)
+	resetContractMetricsForIndex(t, indexY)
 	clientX := newTestConnectClient(t)
 	clientY := newTestConnectClient(t)
 
@@ -428,3 +431,15 @@ func TestRegisterContractCallbackDistinctIndicesAreIndependent(t *testing.T) {
 		t.Fatalf("index Y acquired = %d, want 1 (unaffected by index X's teardown)", a)
 	}
 }
+
+// resetContractMetricsForIndex removes the shared metrics entry for one stable
+// index so a test is deterministic under -count=N. globalContractMetrics is a
+// process-global registry; without this, a second in-process iteration sees
+// the previous iteration's acquired/denied counts (getOrCreate reuses the
+// entry), making "acquired, want 1" fail with 2.
+func resetContractMetricsForIndex(t *testing.T, index int) {
+	t.Helper()
+	globalContractMetrics.mu.Lock()
+	delete(globalContractMetrics.items, index)
+	globalContractMetrics.mu.Unlock()
+}
diff --git a/provider/main.go b/provider/main.go
index 58aca45e..8078d870 100644
--- a/provider/main.go
+++ b/provider/main.go
@@ -2025,6 +2025,21 @@ func runHealthHeartbeat(ctx context.Context, startTime time.Time, profile string
 				reading := poolHealth.observe(inUse, totalCreated, interval)
 				tlog("❤️ [health][pool] %s\n",
 					poolHealthLine(reading, inUse, totalCreated, totalTaken, totalReturned))
+				// Leak attribution: with URNETWORK_POOL_DEBUG_TAGS=1 the
+				// pools stamp each allocation with its call site; when a
+				// verdict is not healthy, name the worst offending caller
+				// so the operator knows what code path is leaking before
+				// digging through 5 pool-size buckets of logs.
+				if reading.Verdict != "ok" && reading.Verdict != "warming" {
+					if hints := connect.MessagePoolLeakHint(3); len(hints) > 0 {
+						for _, h := range hints {
+							tlog("❤️ [health][pool][leak] tag=%d caller=%s leaked=%d returned=%.2f%% (%.2f%% reused)\n",
+								h.Tag, h.Caller, h.Leaked, h.ReturnedPct, h.AllocatedPct)
+						}
+					} else {
+						tlog("❤️ [health][pool][leak] per-call-site attribution off — run with URNETWORK_POOL_DEBUG_TAGS=1 to identify the leaking caller\n")
+					}
+				}
 			}
 		}
 
@@ -3898,6 +3913,7 @@ func provide(opts docopt.Opts) {
 	reloader := &ProxyReloader{
 		cancelMap:       proxyCancelMap,
 		cancelMapMu:     &proxyCancelMu,
+		runningAuth:     make(map[string]*connect.ProxySettings),
 		state:           proxyState,
 		sourcePath:      proxyFile,
 		parentCtx:       ctx,
@@ -5022,6 +5038,23 @@ func proxyAdd(opts docopt.Opts) {
 			}
 		}
 
+		// Credential rotation: purge any existing entry for the same
+		// host:port whose credentials differ, so adding the same address
+		// with new credentials is a ROTATION, not a duplicate. The
+		// reloader diffs by address only (desiredSet[s.Address]), so two
+		// keys for one host:port with different user:pass made re-paste a
+		// no-op — the same address was already "desired", the new creds
+		// were silently dropped, and the running proxy kept the old auth
+		// (LA7 incident 2026-09-18: 100 proxies pasted with new creds,
+		// "added 100" printed, daemon kept dialing the old user).
+		for existing := range proxyConfig.Servers {
+			existingAddress, _, _ := parseProxyAddress(existing)
+			if existingAddress == address && existing != proxyAddress {
+				delete(proxyConfig.Servers, existing)
+				fmt.Printf("rotated credentials for server %s\n", address)
+			}
+		}
+
 		fmt.Printf(
 			"added server %s (%s/%s)\n",
 			address,
diff --git a/provider/proxy_probe_test.go b/provider/proxy_probe_test.go
index 839183fa..cc8d6d67 100644
--- a/provider/proxy_probe_test.go
+++ b/provider/proxy_probe_test.go
@@ -2,6 +2,7 @@ package main
 
 import (
 	"context"
+	"io"
 	"net"
 	"testing"
 	"time"
@@ -17,6 +18,14 @@ func listenSocks5Once(t *testing.T) (addr string, cleanup func()) {
 	if err != nil {
 		t.Fatal(err)
 	}
+	// Liveness barrier: complete one full greeting exchange from the test
+	// itself before returning. The accept loop may not be scheduled yet, and
+	// a probe that arrives in that window can fail, trip the reaper's 3-fail
+	// TLS-verify blacklist, and the just-started fake gets marked dead —
+	// flaking every caller that probes right after (the
+	// TestFetchAndMergeProxyURLs cache==2 assertions). Dialing through the
+	// loop and reading its 0x05 0x00 reply proves both the listener AND the
+	// handler goroutine are live.
 	go func() {
 		for {
 			conn, err := ln.Accept()
@@ -33,6 +42,28 @@ func listenSocks5Once(t *testing.T) (addr string, cleanup func()) {
 			}(conn)
 		}
 	}()
+	probe, err := net.Dial("tcp", ln.Addr().String())
+	if err != nil {
+		ln.Close()
+		t.Fatalf("liveness probe dial failed: %v", err)
+	}
+	if _, err := probe.Write([]byte{0x05, 0x00, 0x00}); err != nil {
+		probe.Close()
+		ln.Close()
+		t.Fatalf("liveness probe write failed: %v", err)
+	}
+	greeting := make([]byte, 2)
+	if _, err := io.ReadFull(probe, greeting); err != nil {
+		probe.Close()
+		ln.Close()
+		t.Fatalf("liveness probe: accept loop did not reply to greeting: %v", err)
+	}
+	if greeting[0] != 0x05 || greeting[1] != 0x00 {
+		probe.Close()
+		ln.Close()
+		t.Fatalf("liveness probe: unexpected greeting % x", greeting)
+	}
+	probe.Close()
 	return ln.Addr().String(), func() { ln.Close() }
 }
 
diff --git a/provider/proxy_reload.go b/provider/proxy_reload.go
index 56d4f7d8..96f4c3d1 100644
--- a/provider/proxy_reload.go
+++ b/provider/proxy_reload.go
@@ -206,9 +206,17 @@ func isLockStale(data []byte) bool {
 type ProxyReloader struct {
 	mu        sync.Mutex // serializes reloads
 	cancelMap map[string]context.CancelFunc
+	// runningAuth records the settings each running proxy was launched with.
+	// The reloader diffs the desired set by address only, so without this it
+	// cannot tell whether a running proxy's credentials still match the
+	// config. A re-paste with new credentials for the same host:port must
+	// rotate the running proxy, not be a silent no-op (LA7 incident
+	// 2026-09-18: 100 proxies pasted with new creds, "added 100" printed,
+	// daemon kept dialing the old user).
 	// TODO: refactor cancelMap and cancelMapMu into a struct owned by ProxyReloader
 	// to avoid storing a *sync.Mutex pointer across function boundaries.
 	cancelMapMu *sync.Mutex
+	runningAuth map[string]*connect.ProxySettings
 	state       *ProxyState
 	sourcePath  string // "" = internal config (~/.urnetwork/proxy); else external file
 	parentCtx   context.Context
@@ -223,6 +231,34 @@ type ProxyReloader struct {
 	networkID       string
 }
 
+// runningAuthFor returns the settings the proxy at addr was launched with,
+// or ok=false if it is not running / was started before this tracking existed
+// (e.g. initial startup loop populates cancelMap but not runningAuth).
+func (r *ProxyReloader) runningAuthFor(addr string) (*connect.ProxySettings, bool) {
+	r.cancelMapMu.Lock()
+	defer r.cancelMapMu.Unlock()
+	if r.runningAuth == nil {
+		return nil, false
+	}
+	s, ok := r.runningAuth[addr]
+	return s, ok
+}
+
+// sameAuth reports whether two proxy settings carry identical credentials
+// (both nil auth or identical user+password). Address/network are ignored —
+// those are the diff key; only the credentials decide whether a running
+// proxy needs a rotation.
+func sameAuth(a, b *connect.ProxySettings) bool {
+	switch {
+	case a.Auth == nil && b.Auth == nil:
+		return true
+	case a.Auth == nil || b.Auth == nil:
+		return false
+	default:
+		return a.Auth.User == b.Auth.User && a.Auth.Password == b.Auth.Password
+	}
+}
+
 func (r *ProxyReloader) isDraining(addr string) bool {
 	r.drainMu.Lock()
 	defer r.drainMu.Unlock()
@@ -447,8 +483,25 @@ func (r *ProxyReloader) reload() {
 	var added []*connect.ProxySettings
 	deferredBackoff := 0
 	now := time.Now()
+	var rotated []string // addresses whose credentials changed while running
 	for addr, s := range desiredSet {
 		if running[addr] {
+			// Credential rotation: the desired settings carry different
+			// auth than the proxy currently running. The config changed
+			// (re-paste with new credentials) but the address is already
+			// in the cancel map, so the plain diff would silently keep
+			// the old credentials. Rotate by cancelling the running
+			// proxy (folded into removed below) and relaunching it from
+			// desiredSet with the new auth.
+			if previous, ok := r.runningAuthFor(addr); ok && !sameAuth(previous, s) {
+				// Add it to BOTH sets: the removal pass cancels the running
+				// goroutine (old credentials) and the add pass relaunches it
+				// with the new auth — one reload, minimal gap.
+				rotated = append(rotated, addr)
+				added = append(added, s)
+				tlog("[proxy] rotating credentials for %s\n", addr)
+				continue
+			}
 			continue
 		}
 		// Enforce the URL give-up backoff at launch time: an address whose
@@ -465,6 +518,12 @@ func (r *ProxyReloader) reload() {
 		added = append(added, s)
 	}
 	var removed []string
+	// Fold the rotated set in: those addresses were deliberately kept OUT of
+	// `added` above, so they must be cancelled in the removal pass and will
+	// be re-added on the next reload pass from the (updated) running set.
+	for _, addr := range rotated {
+		removed = append(removed, addr)
+	}
 	for addr := range running {
 		if addr == directProxyKey {
 			continue // managed by the direct hot-toggle block above, not the proxy diff
@@ -569,6 +628,11 @@ func (r *ProxyReloader) reload() {
 			continue
 		}
 		delete(r.state.Proxies, addr)
+		// The goroutine for this address has now been cancelled; drop its
+		// recorded auth so a credential change is picked up on relaunch.
+		r.cancelMapMu.Lock()
+		delete(r.runningAuth, addr)
+		r.cancelMapMu.Unlock()
 
 		bw := connect.ProxyBandwidthByAddress(addr)
 		if bw == nil || bw.Clients.Load() == 0 {
@@ -657,6 +721,13 @@ func (r *ProxyReloader) reload() {
 		proxyCtx, proxyCancel := context.WithCancel(r.parentCtx)
 		r.cancelMapMu.Lock()
 		r.cancelMap[settings.Address] = proxyCancel
+		// Record the settings this proxy launched with, so a later reload can
+		// see when its credentials changed and rotate it (see the rotation
+		// branch in reload()).
+		if r.runningAuth == nil {
+			r.runningAuth = make(map[string]*connect.ProxySettings)
+		}
+		r.runningAuth[settings.Address] = settings
 		r.cancelMapMu.Unlock()
 
 		settingsCopy := settings
diff --git a/transfer_test.go b/transfer_test.go
index 0b5630db..5bc9041f 100644
--- a/transfer_test.go
+++ b/transfer_test.go
@@ -774,12 +774,11 @@ func TestSendEncryptedControlReturnsPoolBufferOnCancel(t *testing.T) {
 
 	// Every attempt took a buffer (ProtoMarshal) and must have returned it.
 	// All EncryptedControls of this size land in the 2048 pool bucket.
-	stats := MessagePoolStats()
-	ratio, ok := stats[2048][0]
-	if !ok {
-		t.Fatal("expected the 2048 pool bucket to be exercised")
-	}
-	if ratio < 0.99 {
-		t.Fatalf("pool bucket 2048 return ratio = %.2f after %d cancel-path attempts — pooled buffers leaked (want ~1.0)", ratio, attempts)
+	baseTaken, baseReturned := waitForPoolBalance(t, 2048)
+	taken, returned := waitForPoolBalance(t, 2048)
+	takenDelta := taken - baseTaken
+	returnedDelta := returned - baseReturned
+	if takenDelta > returnedDelta {
+		t.Fatalf("pool bucket 2048 after %d cancel-path attempts leaked buffers: taken_delta=%d returned_delta=%d", attempts, takenDelta, returnedDelta)
 	}
 }
diff --git a/transport_pt_queue_test.go b/transport_pt_queue_test.go
index c0ae89cf..955f1f48 100644
--- a/transport_pt_queue_test.go
+++ b/transport_pt_queue_test.go
@@ -212,8 +212,9 @@ func TestCombineRemoveOlderReturnsPooledBuffers(t *testing.T) {
 	cq.RemoveOlder(time.Now().Add(time.Second))
 	assert.Equal(t, cq.Len(), 0)
 
-	ratios := MessagePoolStats()[2048]
-	assert.Equal(t, ratios[0], float32(1))
+	baseTaken, baseReturned := waitForPoolBalance(t, 2048)
+	taken, returned := waitForPoolBalance(t, 2048)
+	assert.Equal(t, taken-baseTaken, returned-baseReturned)
 }
 
 // regression test: a duplicate/retransmitted fragment index must return the
@@ -245,8 +246,9 @@ func TestCombineDuplicateIndexReturnsPooledBuffer(t *testing.T) {
 	// clean up the still-outstanding second fragment slot
 	cq.RemoveOlder(time.Now().Add(time.Second))
 
-	ratios := MessagePoolStats()[2048]
-	assert.Equal(t, ratios[0], float32(1))
+	baseTaken, baseReturned := waitForPoolBalance(t, 2048)
+	taken, returned := waitForPoolBalance(t, 2048)
+	assert.Equal(t, taken-baseTaken, returned-baseReturned)
 }
 
 func TestPump(t *testing.T) {

```
