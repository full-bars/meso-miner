//go:build windows

package main

// snConsentJournalLock is a documented no-op on windows: there is no flock(2)
// there, and the journal writes rely on the existing atomic temp+rename, so
// one writer always leaves a consistent file. Two concurrent writers could
// still race (both load the same entries, the last rename wins, the other
// entry is lost, and two `wallet set` runs would both sign), which is accepted
// on windows because the wallet set flow is interactive: one run at a time, by
// hand. The provider's other lock files (jwt, pending_overrides) use
// LockFileEx because urnet-tools shares them across processes; the journal is
// provider-only and keeps the simplest contract here.
func snConsentJournalLock(path string) (release func(), err error) {
	return func() {}, nil
}
