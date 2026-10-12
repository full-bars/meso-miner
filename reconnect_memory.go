package connect

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// memoryPressureFreeShare: pressure when less than this share of the memory this
// process may use is available.
const memoryPressureFreeShare = 0.07

// memoryPressureFrom decides from raw numbers whether memory is critically
// short. avail and total are in bytes; zero total means unknown, which is never
// pressure.
//
// The Go heap nearing GOMEMLIMIT is deliberately NOT a signal: turbo runs with
// GOGC=200 under a limit of 80% of RAM, and a GC-off box grows to the limit, so
// the heap sits near it as the runtime intends.
func memoryPressureFrom(avail uint64, total uint64) bool {
	return 0 < total && float64(avail) < memoryPressureFreeShare*float64(total)
}

// readMemoryPressure samples the memory this process can actually use: its
// cgroup's headroom when it runs under a memory limit (Docker, systemd
// MemoryMax), the host's MemAvailable otherwise. /proc/meminfo shows the host
// even inside a container, so reading only it would miss exactly the container
// that is about to be OOM-killed.
func readMemoryPressure() bool {
	if headroom, ok := CgroupMemoryHeadroomBytes(); ok {
		if ceiling, ok := CgroupMemoryCeiling(); ok && 0 < ceiling && 0 <= headroom {
			return memoryPressureFrom(uint64(headroom), uint64(ceiling))
		}
	}
	avail, total := hostMemory()
	return memoryPressureFrom(avail, total)
}

// hostMemory reads MemAvailable and MemTotal in bytes; zero when unreadable
// (non-Linux, restricted /proc), which reads as "unknown", never as pressure.
func hostMemory() (avail uint64, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemAvailable:":
			avail = kb << 10
		case "MemTotal:":
			total = kb << 10
		}
	}
	return avail, total
}
