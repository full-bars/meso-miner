//go:build linux

package main

// Real kernel mount census for the fleet durable fixture. The sn module's own
// durablepath observes these same facts, so the fixture declares them.

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// fleetRealMountEntry is one parsed /proc/self/mountinfo record.
type fleetRealMountEntry struct {
	Path   string
	FsType string
	Major  uint32
	Minor  uint32
}

// fleetRealMountCensus parses the real mount census. ok=false when it cannot
// be observed.
func fleetRealMountCensus() ([]fleetRealMountEntry, bool) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, false
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err := errors.Join(readErr, file.Close()); err != nil || len(raw) > 4*1024*1024 {
		return nil, false
	}
	unescape := func(value string) string {
		value = strings.ReplaceAll(value, `\040`, " ")
		value = strings.ReplaceAll(value, `\011`, "\t")
		value = strings.ReplaceAll(value, `\012`, "\n")
		return strings.ReplaceAll(value, `\134`, `\`)
	}
	var result []fleetRealMountEntry
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		separator := -1
		for index, part := range parts {
			if part == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || len(parts) != separator+4 {
			continue
		}
		deviceParts := strings.SplitN(parts[2], ":", 2)
		if len(deviceParts) != 2 {
			continue
		}
		major, majorErr := strconv.ParseUint(deviceParts[0], 10, 32)
		minor, minorErr := strconv.ParseUint(deviceParts[1], 10, 32)
		if majorErr != nil || minorErr != nil {
			continue
		}
		result = append(result, fleetRealMountEntry{Path: unescape(parts[4]), FsType: parts[separator+1], Major: uint32(major), Minor: uint32(minor)})
	}
	if scanner.Err() != nil || len(result) == 0 {
		return nil, false
	}
	return result, true
}

// fleetRealFilesystemUuid resolves the /dev/disk/by-uuid entry whose block
// device matches the selected mount. ok=false when none resolves.
func fleetRealFilesystemUuid(major, minor uint32) (string, bool) {
	entries, err := os.ReadDir("/dev/disk/by-uuid")
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		path := "/dev/disk/by-uuid/" + entry.Name()
		var stat unix.Stat_t
		if err := unix.Stat(path, &stat); err != nil {
			continue
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFBLK {
			continue
		}
		if unix.Major(uint64(stat.Rdev)) == major && unix.Minor(uint64(stat.Rdev)) == minor {
			return entry.Name(), true
		}
	}
	return "", false
}
