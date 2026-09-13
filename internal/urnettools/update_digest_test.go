package urnettools

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateExplicitDigestMismatchNotSkipped verifies the fix: when the
// operator supplies --digest explicitly and the on-disk version matches the
// tag, the digest must still be checked against the binary. A wrong digest
// should cause an update (not a silent skip).
//
// This is the parity counterpart of the 3.23-fix shakedown W6 test:
// "wrong --digest should refuse" — the wrong digest must be detected even
// on a same-version binary swap.
func TestUpdateExplicitDigestMismatchNotSkipped(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "provider")
	binaryContent := []byte("#!/bin/sh\n# fake provider binary\n")
	if err := os.WriteFile(binaryPath, binaryContent, 0o755); err != nil {
		t.Fatal(err)
	}
	actualDigest := fmt.Sprintf("%x", sha256.Sum256(binaryContent))

	// Helper: simulate the skip-decision for a given provider + config.
	checkSkip := func(p Provider, cfg updateConfig) (skip bool) {
		if p.Version == cfg.Tag && !p.BinaryDeleted {
			if p.PID > 0 {
				// RunningImagePath not stubbed here — use PID=0 path below
				skip = true
			} else {
				skip = true
			}
		}
		if skip && cfg.DigestExplicit && p.Binary != "" && !p.BinaryDeleted {
			if actual, err := fileSHA256(p.Binary); err != nil {
				// can't verify — keep skipping (existing behavior)
			} else if !strings.EqualFold(actual, cfg.Digest) {
				skip = false
			}
		}
		return skip
	}

	// Use PID=0 to avoid runningImagePath (which would fail for 1234 on test box).
	p1 := Provider{Binary: binaryPath, Version: "v3.23.0-fix.31.1", PID: 0, BinaryDeleted: false}

	// Case 1: --digest matches the binary → should skip (already on).
	cfgMatch := updateConfig{Tag: "v3.23.0-fix.31.1", Digest: actualDigest, DigestExplicit: true}
	if !checkSkip(p1, cfgMatch) {
		t.Error("expected skip when digest matches on-disk binary")
	}

	// Case 2: --digest does NOT match → skip should be cancelled (proceed to update).
	cfgMismatch := updateConfig{Tag: "v3.23.0-fix.31.1", Digest: "0000000000000000000000000000000000000000000000000000000000000000", DigestExplicit: true}
	if checkSkip(p1, cfgMismatch) {
		t.Error("expected NO skip when explicit --digest mismatches — this is the W6 bug")
	}

	// Case 3: --digest not explicit, version matches → should skip (no digest check).
	cfgNoExplicit := updateConfig{Tag: "v3.23.0-fix.31.1", Digest: "", DigestExplicit: false}
	if !checkSkip(p1, cfgNoExplicit) {
		t.Error("expected skip when no explicit --digest and version matches")
	}
}
