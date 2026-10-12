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

var failureProxy int

// noteFailure records a stall from a different proxy each time, with another
// address of the host having answered just now: the evidence that marks an
// address bad.
func noteFailure(host, ip string, now time.Time) {
	failureProxy++
	addrHealth.mu.Lock()
	if addrHealth.good == nil {
		addrHealth.good = map[string]map[string]time.Time{}
	}
	if addrHealth.good[host] == nil {
		addrHealth.good[host] = map[string]time.Time{}
	}
	added := len(addrHealth.good[host]) == 0
	if added {
		addrHealth.good[host]["192.0.2.254"] = now
	}
	addrHealth.mu.Unlock()
	noteAddrOutcome(host, ip, failureProxy, false, now)
	if added {
		addrHealth.mu.Lock()
		delete(addrHealth.good[host], "192.0.2.254")
		addrHealth.mu.Unlock()
	}
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
		noteFailure("api.example", "10.0.0.1", now)
	}
	if addrIsBad("api.example", "10.0.0.1", now) {
		t.Fatal("marked bad before the threshold")
	}
	noteFailure("api.example", "10.0.0.1", now)
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
	noteFailure("h", "10.0.0.1", now)
	noteFailure("h", "10.0.0.1", now)
	noteAddrOutcome("h", "10.0.0.1", 0, true, now)
	noteFailure("h", "10.0.0.1", now)
	if addrIsBad("h", "10.0.0.1", now) {
		t.Fatal("a success must reset the failure count")
	}
	// failures spread beyond the window never reach the threshold
	resetAddrHealth()
	noteFailure("h", "10.0.0.1", now)
	noteFailure("h", "10.0.0.1", now.Add(addrFailWindow+time.Second))
	noteFailure("h", "10.0.0.1", now.Add(2*addrFailWindow+2*time.Second))
	if addrIsBad("h", "10.0.0.1", now.Add(2*addrFailWindow+2*time.Second)) {
		t.Fatal("stale failures accumulated")
	}
	// a success also clears an already bad address
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("h", "10.0.0.9", now)
	}
	noteAddrOutcome("h", "10.0.0.9", 0, true, now)
	if addrIsBad("h", "10.0.0.9", now) {
		t.Fatal("success must clear a bad address")
	}
}

func TestChooseSkipsBadAndFallsBackWhenAllBad(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("h", "1.1.1.1", now)
		noteFailure("h", "2.2.2.2", now)
	}
	for i := 0; i < 200; i++ {
		if got := chooseTargetAddr(context.Background(), "h", ips); got != "3.3.3.3" {
			t.Fatalf("picked %s, only 3.3.3.3 is healthy", got)
		}
	}
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("h", "3.3.3.3", now)
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

	stats.noteDialedTarget("api.example", 7)
	stats.recordTargetOutcome(connectSocksFail)
	stats.recordTargetOutcome(connectRefused)
	stats.recordTargetOutcome(connectUpgradeFail)
	stats.recordTargetOutcome(connectCanceled)
	if addrIsBad("api.example", ip, time.Now()) {
		t.Fatal("classes that are not evidence about the address must not count")
	}
	// stalls from several different proxies while a sibling address answers
	other := "2.2.2.2"
	if ip == other {
		other = "1.1.1.1"
	}
	noteAddrOutcome("api.example", other, 0, true, time.Now())
	for i := 0; i < addrFailThreshold; i++ {
		stats.noteDialedTarget("api.example", 100+i)
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
		noteFailure("multi.example", "1.1.1.1", now)
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
		noteFailure("h", "10.1."+itoa3(i), now)
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
	noteAddrOutcome("api.example", "5.5.5.5", 0, true, now) // answered earlier
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("api.example", "6.6.6.6", now)
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
	noteAddrOutcome("old.example", "8.8.8.8", 0, true, now.Add(-addrGoodFor-time.Minute))
	if g := knownGood("old.example", now); len(g) != 0 {
		t.Fatalf("expired good address still offered: %v", g)
	}
	// a remembered address that has since been marked bad is not offered
	noteAddrOutcome("api.example", "4.4.4.4", 0, true, now) // a sibling that answers
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("api.example", "5.5.5.5", now)
	}
	for _, ip := range knownGood("api.example", now) {
		if ip == "5.5.5.5" {
			t.Fatalf("bad address offered as known good: %v", ip)
		}
	}
}

func TestPinSurvivesRememberedGoodFallback(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	noteAddrOutcome("h", "5.5.5.5", 0, true, now)
	for i := 0; i < addrFailThreshold; i++ {
		noteFailure("h", "6.6.6.6", now)
	}
	ctx := withAttemptStats(context.Background(), &attemptStats{})
	first := chooseTargetAddr(ctx, "h", []string{"6.6.6.6"})
	for i := 0; i < 20; i++ {
		if got := chooseTargetAddr(ctx, "h", []string{"6.6.6.6"}); got != first {
			t.Fatalf("pin lost: %s -> %s", first, got)
		}
	}
}

// Stalls that come from one proxy, or while no other address answers, say
// nothing about the address and must not mark it.
func TestAddrNotMarkedByOneProxyOrWithoutASiblingAnswering(t *testing.T) {
	resetAddrHealth()
	now := time.Now()
	for i := 0; i < 10; i++ {
		noteAddrOutcome("h", "10.0.0.1", 5, false, now) // one proxy, however often
	}
	if addrIsBad("h", "10.0.0.1", now) {
		t.Fatal("one proxy's stalls marked the address")
	}
	resetAddrHealth()
	for i := 0; i < 10; i++ {
		noteAddrOutcome("h", "10.0.0.1", i, false, now) // many proxies, nothing answers anywhere
	}
	if addrIsBad("h", "10.0.0.1", now) {
		t.Fatal("an address was marked while no sibling answered")
	}
	noteAddrOutcome("h", "10.0.0.2", 99, true, now) // a sibling answers
	noteAddrOutcome("h", "10.0.0.1", 11, false, now)
	if !addrIsBad("h", "10.0.0.1", now) {
		t.Fatal("distinct proxies stalling on one address while a sibling answers must mark it")
	}
}

func TestTargetHealthKillSwitchRestoresTheFirstAddress(t *testing.T) {
	resetAddrHealth()
	defer func(v bool) { targetHealthEnabled = v }(targetHealthEnabled)
	targetHealthEnabled = false
	now := time.Now()
	for i := 0; i < 6; i++ {
		noteFailure("h", "1.1.1.1", now)
	}
	if addrIsBad("h", "1.1.1.1", now) {
		t.Fatal("recorded health while disabled")
	}
	for i := 0; i < 20; i++ {
		if got := chooseTargetAddr(context.Background(), "h", []string{"1.1.1.1", "2.2.2.2"}); got != "1.1.1.1" {
			t.Fatalf("disabled must keep the first record, got %s", got)
		}
	}
}
