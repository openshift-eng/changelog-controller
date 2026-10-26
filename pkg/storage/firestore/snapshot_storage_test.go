package firestore

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
)

func TestNewSnapshotStorage_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SnapshotConfig
		wantErr bool
	}{
		{
			name: "missing project ID",
			cfg: SnapshotConfig{
				CollectionID: "snapshots",
				BucketName:   "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "missing collection ID",
			cfg: SnapshotConfig{
				ProjectID:  "test-project",
				BucketName: "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "missing bucket name",
			cfg: SnapshotConfig{
				ProjectID:    "test-project",
				CollectionID: "snapshots",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSnapshotStorage(context.Background(), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSnapshotStorage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPullSpecHash(t *testing.T) {
	h1 := pullSpecHash("registry.ci.openshift.org/ocp/release:4.18.2-x86_64")
	h2 := pullSpecHash("registry.ci.openshift.org/ocp/release:4.18.2-x86_64")
	h3 := pullSpecHash("registry.ci.openshift.org/ocp/release:4.18.3-x86_64")

	if h1 != h2 {
		t.Errorf("pullSpecHash() not deterministic: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Error("pullSpecHash() returned same hash for different pull specs")
	}
	if len(h1) != 32 {
		t.Errorf("pullSpecHash() length = %d, want 32 hex chars", len(h1))
	}
}

func TestSnapshotObjectPath(t *testing.T) {
	path1 := snapshotObjectPath("registry.ci.openshift.org/ocp/release:4.18.2-x86_64")
	path2 := snapshotObjectPath("registry.ci.openshift.org/ocp/release:4.18.2-x86_64")
	path3 := snapshotObjectPath("registry.ci.openshift.org/ocp/release:4.18.3-x86_64")

	if path1 != path2 {
		t.Errorf("snapshotObjectPath() not deterministic: %q != %q", path1, path2)
	}
	if path1 == path3 {
		t.Error("snapshotObjectPath() returned same path for different pull specs")
	}
	if !strings.HasPrefix(path1, "snapshots/") {
		t.Errorf("snapshotObjectPath() missing prefix: %q", path1)
	}
	if !strings.HasSuffix(path1, ".json") {
		t.Errorf("snapshotObjectPath() missing suffix: %q", path1)
	}
}

// TestSnapshotStorage_GetSet runs against real Firestore and GCS.
// Required env vars: FIRESTORE_TEST_PROJECT, FIRESTORE_TEST_BUCKET
func TestSnapshotStorage_GetSet(t *testing.T) {
	project := os.Getenv("FIRESTORE_TEST_PROJECT")
	bucket := os.Getenv("FIRESTORE_TEST_BUCKET")
	if project == "" || bucket == "" {
		t.Skip("Skipping integration test - FIRESTORE_TEST_PROJECT and FIRESTORE_TEST_BUCKET must be set")
	}

	ctx := context.Background()
	ss, err := NewSnapshotStorage(ctx, SnapshotConfig{
		ProjectID:    project,
		DatabaseID:   os.Getenv("FIRESTORE_TEST_DATABASE"),
		CollectionID: "test_snapshots",
		BucketName:   bucket,
	})
	if err != nil {
		t.Fatalf("Failed to create snapshot storage: %v", err)
	}

	pullSpec := "test-snapshot-registry.ci.openshift.org/ocp/release@sha256:test123"

	cleanup := func() {
		_ = ss.gcsClient.Bucket(bucket).Object(snapshotObjectPath(pullSpec)).Delete(ctx)
		_, _ = ss.fsClient.Collection("test_snapshots").Doc(pullSpecHash(pullSpec)).Delete(ctx)
	}

	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = ss.Close()
	})

	// Get on non-existent snapshot
	snap, found, err := ss.Get(ctx, pullSpec)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if found {
		t.Error("Get() found = true, want false for non-existent snapshot")
	}
	if snap != nil {
		t.Error("Get() returned non-nil snapshot")
	}

	// Set a snapshot
	now := time.Now().Truncate(time.Second)
	snapshot := &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"references":{"spec":{"tags":[{"name":"test"}]}}}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{
				"kernel": "5.14.0-427.22.1.el9_4",
				"cri-o":  "1.30.0-1.el9",
			},
		},
		ExtractedAt: now,
	}

	if err := ss.Set(ctx, pullSpec, snapshot); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Get after Set
	snap, found, err = ss.Get(ctx, pullSpec)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("Get() found = false, want true after Set")
	}
	if snap.PullSpec != pullSpec {
		t.Errorf("Get() pullSpec = %q, want %q", snap.PullSpec, pullSpec)
	}
	if snap.RPMList == nil {
		t.Fatal("Get() RPMList is nil")
	}
	if snap.RPMList.Packages["kernel"] != "5.14.0-427.22.1.el9_4" {
		t.Errorf("Get() kernel = %q, want %q", snap.RPMList.Packages["kernel"], "5.14.0-427.22.1.el9_4")
	}
	if snap.RPMList.Packages["cri-o"] != "1.30.0-1.el9" {
		t.Errorf("Get() cri-o = %q, want %q", snap.RPMList.Packages["cri-o"], "1.30.0-1.el9")
	}

	// List
	listed, err := ss.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	found = false
	for _, s := range listed {
		if s.PullSpec == pullSpec {
			found = true
			if s.RPMList == nil {
				t.Error("Listed snapshot has nil RPMList")
			}
		}
	}
	if !found {
		t.Error("List() did not return the snapshot stored by Set()")
	}
}

// TestSnapshotStorage_SetOverwrite verifies that Set overwrites existing snapshots
func TestSnapshotStorage_SetOverwrite(t *testing.T) {
	project := os.Getenv("FIRESTORE_TEST_PROJECT")
	bucket := os.Getenv("FIRESTORE_TEST_BUCKET")
	if project == "" || bucket == "" {
		t.Skip("Skipping integration test - FIRESTORE_TEST_PROJECT and FIRESTORE_TEST_BUCKET must be set")
	}

	ctx := context.Background()
	ss, err := NewSnapshotStorage(ctx, SnapshotConfig{
		ProjectID:    project,
		DatabaseID:   os.Getenv("FIRESTORE_TEST_DATABASE"),
		CollectionID: "test_snapshots",
		BucketName:   bucket,
	})
	if err != nil {
		t.Fatalf("Failed to create snapshot storage: %v", err)
	}

	pullSpec := "test-overwrite-registry.ci.openshift.org/ocp/release@sha256:overwrite"

	cleanup := func() {
		_ = ss.gcsClient.Bucket(bucket).Object(snapshotObjectPath(pullSpec)).Delete(ctx)
		_, _ = ss.fsClient.Collection("test_snapshots").Doc(pullSpecHash(pullSpec)).Delete(ctx)
	}

	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = ss.Close()
	})

	// First Set: manifest only, no RPM data
	snap1 := &storage.ReleaseSnapshot{
		PullSpec:    pullSpec,
		Manifest:    json.RawMessage(`{"partial": true}`),
		RPMList:     nil,
		ExtractedAt: time.Now(),
	}
	if err := ss.Set(ctx, pullSpec, snap1); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Second Set: full snapshot with RPM data
	snap2 := &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"complete": true}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-427.el9"},
		},
		ExtractedAt: time.Now(),
	}
	if err := ss.Set(ctx, pullSpec, snap2); err != nil {
		t.Fatalf("Set() overwrite error = %v", err)
	}

	// Verify the overwrite took effect
	result, found, err := ss.Get(ctx, pullSpec)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("Get() found = false after overwrite")
	}
	if result.RPMList == nil {
		t.Fatal("Overwritten snapshot has nil RPMList")
	}
	if result.RPMList.Packages["kernel"] != "5.14.0-427.el9" {
		t.Errorf("kernel = %q, want %q", result.RPMList.Packages["kernel"], "5.14.0-427.el9")
	}
}
