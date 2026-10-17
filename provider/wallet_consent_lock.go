package main

// wallet_consent_lock.go -- the one lock that orders every change to the
// wallet consent journal, within this process and across processes.
//
// The journal is read-modify-written: snConsentJournalStore and
// snConsentJournalDrop load every entry, replace or remove one, and write the
// file back. Two writers that both load before either writes would let the
// second write drop the first one's entry, so both take snConsentJournalLock:
// an exclusive flock on the journal's .lock file on unix
// (wallet_consent_lock_unix.go), a documented no-op on windows
// (wallet_consent_lock_windows.go).
//
// The journal functions are layered so the lock is never taken twice while
// held: snConsentJournalStore, snConsentJournalDrop, snResolveRetainedConsent
// and snStoreNetworkConsent take the lock, while their ...Locked variants
// assume the caller holds it. `wallet set` holds it once from the first
// journal read through the stored consent, so a concurrent run for the same
// consent waits there and then replays or refuses the retained consent
// instead of signing a second one.

// snConsentJournalLockFn is a test seam: the lock tests replace it with a
// no-op to show the lost update the real lock prevents, and wrap it to count
// the acquisitions and prove the second writer waits.
var snConsentJournalLockFn = snConsentJournalLock

// snConsentJournalAfterLoadHook is a test seam: it runs inside the store's
// read-modify-write, after the journal is loaded and before it is written, so
// a test can hold the read open and observe which entries the load saw. nil in
// production.
var snConsentJournalAfterLoadHook func()
