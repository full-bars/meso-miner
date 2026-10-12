package connect

import (
	"context"
	"testing"
	"time"
)

func resetAddrHealth() {
	addrHealth.mu.Lock()
	addrHealth.m = nil
	addrHealth.good = nil
	addrHealth.mu.Unlock()
}

func resetDnsCache() {
	dnsCache.mu.Lock()
	dnsCache.m = nil
	dnsCache.neg = nil
	dnsCache.mu.Unlock()
}

func TestAddrMarkedBadAfterRepeatedStalls(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	for i := 0; i < addrFailThreshold-1; i++ {
		noteAddrOutcome("api.example", "10.0.0.1", false, now)
	}
	if addrIsBad("api.example", "10.0.0.1", now) {
		t.Fatal("marked bad before the threshold")
	}
	noteAddrOutcome("api.example", "10.0.0.1", false, now)
	if !addrIsBad("api.example", "10.0.0.1", now) {
		t.Fatal("not marked bad at the threshold")
	}
	if addrIsBad("api.example", "10.0.0.1", now.Add(addrBadFor+time.Second)) {
		t.Fatal("must expire after addrBadFor")
	}
	if addrIsBad("api.example", "10.0.0.2", now) || addrIsBad("other.example", "10.0.0.1", now) {
		t.Fatal("badness leaked to another address or host")
	}
}

func TestAddrSuccessClearsAndOldFailuresDoNotAccumulate(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	noteAddrOutcome("h", "10.0.0.1", false, now)
	noteAddrOutcome("h", "10.0.0.1", false, now)
	noteAddrOutcome("h", "10.0.0.1", true, now)
	noteAddrOutcome("h", "10.0.0.1", false, now)
	if addrIsBad("h", "10.0.0.1", now) {
		t.Fatal("a success must reset the failure count")
	}
	// failures spread beyond the window never reach the threshold
	resetAddrHealth()
	noteAddrOutcome("h", "10.0.0.1", false, now)
	noteAddrOutcome("h", "10.0.0.1", false, now.Add(addrFailWindow+time.Second))
	noteAddrOutcome("h", "10.0.0.1", false, now.Add(2*addrFailWindow+2*time.Second))
	if addrIsBad("h", "10.0.0.1", now.Add(2*addrFailWindow+2*time.Second)) {
		t.Fatal("stale failures accumulated")
	}
	// a success also clears an already bad address
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("h", "10.0.0.9", false, now)
	}
	noteAddrOutcome("h", "10.0.0.9", true, now)
	if addrIsBad("h", "10.0.0.9", now) {
		t.Fatal("success must clear a bad address")
	}
}

func TestChooseSkipsBadAndFallsBackWhenAllBad(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("h", "1.1.1.1", false, now)
		noteAddrOutcome("h", "2.2.2.2", false, now)
	}
	for i := 0; i < 200; i++ {
		if got := chooseTargetAddr(context.Background(), "h", ips); got != "3.3.3.3" {
			t.Fatalf("picked %s, only 3.3.3.3 is healthy", got)
		}
	}
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("h", "3.3.3.3", false, now)
	}
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		seen[chooseTargetAddr(context.Background(), "h", ips)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("with every address marked it must still pick among all, saw %v", seen)
	}
}

