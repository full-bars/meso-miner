// wallet_consent_lock_test.go — the journal lock: two concurrent stores must
// not lose each other's entry, the second store must wait while the first
// holds the lock, and two concurrent `wallet set` runs for the same consent
// must sign exactly once.

package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vedhavyas/go-subkey/v2"
)

// lockTestEntry is a journal entry for a synthetic coldkey; the journal is
// keyed by (api url, coldkey), so two of these stay separate entries.
func lockTestEntry(coldkey string) snConsentJournalEntry {
	return snConsentJournalEntry{
		ApiUrl:    "https://api.example.com",
		Coldkey:   coldkey,
		Message:   "consent message for " + coldkey,
		Signature: "0x" + strings.Repeat("ab", 64),
		FromEpoch: 51,
	}
}

// lockTestJournalEntries reports how many entries the journal file holds; it
// is safe to call from a store goroutine (no t.Fatal).
func lockTestJournalEntries(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var entries []snConsentJournalEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}

// requireConsentJournalSerialization skips the calling test when this
// platform's journal lock cannot serialize two acquirers. The probe checks
// LOCK BEHAVIOR (a second acquisition must block while the first is held)
// instead of switching on GOOS, so a future non-no-op implementation is
// covered the day it lands. windows is the no-op case: see the documented
// rationale in wallet_consent_lock_windows.go (the wallet set flow is
// interactive there, one run at a time by hand).
func requireConsentJournalSerialization(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wallet_consent.json")
	release, err := snConsentJournalLockFn(path)
	if err != nil {
		t.Fatalf("probe: first journal lock acquisition: %v", err)
	}
	type lockResult struct {
		release func()
		err     error
	}
	second := make(chan lockResult, 1)
	go func() {
		release2, err2 := snConsentJournalLockFn(path)
		second <- lockResult{release2, err2}
	}()
	select {
	case result := <-second:
		// Never blocked: this platform's lock is a no-op.
		if result.err != nil {
			t.Fatalf("probe: second journal lock acquisition: %v", result.err)
		}
		result.release()
		release()
		t.Skip("the wallet consent journal lock does not serialize concurrent acquirers on this platform (windows: a documented no-op, see wallet_consent_lock_windows.go); the serialization guarantee this test asserts is not provided here")
	case <-time.After(250 * time.Millisecond):
		// Blocked as expected: let the second acquirer through and clean up.
		release()
		result := <-second
		if result.err != nil {
			t.Fatalf("probe: second journal lock acquisition: %v", result.err)
		}
		result.release()
	}
}

// The fail-before control and the fix, one after the other. With the lock
// stubbed out both stores pause after their own load, each sees the empty
// journal, and the last write wins: an entry is LOST. With the real lock the
// same concurrent pair serializes and keeps both.
func TestSnConsentJournalConcurrentStoresKeepEveryEntry(t *testing.T) {
	first, second := lockTestEntry("5First"), lockTestEntry("5Second")

	t.Run("without the lock one entry is lost", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wallet_consent.json")
		realLock := snConsentJournalLockFn
		snConsentJournalLockFn = func(string) (func(), error) { return func() {}, nil }
		arrived, open := atomic.Int32{}, make(chan struct{})
		snConsentJournalAfterLoadHook = func() {
			// both stores hold their read open until both have read, so the
			// lost update is deterministic instead of a timing race
			if arrived.Add(1) == 2 {
				close(open)
			}
			<-open
		}
		defer func() {
			snConsentJournalLockFn = realLock
			snConsentJournalAfterLoadHook = nil
		}()

		var stores sync.WaitGroup
		for _, entry := range []snConsentJournalEntry{first, second} {
			stores.Add(1)
			go func(entry snConsentJournalEntry) {
				defer stores.Done()
				if err := snConsentJournalStore(path, entry); err != nil {
					t.Errorf("store: %v", err)
				}
			}(entry)
		}
		stores.Wait()

		kept, err := lockTestJournalEntries(path)
		if err != nil {
			t.Fatalf("read the journal back: %v", err)
		}
		if kept != 1 {
			t.Fatalf("%d journal entries survived without the lock, want the lost update (1 of 2)", kept)
		}
		t.Logf("control: without the lock %d of the 2 concurrent stores' entries survived, the other was written over from a stale load", kept)
	})

	t.Run("with the lock both entries are kept", func(t *testing.T) {
		requireConsentJournalSerialization(t)
		path := filepath.Join(t.TempDir(), "wallet_consent.json")

		var stores sync.WaitGroup
		for _, entry := range []snConsentJournalEntry{first, second} {
			stores.Add(1)
			go func(entry snConsentJournalEntry) {
				defer stores.Done()
				if err := snConsentJournalStore(path, entry); err != nil {
					t.Errorf("store: %v", err)
				}
			}(entry)
		}
		stores.Wait()

		for _, want := range []snConsentJournalEntry{first, second} {
			if entry, err := snConsentJournalLoad(path, want.ApiUrl, want.Coldkey); err != nil || entry == nil {
				t.Fatalf("the entry for %s was lost: %v", want.Coldkey, err)
			}
		}
	})
}

