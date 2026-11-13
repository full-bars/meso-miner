package urnettools

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// openbsdManagedPID identifies a service only when rc.d's saved matching
// expression selects exactly one process. Zero means confirmed absent; -1 means
// the lookup is ambiguous or failed.
func openbsdManagedPID(runfile string, lookup func(string, string) ([]byte, error)) int {
	values := make(map[string]string)
	for _, line := range strings.Split(runfile, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "pexp" && key != "daemon_rtable") {
			continue
		}
		if _, exists := values[key]; exists {
			return -1
		}
		values[key] = value
	}
	pattern, table := values["pexp"], values["daemon_rtable"]
	if pattern == "" || strings.ContainsAny(pattern, "\r\n") || table == "" {
		return -1
	}
	if _, err := strconv.ParseUint(table, 10, 31); err != nil {
		return -1
	}
	out, err := lookup(table, pattern)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(strings.TrimSpace(string(out))) == 0 {
			return 0
		}
		return -1
	}
	fields := strings.Fields(string(out))
	if len(fields) != 1 {
		return -1
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return -1
	}
	return pid
}
