package main

import (
	"testing"

	"github.com/urnetwork/connect/internal/testsocket"
)

func shortSocketDir(t *testing.T) string {
	t.Helper()
	return testsocket.Dir(t)
}