// The second store must WAIT for the lock. The first store is held inside its
// read-modify-write, after its load and before its write; the second one asks
// for the lock and must not enter the same section while the first holds it.
// Once in, its own load already contains the first store's entry, so the two
// read-modify-writes never interleave.
func TestSnConsentJournalLockHoldsAcrossTheStore(t *testing.T) {
	requireConsentJournalSerialization(t)
	path := filepath.Join(t.TempDir(), "wallet_consent.json")
	first, second := lockTestEntry("5First"), lockTestEntry("5Second")

	realLock := snConsentJournalLockFn
	var acquires, releases, active, maxActive atomic.Int32
	attempted := make(chan struct{}, 2)
	snConsentJournalLockFn = func(lockPath string) (func(), error) {
		attempted <- struct{}{}
		release, err := realLock(lockPath)
		if err != nil {
			return nil, err
		}
		acquires.Add(1)
		if n := active.Add(1); n > maxActive.Load() {
			maxActive.Store(n)
		}
		return func() {
			releases.Add(1)
			active.Add(-1)
			release()
		}, nil
	}

	views := make(chan int, 2)
	entered := make(chan struct{}, 2)
	open := make(chan struct{})
	releaseOpen := sync.OnceFunc(func() { close(open) })
	snConsentJournalAfterLoadHook = func() {
		// what the store's load just saw: nobody else writes while the lock
		// is held, so the file here reports the load's view
		kept, err := lockTestJournalEntries(path)
		if err != nil {
			t.Errorf("read the journal inside the store: %v", err)
		}
		views <- kept
		entered <- struct{}{}
		<-open
	}
	defer func() {
		releaseOpen()
		snConsentJournalLockFn = realLock
		snConsentJournalAfterLoadHook = nil
	}()

	done := make(chan error, 2)
	go func() { done <- snConsentJournalStore(path, first) }()
	if kept := <-views; kept != 0 {
		t.Fatalf("the first store's load saw %d entries, want the empty journal", kept)
	}
	<-entered // the first store holds the lock, paused mid read-modify-write
	<-attempted

	go func() { done <- snConsentJournalStore(path, second) }()
	<-attempted // the second store is asking for the lock now
	select {
	case <-entered:
		t.Fatal("the second store entered the read-modify-write while the first held the lock")
	case <-time.After(250 * time.Millisecond):
	}

	releaseOpen() // the first store finishes its write and releases the lock
	if err := <-done; err != nil {
		t.Fatalf("the first store: %v", err)
	}
	if kept := <-views; kept != 1 {
		t.Fatalf("the second store's load saw %d entries, want the first store's entry already in the journal", kept)
	}
	<-entered
	if err := <-done; err != nil {
		t.Fatalf("the second store: %v", err)
	}

	kept, err := lockTestJournalEntries(path)
	if err != nil || kept != 2 {
		t.Fatalf("the journal holds %d entries after two concurrent stores (%v), want 2", kept, err)
	}
	for _, want := range []snConsentJournalEntry{first, second} {
		if entry, err := snConsentJournalLoad(path, want.ApiUrl, want.Coldkey); err != nil || entry == nil {
			t.Fatalf("the entry for %s was lost: %v", want.Coldkey, err)
		}
	}
	if acquires.Load() != 2 || releases.Load() != 2 {
		t.Fatalf("acquire/release = %d/%d, want 2/2", acquires.Load(), releases.Load())
	}
	if maxActive.Load() != 1 {
		t.Fatalf("%d stores held the lock at once, want at most one", maxActive.Load())
	}
}

// Two concurrent `wallet set` runs for the same consent must sign exactly
// once: the run that wins the journal lock signs, retains and stores; the
// other waits for the lock, finds the identical consent already acknowledged
// and refuses instead of signing a second one.
func TestSnConsentWalletSetConcurrentRunsSignOnce(t *testing.T) {
	requireConsentJournalSerialization(t)
	consentTestHome(t)
	t.Setenv("URNETWORK_WALLET_ALLOW_CUSTOM_API", "1")
	seed := walletTestSeed(0x11)
	address := walletTestAddress(t, seed)
	seedPath := writeSeedFile(t, t.TempDir(), seed, 0o600, true)
	fake := &consentServer{t: t, want: consentTestExpect(t, seed, time.Now())}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	var signs atomic.Int32
	realSign := snSignWalletChallengeFn
	snSignWalletChallengeFn = func(keypair subkey.KeyPair, message string) (string, error) {
		signs.Add(1)
		return realSign(keypair, message)
	}
	defer func() { snSignWalletChallengeFn = realSign }()

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- runConsentSet(t, server, address, seedPath)
		}()
	}
	close(start)
	outcomes := []error{<-results, <-results}

	if got := signs.Load(); got != 1 {
		t.Fatalf("the coldkey signed %d times for one consent; two concurrent `wallet set` runs must sign once", got)
	}
	if fake.challenges != 1 || len(fake.submissions) != 1 {
		t.Fatalf("%d consents were requested and %d submitted, want 1 and 1", fake.challenges, len(fake.submissions))
	}
	stored, refused := 0, 0
	for _, err := range outcomes {
		switch {
		case err == nil:
			stored++
		case strings.Contains(err.Error(), "already stored and acknowledged"):
			refused++
		default:
			t.Fatalf("unexpected outcome from a concurrent run: %v", err)
		}
	}
	if stored != 1 || refused != 1 {
		t.Fatalf("%d runs stored and %d refused, want one stored and one refused", stored, refused)
	}
	path, err := snConsentJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := snConsentJournalLoad(path, server.URL, address)
	if err != nil || entry == nil || !entry.Acked {
		t.Fatalf("the journal does not hold the one acknowledged consent: %v %+v", err, entry)
	}
}
