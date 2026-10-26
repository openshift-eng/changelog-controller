package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFilesystemStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				BaseDir: filepath.Join(os.TempDir(), "test-changelogs"),
			},
			wantErr: false,
		},
		{
			name:    "missing base dir",
			cfg:     Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			storage, err := NewFilesystemStorage(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFilesystemStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && storage == nil {
				t.Error("NewFilesystemStorage() returned nil storage")
			}
			if storage != nil {
				_ = storage.Close()
				// Clean up
				if tt.cfg.BaseDir != "" {
					_ = os.RemoveAll(tt.cfg.BaseDir)
				}
			}
		})
	}
}

func TestFilesystemStorage_GetSet(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-changelog-storage")
	defer func() { _ = os.RemoveAll(tempDir) }()

	storage, err := NewFilesystemStorage(ctx, Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Test Get on non-existent changelog
	changelog, found, err := storage.Get(ctx, "1.0.0", "2.0.0")
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
	testChangelog := "This is a test changelog\nWith multiple lines\n- Feature 1\n- Feature 2"
	err = storage.Set(ctx, "1.0.0", "2.0.0", testChangelog)
	if err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Test Get after Set
	changelog, found, err = storage.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true after Set")
	}
	if changelog != testChangelog {
		t.Errorf("Get() changelog = %q, want %q", changelog, testChangelog)
	}

	// Test Update (Set with same from/to should overwrite)
	updatedChangelog := "This is an updated changelog"
	err = storage.Set(ctx, "1.0.0", "2.0.0", updatedChangelog)
	if err != nil {
		t.Errorf("Set() update error = %v", err)
	}

	changelog, found, err = storage.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true after update")
	}
	if changelog != updatedChangelog {
		t.Errorf("Get() changelog = %q, want %q", changelog, updatedChangelog)
	}

	// Test multiple entries
	err = storage.Set(ctx, "2.0.0", "3.0.0", "Another changelog")
	if err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Ensure first entry is still there
	changelog, found, err = storage.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true")
	}
	if changelog != updatedChangelog {
		t.Errorf("Get() changelog = %q, want %q", changelog, updatedChangelog)
	}

	// Check second entry
	changelog, found, err = storage.Get(ctx, "2.0.0", "3.0.0")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true")
	}
	if changelog != "Another changelog" {
		t.Errorf("Get() changelog = %q, want %q", changelog, "Another changelog")
	}
}

func TestFilesystemStorage_List(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-changelog-list")
	defer func() { _ = os.RemoveAll(tempDir) }()

	storage, err := NewFilesystemStorage(ctx, Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Add some changelogs
	changelogs := map[string]string{
		"1.0.0->2.0.0": "Changelog 1",
		"2.0.0->3.0.0": "Changelog 2",
		"3.0.0->4.0.0": "Changelog 3",
	}

	for key, content := range changelogs {
		parts := splitKey(key)
		if len(parts) != 2 {
			t.Fatalf("Invalid key format: %s", key)
		}
		err = storage.Set(ctx, parts[0], parts[1], content)
		if err != nil {
			t.Errorf("Set() error = %v", err)
		}
	}

	// List all changelogs
	list, err := storage.List(ctx)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}

	if len(list) != len(changelogs) {
		t.Errorf("List() returned %d items, want %d", len(list), len(changelogs))
	}

	// Verify entries contain the expected data
	foundEntries := make(map[string]bool)
	for _, entry := range list {
		key := entry.FromVersion + "->" + entry.ToVersion
		foundEntries[key] = true

		expectedContent, exists := changelogs[key]
		if !exists {
			t.Errorf("Unexpected entry: %s", key)
			continue
		}

		if entry.Changelog != expectedContent {
			t.Errorf("Entry %s has changelog %q, want %q", key, entry.Changelog, expectedContent)
		}
	}

	// Verify all expected entries were found
	for key := range changelogs {
		if !foundEntries[key] {
			t.Errorf("Expected entry not found: %s", key)
		}
	}
}

func TestFilesystemStorage_Close(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-changelog-close")
	defer func() { _ = os.RemoveAll(tempDir) }()

	storage, err := NewFilesystemStorage(ctx, Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	err = storage.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should still work (filesystem doesn't hold resources)
	_, _, err = storage.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Logf("Get() after Close() error (expected): %v", err)
	}
}

func TestFilesystemStorage_FilePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-changelog-paths")
	defer func() { _ = os.RemoveAll(tempDir) }()

	storage, err := NewFilesystemStorage(ctx, Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Test that different version pairs produce different file paths
	path1 := storage.getFilePath("1.0.0", "2.0.0")
	path2 := storage.getFilePath("2.0.0", "3.0.0")
	path3 := storage.getFilePath("1.0.0", "2.0.0") // Same as path1

	if path1 == path2 {
		t.Error("Different version pairs should produce different paths")
	}

	if path1 != path3 {
		t.Error("Same version pair should produce same path")
	}

	// Test that paths are within base directory
	if !filepath.IsAbs(path1) {
		t.Error("File path should be absolute")
	}

	relPath, err := filepath.Rel(tempDir, path1)
	if err != nil {
		t.Errorf("Failed to get relative path: %v", err)
	}

	if filepath.Dir(relPath) != "." {
		t.Error("File should be directly in base directory")
	}
}

func TestFilesystemStorage_Count(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-changelog-count")
	defer func() { _ = os.RemoveAll(tempDir) }()

	storage, err := NewFilesystemStorage(ctx, Config{BaseDir: tempDir})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	if err := storage.Set(ctx, "1.0.0", "2.0.0", "first"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := storage.Set(ctx, "2.0.0", "3.0.0", "second"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "orphan.txt"), []byte("no metadata"), 0644); err != nil {
		t.Fatalf("Failed to write orphan: %v", err)
	}

	count, err := storage.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 2 {
		t.Errorf("Count() = %d, want 2", count)
	}
}

// Helper function to split "from->to" format
func splitKey(key string) []string {
	parts := make([]string, 2)
	idx := 0
	for i, c := range key {
		if c == '-' && i+1 < len(key) && key[i+1] == '>' {
			parts[0] = key[:i]
			parts[1] = key[i+2:]
			idx++
			break
		}
	}
	if idx == 0 {
		return nil
	}
	return parts
}
