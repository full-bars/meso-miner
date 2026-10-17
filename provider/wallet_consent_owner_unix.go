//go:build !windows

package main

import (
	"os"
	"syscall"
)

// snFileOwnedByCaller reports whether the file belongs to the user running the
// provider, so a seed file another user could have written is refused.
func snFileOwnedByCaller(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || int(stat.Uid) == os.Geteuid()
}

// snSeedModeTooOpen reports whether group or other can read the seed file.
func snSeedModeTooOpen(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 != 0
}
