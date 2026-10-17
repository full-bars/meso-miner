//go:build windows

package main

import "os"

// snFileOwnedByCaller has no uid to compare on windows. Access there is an ACL
// matter that the unix mode bits do not express, so the file is accepted.
func snFileOwnedByCaller(info os.FileInfo) bool { return true }

// snSeedModeTooOpen is always false on windows: os.FileInfo reports a
// synthetic 0666 (or 0444) mode for every file there, so the unix rule would
// refuse every seed file. The profile directory ACL is the protection.
func snSeedModeTooOpen(info os.FileInfo) bool { return false }
