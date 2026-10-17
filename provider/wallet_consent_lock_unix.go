//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const snConsentJournalLockSuffix = ".lock"

// snConsentJournalLock takes the process-wide lock on the wallet consent
// journal: a blocking, exclusive flock on <journal>.lock, the same mechanism
// the jwt and pending-overrides stores use. Every process that reads or writes
// the journal takes it, so their read-modify-writes serialize and no entry is
// lost. The lock file is created 0600 (with its directory 0700) when missing
// and is never removed: no other lock in the codebase removes its lock file,
// and unlinking a lock another process is about to flock would let two later
// lockers hold different files. Returns a release function that must be called
// when done; it releases the flock and closes the file.
func snConsentJournalLock(path string) (release func(), err error) {
	// the lock is the first thing to touch the state dir on a fresh install,
	// before any journal write can create it
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("wallet consent journal lock dir: %w", err)
	}
	f, err := os.OpenFile(path+snConsentJournalLockSuffix, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open wallet consent journal lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock wallet consent journal: %w", err)
	}
	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
