package firestore

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNewFirestoreStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "missing project ID",
			cfg: Config{
				CollectionID: "test-collection",
				BucketName:   "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "missing collection ID",
			cfg: Config{
				ProjectID:  "test-project",
				BucketName: "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "missing bucket name",
			cfg: Config{
				ProjectID:    "test-project",
				CollectionID: "test-collection",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := NewFirestoreStorage(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFirestoreStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestObjectPath(t *testing.T) {
	path1 := objectPath("4.15.0", "4.16.0")
	path2 := objectPath("4.15.0", "4.16.0")
	path3 := objectPath("4.16.0", "4.17.0")
	reversed := objectPath("4.16.0", "4.15.0")

	if path1 != path2 {
		t.Errorf("objectPath() not deterministic: %q != %q", path1, path2)
	}
	if path1 == path3 {
		t.Error("objectPath() returned same path for different version pairs")
	}
	if path1 == reversed {
		t.Error("objectPath() returned same path for reversed version pair")
	}
	if !strings.HasPrefix(path1, "changelogs/") {
		t.Errorf("objectPath() missing prefix: %q", path1)
	}
}

func TestDocID(t *testing.T) {
	id1 := docID("4.15.0", "4.16.0")
	id2 := docID("4.15.0", "4.16.0")
	id3 := docID("4.16.0", "4.17.0")
	reversed := docID("4.16.0", "4.15.0")

	if id1 != id2 {
		t.Errorf("docID() not deterministic: %q != %q", id1, id2)
	}
	if id1 == id3 {
		t.Error("docID() returned same ID for different version pairs")
	}
	if id1 == reversed {
		t.Error("docID() returned same ID for reversed version pair")
	}
}

// TestFirestoreStorage_GetSet runs against real Firestore and GCS.
// Set FIRESTORE_EMULATOR_HOST for local emulator testing, or ensure
// GCP credentials are configured for live testing.
// Required env vars: FIRESTORE_TEST_PROJECT, FIRESTORE_TEST_BUCKET
func TestFirestoreStorage_GetSet(t *testing.T) {
	project := os.Getenv("FIRESTORE_TEST_PROJECT")
	bucket := os.Getenv("FIRESTORE_TEST_BUCKET")
	if project == "" || bucket == "" {
		t.Skip("Skipping integration test - FIRESTORE_TEST_PROJECT and FIRESTORE_TEST_BUCKET must be set")
	}

	ctx := context.Background()
	storage, err := NewFirestoreStorage(ctx, Config{
		ProjectID:    project,
		DatabaseID:   os.Getenv("FIRESTORE_TEST_DATABASE"),
		CollectionID: "test_changelogs",
		BucketName:   bucket,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	from, to := "test-1.0.0", "test-2.0.0"

	cleanup := func() {
		_ = storage.gcsClient.Bucket(bucket).Object(objectPath(from, to)).Delete(ctx)
		_, _ = storage.fsClient.Collection("test_changelogs").Doc(docID(from, to)).Delete(ctx)
	}

	// Remove any leftover data from previous runs
	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = storage.Close()
	})

	// Test Get on non-existent changelog
	changelog, found, err := storage.Get(ctx, from, to)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if found {
		t.Error("Get() found = true, want false for non-existent changelog")
	}
	if changelog != "" {
		t.Errorf("Get() changelog = %q, want empty string", changelog)
	}

	// Test Set
	testChangelog := `{"from":{"name":"test-1.0.0"},"to":{"name":"test-2.0.0"}}`
	err = storage.Set(ctx, from, to, testChangelog)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Test Get after Set
	changelog, found, err = storage.Get(ctx, from, to)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true after Set")
	}
	if changelog != testChangelog {
		t.Errorf("Get() changelog = %q, want %q", changelog, testChangelog)
	}

	// Test List
	entries, err := storage.List(ctx)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}
	found = false
	for _, entry := range entries {
		if entry.FromVersion == from && entry.ToVersion == to {
			found = true
			if entry.Changelog != testChangelog {
				t.Errorf("List() changelog = %q, want %q", entry.Changelog, testChangelog)
			}
		}
	}
	if !found {
		t.Error("List() did not return the entry stored by Set()")
	}
}
