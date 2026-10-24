package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
)

func TestSnapshotStorage_GetSet(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-snapshots")
	defer os.RemoveAll(tempDir)

	ss, err := NewSnapshotStorage(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSnapshotStorage() error = %v", err)
	}
	defer ss.Close()

	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:abc123"

	// Get on non-existent snapshot
	snap, found, err := ss.Get(ctx, pullSpec)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if found {
		t.Error("Get() found = true, want false for non-existent snapshot")
	}
	if snap != nil {
		t.Error("Get() returned non-nil snapshot for non-existent entry")
	}

	// Set a snapshot
	manifest := json.RawMessage(`{"references":{"spec":{"tags":[{"name":"test"}]}}}`)
	rpmPkgs := map[string]string{"kernel": "5.14.0-427.el9", "cri-o": "1.30.0-1.el9"}
	now := time.Now().Truncate(time.Second)
	snapshot := &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: manifest,
		RPMList: &storage.RpmList{
			Packages: rpmPkgs,
		},
		ExtractedAt: now,
	}

	if err := ss.Set(ctx, pullSpec, snapshot); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Get after Set
	snap, found, err = ss.Get(ctx, pullSpec)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("Get() found = false, want true after Set")
	}
	if snap.PullSpec != pullSpec {
		t.Errorf("Get() pullSpec = %q, want %q", snap.PullSpec, pullSpec)
	}
	if string(snap.Manifest) != string(manifest) {
		t.Errorf("Get() manifest mismatch")
	}
	if snap.RPMList == nil {
		t.Fatal("Get() RPMList is nil")
	}
	if len(snap.RPMList.Packages) != len(rpmPkgs) {
		t.Errorf("Get() RPMList.Packages has %d entries, want %d", len(snap.RPMList.Packages), len(rpmPkgs))
	}
	for pkg, ver := range rpmPkgs {
		if snap.RPMList.Packages[pkg] != ver {
			t.Errorf("Get() RPMList.Packages[%q] = %q, want %q", pkg, snap.RPMList.Packages[pkg], ver)
		}
	}
}

func TestSnapshotStorage_List(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-snapshots-list")
	defer os.RemoveAll(tempDir)

	ss, err := NewSnapshotStorage(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSnapshotStorage() error = %v", err)
	}
	defer ss.Close()

	specs := []string{
		"registry.ci.openshift.org/ocp/release@sha256:aaa",
		"registry.ci.openshift.org/ocp/release@sha256:bbb",
	}

	for _, spec := range specs {
		snap := &storage.ReleaseSnapshot{
			PullSpec:    spec,
			Manifest:    json.RawMessage(`{}`),
			ExtractedAt: time.Now(),
		}
		if err := ss.Set(ctx, spec, snap); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}

	listed, err := ss.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != len(specs) {
		t.Errorf("List() returned %d items, want %d", len(listed), len(specs))
	}
}

func TestNewSnapshotStorage_MissingBaseDir(t *testing.T) {
	ctx := context.Background()
	_, err := NewSnapshotStorage(ctx, "")
	if err == nil {
		t.Error("NewSnapshotStorage() expected error for empty baseDir")
	}
}
