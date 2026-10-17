//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportantLogLocationPrefersStateDirWithRamFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, max := importantLogLocation()
	if path != filepath.Join(home, ".urnetwork", shmImportantLogDiskName) || max != shmImportantLogMaxSize {
		t.Errorf("got %q cap %d", path, max)
	}
	if 2*max != 20*1024*1024 {
		t.Errorf("disk cap = %d per file, want 20 MiB across the two files", max)
	}

	// no usable home: the RAM file and its small cap
	t.Setenv("HOME", "")
	path, max = importantLogLocation()
	if path != shmImportantLogPath || max != shmImportantLogMaxSizeRAM {
		t.Errorf("fallback got %q cap %d", path, max)
	}
}

func TestRotateImportantLogKeepsOnePreviousFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "important.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("old line one\nold line two\n")

	// under the cap: nothing moves
	if err := rotateImportantLog(&f, path, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Fatal("rotated below the cap")
	}

	// over the cap: the old file becomes .1 and writes go to a fresh file
	if err := rotateImportantLog(&f, path, 10); err != nil {
		t.Fatal(err)
	}
	f.WriteString("new line\n")
	f.Close()
	prev, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if string(prev) != "old line one\nold line two\n" || string(cur) != "new line\n" {
		t.Errorf("prev=%q cur=%q", prev, cur)
	}
}
