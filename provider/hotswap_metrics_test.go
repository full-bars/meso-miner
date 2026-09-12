package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadHotswapDeclinesFromDisk(t *testing.T) {
	// Save the original state dir file and restore after test.
	stateDir := mustStateDir()
	if stateDir == "" {
		t.Skip("mustStateDir returned empty")
	}
	origPath := filepath.Join(stateDir, ".hotswap_declines.json")
	origData, origErr := os.ReadFile(origPath)
	// Whether or not the original existed, restore it at the end.
	defer func() {
		if origErr != nil {
			os.Remove(origPath) // didn't exist before; clean up
		} else {
			os.WriteFile(origPath, origData, 0600) // restore
		}
	}()

	// Write a decline file the same way urnet-tools CLI would.
	counts := map[string]int64{"version_old": 3, "unit_not_notify": 1, "success": 5}
	data, _ := json.Marshal(struct {
		Counts map[string]int64 `json:"counts"`
	}{Counts: counts})
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(origPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	// Call the actual function and verify the result.
	result := readHotswapDeclinesFromDisk()
	if result == nil {
		t.Fatal("readHotswapDeclinesFromDisk returned nil for existing file")
	}
	if result["version_old"] != 3 {
		t.Errorf("version_old = %d, want 3", result["version_old"])
	}
	if result["unit_not_notify"] != 1 {
		t.Errorf("unit_not_notify = %d, want 1", result["unit_not_notify"])
	}
	if result["success"] != 5 {
		t.Errorf("success = %d, want 5", result["success"])
	}
}

func TestReadHotswapDeclinesFromDiskMissing(t *testing.T) {
	// Save the original state dir file and ensure it's absent during the test.
	stateDir := mustStateDir()
	if stateDir == "" {
		t.Skip("mustStateDir returned empty")
	}
	origPath := filepath.Join(stateDir, ".hotswap_declines.json")
	origData, origErr := os.ReadFile(origPath)
	defer func() {
		if origErr != nil {
			os.Remove(origPath) // didn't exist before; clean up
		} else {
			os.WriteFile(origPath, origData, 0600) // restore
		}
	}()

	// Remove the file so the function returns nil.
	os.Remove(origPath)

	result := readHotswapDeclinesFromDisk()
	if result != nil {
		t.Errorf("readHotswapDeclinesFromDisk with missing file returned %v, want nil", result)
	}
}
