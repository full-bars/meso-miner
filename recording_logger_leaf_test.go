package connect

// Full-capture recording logger for the ported udp-candidates leaf tests.
// Ported from upstream ip_remote_multi_client_observability_test.go at
// 20b29f79, renamed because the fork already has a recordingLogger
// (transfer_receiveack_desync_test.go, Errorf-only). Merge when the upstream
// test helpers land.

import (
	"fmt"
	"strings"
	"sync"
)

type recordingLoggerFull struct {
	mutex sync.Mutex
	out   []string
}

func newRecordingLogger() *recordingLoggerFull {
	return &recordingLoggerFull{}
}

// record takes the args as a plain slice rather than as a variadic forward.
// That is deliberate: a method with the shape `Infof(format string, args ...any)`
// that forwards straight into fmt.Sprintf teaches `go vet` to treat the whole
// `Logger` interface as printf-like, which then reports pre-existing format
// mismatches at unrelated call sites across the package.
func (self *recordingLoggerFull) record(format string, args []any) {
	line := fmt.Sprintf(format, args...)
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.out = append(self.out, strings.TrimRight(line, "\n"))
}

func (self *recordingLoggerFull) lines() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.out...)
}

func (self *recordingLoggerFull) linesWith(substring string) []string {
	matched := []string{}
	for _, line := range self.lines() {
		if strings.Contains(line, substring) {
			matched = append(matched, line)
		}
	}
	return matched
}

func (self *recordingLoggerFull) reset() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.out = nil
}

func (self *recordingLoggerFull) Info(args ...any) {
	self.record("%s", []any{fmt.Sprint(args...)})
}

func (self *recordingLoggerFull) Infof(format string, args ...any) {
	self.record(format, args)
}

func (self *recordingLoggerFull) Warningf(format string, args ...any) {
	self.record(format, args)
}

func (self *recordingLoggerFull) Errorf(format string, args ...any) {
	self.record(format, args)
}

func (self *recordingLoggerFull) V(level int32) Verbose {
	return noopVerbose{}
}
