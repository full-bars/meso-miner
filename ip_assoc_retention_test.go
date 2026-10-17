package connect

import (
	"runtime"
	"testing"
	"time"
	"weak"
)

// Trimming the block history by slicing leaves the dropped blocks reachable
// through the backing array until it next grows. Clearing them first lets the
// association matrices they hold be collected as soon as they expire.
func TestIpAssocExpiredBlocksBecomeCollectable(t *testing.T) {
	settings := DefaultIpAssocSettings()
	settings.AssociationBlockCount = 2
	assoc := NewIpAssoc(t.Context(), settings)

	base := time.Now()
	step := settings.AssociationBlockDuration + time.Second

	assoc.stateLock.Lock()
	// grow the backing array first so later appends reuse it in place
	for i := 0; i < 4; i++ {
		assoc.blockWithLock(base.Add(time.Duration(i) * step))
	}
	expired := weak.Make(assoc.blockWithLock(base.Add(4 * step)))
	// two more blocks push the one above out of the visible history
	assoc.blockWithLock(base.Add(5 * step))
	assoc.blockWithLock(base.Add(6 * step))
	if len(assoc.blocks) != settings.AssociationBlockCount {
		assoc.stateLock.Unlock()
		t.Fatalf("history must stay at %d blocks, got %d", settings.AssociationBlockCount, len(assoc.blocks))
	}
	assoc.stateLock.Unlock()

	runtime.GC()
	runtime.GC()
	if expired.Value() != nil {
		t.Fatal("an expired association block is still reachable through the slice's backing array")
	}
}
