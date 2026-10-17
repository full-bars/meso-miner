//go:build !windows

package main

import (
	"os"
	"syscall"
)

// openSeedNoFollow opens the seed file read-only without following a final
// symlink (it fails with ELOOP instead). O_NONBLOCK keeps a path swapped for a
// fifo from hanging the open; the caller then refuses it as not regular.
func openSeedNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
