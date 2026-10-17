//go:build linux || darwin

package main

// The sn module opens its own native custody (chain journals) through the
// sn-internal durablepath, whose synthetic-host key is a different unexported
// type than this module's internal/durablepath. A host injected by
// internal/durablefixture therefore never reaches the module's own durable
// opens, and those fall back to the real kernel census.
//
// The fleet fixture consequently declares the real covering mount in
// owner-local scope when the host has one (approved filesystem type plus a
// resolvable /dev/disk/by-uuid device), so this module's and the module's own
// durable opens observe the same kernel facts. Hosts without one keep the
// synthetic host and skip the register paths that need the module journal
// (see requireRealDurableMount).

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/durablesys"
	"github.com/urnetwork/connect/durablevolume"
	"github.com/urnetwork/connect/internal/durablefixture"

	"golang.org/x/sys/unix"
)

// fleetDurableFixture mirrors durablefixture.Fixture with the real-mount
// declaration used by the fleet tests.
type fleetDurableFixture struct {
	Context   context.Context
	Reference durablevolume.Reference
	Roots     []string
	// RealMount is true when the declaration names the real covering mount, so
	// the sn module's own durablepath can also open the roots.
	RealMount bool
}

// requireRealDurableMount skips the module-journal register paths on hosts
// where no real approved durable mount is available.
func (self *fleetDurableFixture) requireRealDurableMount(t *testing.T) {
	t.Helper()
	if !self.RealMount {
		t.Skip("no real approved durable mount on this host; the sn module's chain journal needs real kernel facts")
	}
}

// newFleetDurableFixture prefers the real-mount declaration for preferredRoot
// and falls back to the synthetic host (this module only) otherwise.
func newFleetDurableFixture(t *testing.T, ctx context.Context, preferredRoot string) *fleetDurableFixture {
	t.Helper()
	if fixture, ok := newFleetRealDurableFixture(t, ctx, preferredRoot); ok {
		return fixture
	}
	if err := os.MkdirAll(preferredRoot, 0700); err != nil {
		t.Fatal(err)
	}
	f := durablefixture.NewOwnerLocal(t, ctx, preferredRoot)
	return &fleetDurableFixture{Context: f.Context, Reference: f.Reference, Roots: f.Roots}
}

// newFleetRealDurableFixture builds the owner-local declaration against the
// real mount census. It returns ok=false when the host has no approved mount
// under which a private root can live.
func newFleetRealDurableFixture(t *testing.T, ctx context.Context, preferredRoot string) (*fleetDurableFixture, bool) {
	t.Helper()
	census, ok := fleetRealMountCensus()
	if !ok {
		return nil, false
	}
	approved := func(fsType string) bool {
		return fsType == "ext4" || fsType == "xfs" || fsType == "btrfs"
	}
	var selected fleetRealMountEntry
	var root string
	var err error
	found := false
	// The provider TestMain patches HOME to a temp directory, so approved
	// bases must come from the real census: the caller's temp root first, then
	// the conventional persistent temp location.
	candidates := []string{preferredRoot, "/var/tmp"}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			continue
		}
		mount, present := fleetCoveringMount(census, candidate)
		if !present || !approved(mount.FsType) {
			continue
		}
		if err := os.MkdirAll(candidate, 0700); err != nil {
			continue
		}
		root, err = os.MkdirTemp(candidate, "durable-state-")
		if err != nil {
			continue
		}
		// No other mount may sit over the root, the marker or the lease.
		metadata := filepath.Join(filepath.Dir(root), ".fleet-durable-metadata")
		clean := true
		for _, other := range census {
			if other.Path == mount.Path || !fleetPathBeneath(mount.Path, other.Path) {
				continue
			}
			if fleetPathBeneath(other.Path, root) || fleetPathBeneath(other.Path, metadata) {
				clean = false
				break
			}
		}
		if !clean {
			_ = os.RemoveAll(root)
			continue
		}
		selected, found = mount, true
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		break
	}
	if !found {
		return nil, false
	}
	uuid, ok := fleetRealFilesystemUuid(selected.Major, selected.Minor)
	if !ok {
		return nil, false
	}
	// The marker and lease live beside the root: declaration validation
	// refuses identity or lease bytes inside a journal root.
	metadata, err := os.MkdirTemp(filepath.Dir(root), ".fleet-durable-metadata-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(metadata) })
	marker := filepath.Join(metadata, "identity")
	markerBytes := []byte("fleet-real-fixture-volume-generation\n")
	if err := os.WriteFile(marker, markerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	spec := durablevolume.VolumeSpec{MountPath: selected.Path, FilesystemUuid: uuid, FilesystemType: selected.FsType, MarkerPath: marker, MarkerSha256: durablefixture.Digest(markerBytes), MinAvailableBytes: 1024, MinAvailableInodes: 8}
	lease := filepath.Join(metadata, "root-0-lease")
	leaseBytes := []byte("fleet-real-fixture-root-lease:" + root + "\n")
	if err := os.WriteFile(lease, leaseBytes, 0600); err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, durablevolume.RootGenerationBytes)
	count, err := unix.Getxattr(root, durablevolume.RootGenerationAttribute, nonce)
	if errors.Is(err, durablesys.ErrNoAttribute) {
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		if err := unix.Setxattr(root, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
			t.Fatal(err)
		}
	} else if err != nil || count != len(nonce) {
		t.Fatalf("synthetic root generation: size=%d err=%v", count, err)
	}
	var rootStat unix.Stat_t
	if err := unix.Stat(root, &rootStat); err != nil {
		t.Fatal(err)
	}
	spec.StateRoots = append(spec.StateRoots, durablevolume.StateRootSpec{Path: root, LeasePath: lease, LeaseSha256: durablefixture.Digest(leaseBytes), RootInode: rootStat.Ino, GenerationSha256: durablefixture.Digest(nonce)})
	raw, err := json.Marshal(durablevolume.Config{Schema: durablevolume.OwnerLocalSchema, Volumes: []durablevolume.VolumeSpec{spec}})
	if err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: filepath.Join(metadata, "volumes.json"), Sha256: durablefixture.Digest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &fleetDurableFixture{Context: durablevolume.WithReference(ctx, reference), Reference: reference, Roots: []string{root}, RealMount: true}, true
}

// fleetPathBeneath reports whether child lives under parent (parent itself
// qualifies), on canonical absolute paths only. It mirrors the durable
// declaration's own beneath rule for the root mount.
func fleetPathBeneath(parent, child string) bool {
	if parent == string(filepath.Separator) {
		return filepath.IsAbs(child)
	}
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}

// fleetCoveringMount selects the longest real mount path containing path.
func fleetCoveringMount(census []fleetRealMountEntry, path string) (fleetRealMountEntry, bool) {
	var selected fleetRealMountEntry
	found := false
	for _, entry := range census {
		if !fleetPathBeneath(entry.Path, path) {
			continue
		}
		if !found || len(entry.Path) > len(selected.Path) {
			selected, found = entry, true
		}
	}
	return selected, found
}