func TestChooseSpreadsAcrossHealthyAddresses(t *testing.T) {
	resetAddrHealth()
	seen := map[string]int{}
	for i := 0; i < 600; i++ {
		seen[chooseTargetAddr(context.Background(), "h", []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"})]++
	}
	if len(seen) != 3 {
		t.Fatalf("must spread over healthy addresses, saw %v", seen)
	}
}

func TestChoicePinnedPerAttempt(t *testing.T) {
	resetAddrHealth()
	stats := &attemptStats{}
	ctx := withAttemptStats(context.Background(), stats)
	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}
	first := chooseTargetAddr(ctx, "h", ips)
	for i := 0; i < 100; i++ {
		if got := chooseTargetAddr(ctx, "h", ips); got != first {
			t.Fatalf("attempt switched address %s -> %s", first, got)
		}
	}
	// a different attempt is free to choose differently
	other := map[string]bool{}
	for i := 0; i < 200; i++ {
		other[chooseTargetAddr(withAttemptStats(context.Background(), &attemptStats{}), "h", ips)] = true
	}
	if len(other) < 2 {
		t.Fatalf("separate attempts all picked the same address: %v", other)
	}
	// a pinned address that left the answer set is replaced
	if got := chooseTargetAddr(ctx, "h", []string{"9.9.9.9"}); got != "9.9.9.9" {
		t.Fatalf("stale pin kept: %s", got)
	}
}

func TestOutcomeAttributedToTheDialledAddress(t *testing.T) {
	resetAddrHealth()
	stats := &attemptStats{}
	ctx := withAttemptStats(context.Background(), stats)
	ip := chooseTargetAddr(ctx, "api.example", []string{"1.1.1.1", "2.2.2.2"})

	// never reached the address: a proxy-level failure and a missing dial say nothing
	for i := 0; i < 10; i++ {
		stats.recordTargetOutcome(connectTlsFail)
	}
	if addrIsBad("api.example", ip, time.Now()) {
		t.Fatal("blamed an address the proxy never reached")
	}

	stats.noteDialedTarget("api.example")
	stats.recordTargetOutcome(connectSocksFail)
	stats.recordTargetOutcome(connectRefused)
	stats.recordTargetOutcome(connectUpgradeFail)
	stats.recordTargetOutcome(connectCanceled)
	if addrIsBad("api.example", ip, time.Now()) {
		t.Fatal("classes that are not evidence about the address must not count")
	}
	for i := 0; i < addrFailThreshold; i++ {
		stats.recordTargetOutcome(connectTimeoutNoReply)
	}
	if !addrIsBad("api.example", ip, time.Now()) {
		t.Fatal("repeated silent stalls after a successful CONNECT must mark the address")
	}
	stats.recordTargetOutcome(connectOK)
	if addrIsBad("api.example", ip, time.Now()) {
		t.Fatal("a connected attempt must clear the address")
	}
	var nilStats *attemptStats
	nilStats.recordTargetOutcome(connectOK) // must not panic
}

func TestLookupProxyTargetUsesFullAnswerSetAndSkipsBadAddress(t *testing.T) {
	resetAddrHealth()
	resetDnsCache()
	defer resetDnsCache()
	dnsCache.mu.Lock()
	dnsCache.m = map[string]dnsCacheEntry{
		"multi.example":  {ip: "1.1.1.1", ips: []string{"1.1.1.1", "2.2.2.2"}, expiry: time.Now().Add(time.Minute)},
		"legacy.example": {ip: "7.7.7.7", expiry: time.Now().Add(time.Minute)},
	}
	dnsCache.mu.Unlock()

	if ip, ok := lookupProxyTarget(context.Background(), "legacy.example"); !ok || ip != "7.7.7.7" {
		t.Fatalf("single-address entry: %q %v", ip, ok)
	}
	now := time.Now()
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("multi.example", "1.1.1.1", false, now)
	}
	for i := 0; i < 100; i++ {
		if ip, ok := lookupProxyTarget(context.Background(), "multi.example"); !ok || ip != "2.2.2.2" {
			t.Fatalf("dead 1.1.1.1 was handed out: %q %v", ip, ok)
		}
	}
}

func TestAddrHealthTableIsBounded(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	for i := 0; i < addrHealthMax+500; i++ {
		noteAddrOutcome("h", "10.1."+itoa3(i), false, now)
	}
	addrHealth.mu.Lock()
	n := len(addrHealth.m)
	addrHealth.mu.Unlock()
	if n > addrHealthMax {
		t.Fatalf("table grew to %d", n)
	}
}

func itoa3(i int) string {
	return string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
}

// DNS can return a single record per lookup. When that one is marked bad the only
// way out is an address that answered before.
func TestFallsBackToRememberedGoodWhenTheOnlyAnswerIsBad(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	noteAddrOutcome("api.example", "5.5.5.5", true, now) // answered earlier
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("api.example", "6.6.6.6", false, now)
	}
	for i := 0; i < 50; i++ {
		if got := chooseTargetAddr(context.Background(), "api.example", []string{"6.6.6.6"}); got != "5.5.5.5" {
			t.Fatalf("picked %s; the lone answer is marked bad and 5.5.5.5 answered recently", got)
		}
	}
	// a healthy lone answer is used as is
	if got := chooseTargetAddr(context.Background(), "api.example", []string{"7.7.7.7"}); got != "7.7.7.7" {
		t.Fatalf("healthy answer overridden: %s", got)
	}
	// the memory expires
	noteAddrOutcome("old.example", "8.8.8.8", true, now.Add(-addrGoodFor-time.Minute))
	if g := knownGood("old.example", now); len(g) != 0 {
		t.Fatalf("expired good address still offered: %v", g)
	}
	// a remembered address that has since been marked bad is not offered
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("api.example", "5.5.5.5", false, now)
	}
	if g := knownGood("api.example", now); len(g) != 0 {
		t.Fatalf("bad address offered as known good: %v", g)
	}
}

func TestPinSurvivesRememberedGoodFallback(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	noteAddrOutcome("h", "5.5.5.5", true, now)
	for i := 0; i < addrFailThreshold; i++ {
		noteAddrOutcome("h", "6.6.6.6", false, now)
	}
	ctx := withAttemptStats(context.Background(), &attemptStats{})
	first := chooseTargetAddr(ctx, "h", []string{"6.6.6.6"})
	for i := 0; i < 20; i++ {
		if got := chooseTargetAddr(ctx, "h", []string{"6.6.6.6"}); got != first {
			t.Fatalf("pin lost: %s -> %s", first, got)
		}
	}
}
