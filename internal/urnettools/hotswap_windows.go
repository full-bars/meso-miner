//go:build windows

package urnettools

import "errors"

// triggerHotSwap on Windows is a stub until the named pipe adapter is connected.
func triggerHotSwap(p Provider) error {
	return errors.New("zero-downtime hotswap is not yet supported on Windows. Use `urnet-tools restart` to apply updates (there will be a brief restart gap)")
}

// pidIsAlive is unreachable in practice on Windows: triggerHotSwap always
// errors here, so update.go's rollback path (which calls this) never
// executes on this platform. Kept trivially correct for completeness.
func pidIsAlive(pid int) bool {
	return false
}
