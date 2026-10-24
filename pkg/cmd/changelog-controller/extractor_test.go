package changelog_controller

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"github.com/openshift-eng/changelog-controller/pkg/storage/filesystem"
)

func TestReleaseExtractor_GetSetCache(t *testing.T) {
	extractor := NewReleaseExtractor(nil, 0)
	ctx := context.Background()

	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:test"

	// Get on empty cache
	snap, found := extractor.Get(ctx, pullSpec)
	if found {
		t.Error("Get() found = true, want false for empty cache")
	}
	if snap != nil {
		t.Error("Get() returned non-nil snapshot")
	}

	// Manually populate cache to test Get without running oc
	extractor.cache[pullSpec] = &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"test": true}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-427.el9"},
		},
		ExtractedAt: time.Now(),
	}

	snap, found = extractor.Get(ctx, pullSpec)
	if !found {
		t.Fatal("Get() found = false after cache population")
	}
	if snap.PullSpec != pullSpec {
		t.Errorf("Get() pullSpec = %q, want %q", snap.PullSpec, pullSpec)
	}
}

func TestReleaseExtractor_GetFromStorage(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-extractor-storage")
	defer os.RemoveAll(tempDir)

	ss, err := filesystem.NewSnapshotStorage(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSnapshotStorage() error = %v", err)
	}
	defer ss.Close()

	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:stored"

	// Write directly to storage (bypassing extractor)
	snapshot := &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"stored": true}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{"cri-o": "1.30.0-1.el9"},
		},
		ExtractedAt: time.Now(),
	}
	if err := ss.Set(ctx, pullSpec, snapshot); err != nil {
		t.Fatalf("storage Set() error = %v", err)
	}

	// Create extractor backed by this storage
	extractor := NewReleaseExtractor(ss, 0)

	// Get should find it in storage and populate the in-memory cache
	snap, found := extractor.Get(ctx, pullSpec)
	if !found {
		t.Fatal("Get() found = false, want true from storage backend")
	}
	if snap.PullSpec != pullSpec {
		t.Errorf("Get() pullSpec = %q, want %q", snap.PullSpec, pullSpec)
	}

	// Verify it was promoted to in-memory cache
	extractor.mu.RLock()
	_, inMemory := extractor.cache[pullSpec]
	extractor.mu.RUnlock()
	if !inMemory {
		t.Error("snapshot not promoted to in-memory cache after storage hit")
	}
}

func TestReleaseExtractor_WarmCache(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-extractor-warm")
	defer os.RemoveAll(tempDir)

	ss, err := filesystem.NewSnapshotStorage(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSnapshotStorage() error = %v", err)
	}
	defer ss.Close()

	specs := []string{
		"registry.ci.openshift.org/ocp/release@sha256:warm1",
		"registry.ci.openshift.org/ocp/release@sha256:warm2",
	}
	for _, spec := range specs {
		snap := &storage.ReleaseSnapshot{
			PullSpec:    spec,
			Manifest:    json.RawMessage(`{}`),
			ExtractedAt: time.Now(),
		}
		if err := ss.Set(ctx, spec, snap); err != nil {
			t.Fatalf("storage Set() error = %v", err)
		}
	}

	extractor := NewReleaseExtractor(ss, 0)

	if err := extractor.WarmCache(ctx); err != nil {
		t.Fatalf("WarmCache() error = %v", err)
	}

	extractor.mu.RLock()
	cacheLen := len(extractor.cache)
	extractor.mu.RUnlock()

	if cacheLen != len(specs) {
		t.Errorf("cache has %d entries after warm, want %d", cacheLen, len(specs))
	}

	for _, spec := range specs {
		snap, found := extractor.Get(ctx, spec)
		if !found {
			t.Errorf("Get(%s) found = false after warm", spec)
		}
		if snap.PullSpec != spec {
			t.Errorf("Get(%s) pullSpec = %q", spec, snap.PullSpec)
		}
	}
}

func TestReleaseExtractor_WarmCacheNilStorage(t *testing.T) {
	extractor := NewReleaseExtractor(nil, 0)
	if err := extractor.WarmCache(context.Background()); err != nil {
		t.Errorf("WarmCache() with nil storage should not error, got %v", err)
	}
}

func TestReleaseExtractor_ExtractSkipsWhenCachedWithRPMList(t *testing.T) {
	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:already-cached"

	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache[pullSpec] = &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"cached": true}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-427.el9"},
		},
		ExtractedAt: time.Now(),
	}

	// Extract should return nil (no-op) because snapshot already has RPMList.
	// If oc were actually invoked it would fail since the pullSpec is fake.
	err := extractor.Extract(context.Background(), pullSpec)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil for cached snapshot with RPMList", err)
	}

	// Verify cache is unchanged
	snap, found := extractor.Get(context.Background(), pullSpec)
	if !found {
		t.Fatal("Get() found = false after Extract no-op")
	}
	if snap.RPMList.Packages["kernel"] != "5.14.0-427.el9" {
		t.Errorf("kernel version changed unexpectedly: %q", snap.RPMList.Packages["kernel"])
	}
}

