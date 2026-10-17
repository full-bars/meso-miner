//go:build !linux

package main

// The real mount census behind the fleet durable fixture is observed on Linux
// only; other platforms fall back to the synthetic host.

// fleetRealMountEntry is one parsed mount record (unused off Linux).
type fleetRealMountEntry struct {
	Path   string
	FsType string
	Major  uint32
	Minor  uint32
}

func fleetRealMountCensus() ([]fleetRealMountEntry, bool) {
	return nil, false
}

func fleetRealFilesystemUuid(major, minor uint32) (string, bool) {
	return "", false
}
