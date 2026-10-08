//go:build unix

package main

// Seed-file custody checks that need unix-only syscalls (Stat_t, O_NOFOLLOW),
// mirroring the parts of crv4's seed_file policy this port enforces itself:
// no hardlinks (a second name is an unrevocable reference) and owner-only
// custody, with an O_NOFOLLOW open so the path cannot swap to a symlink
// between the Lstat and the open.

import (
	"fmt"
	"os"
	"syscall"
)

const seedFileOpenFlags = os.O_RDONLY | syscall.O_NOFOLLOW

// snSeedFileOwnership refuses a seed file that is hardlinked elsewhere or
// owned by another user.
func snSeedFileOwnership(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if stat.Nlink != 1 {
		return fmt.Errorf("seed file must not be hardlinked (nlink %d)", stat.Nlink)
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("seed file must be owned by the current user (uid %d)", stat.Uid)
	}
	return nil
}

// snSeedFileSameInode re-checks the open descriptor against the Lstat so a
// path swapped between check and open is caught.
func snSeedFileSameInode(lstatInfo os.FileInfo, openStat os.FileInfo) error {
	lstatT, lstatOk := lstatInfo.Sys().(*syscall.Stat_t)
	openT, openOk := openStat.Sys().(*syscall.Stat_t)
	if !lstatOk || !openOk {
		return nil
	}
	if lstatT.Ino != openT.Ino || lstatT.Dev != openT.Dev {
		return fmt.Errorf("file changed on disk while opening (possible symlink swap)")
	}
	return nil
}
