//go:build !unix

package main

import (
	"os"
)

// Non-unix fallback: no Stat_t (nlink/uid) and no O_NOFOLLOW, so only the
// portable checks (regular file, mode) apply. The seed custody policy is
// strongest on the unix targets the provider actually runs on.

const seedFileOpenFlags = os.O_RDONLY

func snSeedFileOwnership(info os.FileInfo) error {
	return nil
}

func snSeedFileSameInode(lstatInfo os.FileInfo, openStat os.FileInfo) error {
	return nil
}