func TestReleaseExtractor_ExtractDoesNotSkipWhenRPMListNil(t *testing.T) {
	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:needs-rpm"

	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache[pullSpec] = &storage.ReleaseSnapshot{
		PullSpec:    pullSpec,
		Manifest:    json.RawMessage(`{"partial": true}`),
		RPMList:     nil, // manifest was extracted, RPMs were not
		ExtractedAt: time.Now(),
	}

	// Extract should NOT skip — it should attempt to run oc.
	// Since oc won't succeed with a fake pullSpec, we expect an error,
	// which proves the short-circuit was correctly bypassed.
	err := extractor.Extract(context.Background(), pullSpec)
	if err == nil {
		t.Fatal("Extract() returned nil, expected error because oc cannot process a fake pullSpec")
	}
}

func TestReleaseExtractor_StoragePersistence(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-extractor-persist")
	defer os.RemoveAll(tempDir)

	ss, err := filesystem.NewSnapshotStorage(ctx, tempDir)
	if err != nil {
		t.Fatalf("NewSnapshotStorage() error = %v", err)
	}
	defer ss.Close()

	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:persist-test"

	// Directly populate cache and simulate what Extract does after oc succeeds
	extractor := NewReleaseExtractor(ss, 0)
	snapshot := &storage.ReleaseSnapshot{
		PullSpec: pullSpec,
		Manifest: json.RawMessage(`{"persisted": true}`),
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-427.el9"},
		},
		ExtractedAt: time.Now(),
	}

	// Manually store to both cache and storage (same as Extract does)
	extractor.mu.Lock()
	extractor.cache[pullSpec] = snapshot
	extractor.mu.Unlock()
	if err := ss.Set(ctx, pullSpec, snapshot); err != nil {
		t.Fatalf("storage Set() error = %v", err)
	}

	// Create a new extractor with the same storage but empty in-memory cache
	extractor2 := NewReleaseExtractor(ss, 0)

	// It should find the snapshot via storage backend
	snap, found := extractor2.Get(ctx, pullSpec)
	if !found {
		t.Fatal("Get() found = false, expected storage backend hit")
	}
	if snap.RPMList == nil {
		t.Fatal("snapshot.RPMList is nil after storage retrieval")
	}
	if snap.RPMList.Packages["kernel"] != "5.14.0-427.el9" {
		t.Errorf("kernel = %q, want %q", snap.RPMList.Packages["kernel"], "5.14.0-427.el9")
	}
}

func TestReleaseExtractor_GetMissOnStorageError(t *testing.T) {
	pullSpec := "registry.ci.openshift.org/ocp/release@sha256:missing"

	// Extractor with nil storage — should handle gracefully
	extractor := NewReleaseExtractor(nil, 0)

	snap, found := extractor.Get(context.Background(), pullSpec)
	if found {
		t.Error("Get() found = true, want false for uncached pullSpec with nil storage")
	}
	if snap != nil {
		t.Error("Get() returned non-nil snapshot for miss")
	}
}

func TestDetectMachineOSTag(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "modern OCP 4.14-4.20 with rhel-coreos",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"rhel-coreos"},
				{"name":"rhel-coreos-extensions"},
				{"name":"cluster-kube-apiserver-operator"}
			]}}}`,
			want: "rhel-coreos",
		},
		{
			name: "multi-stream OCP 4.21+ with rhel-coreos and rhel-coreos-10",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"rhel-coreos"},
				{"name":"rhel-coreos-extensions"},
				{"name":"rhel-coreos-10"},
				{"name":"rhel-coreos-10-extensions"},
				{"name":"etcd"}
			]}}}`,
			want: "rhel-coreos",
		},
		{
			name: "OKD with stream-coreos",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"stream-coreos"},
				{"name":"stream-coreos-extensions"},
				{"name":"cluster-version-operator"}
			]}}}`,
			want: "stream-coreos",
		},
		{
			name: "old OCP 4.13 and earlier with machine-os-content",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"machine-os-content"},
				{"name":"cluster-kube-apiserver-operator"}
			]}}}`,
			want: "machine-os-content",
		},
		{
			name: "no machine-OS tags at all",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"etcd"},
				{"name":"kube-apiserver"}
			]}}}`,
			want: "",
		},
		{
			name:     "invalid JSON",
			manifest: `not valid json`,
			want:     "",
		},
		{
			name:     "empty manifest",
			manifest: `{}`,
			want:     "",
		},
		{
			name: "extensions tag without matching base",
			manifest: `{"references":{"spec":{"tags":[
				{"name":"rhel-coreos-extensions"},
				{"name":"etcd"}
			]}}}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectMachineOSTag([]byte(tt.manifest))
			if got != tt.want {
				t.Errorf("detectMachineOSTag() = %q, want %q", got, tt.want)
			}
		})
	}
}
