//go:build windows

package main

import "os"

// openSeedNoFollow on windows keeps the plain open: there is no O_NOFOLLOW,
// and the caller's Lstat check has already refused a symlink.
func openSeedNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}
