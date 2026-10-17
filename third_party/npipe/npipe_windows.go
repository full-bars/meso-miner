//go:build windows

// Package npipe is a minimal in-tree replacement for the archived
// gopkg.in/natefinch/npipe.v2 module, which the vendored substrate RPC client
// imports for its Windows IPC transport. Upstream npipe carries no license
// file, so this repository implements the two calls the RPC client makes
// (Listen, DialTimeout) directly on github.com/Microsoft/go-winio, which the
// module graph already carries for Windows builds.
package npipe

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// Listen creates a Windows named-pipe listener at endpoint.
func Listen(endpoint string) (net.Listener, error) {
	return winio.ListenPipe(endpoint, nil)
}

// DialTimeout connects to the Windows named pipe at endpoint. A nil timeout
// waits without a deadline, matching the upstream module's contract.
func DialTimeout(endpoint string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(endpoint, &timeout)
}
