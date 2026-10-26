package changelog_controller

import (
	"context"
	"testing"

	"os"
	"path/filepath"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"github.com/openshift-eng/changelog-controller/pkg/storage/filesystem"
)

func TestChangelogCache_Stats(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-cache-stats")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create filesystem storage backend
	fsStorage, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = fsStorage.Close() }()

	// Create cache with storage backend
	cache := NewChangelogCache(fsStorage)

	// Initial stats should be empty
	stats := cache.Stats(ctx)
	if stats.MemoryEntries != 0 {
		t.Errorf("Expected 0 memory entries, got %d", stats.MemoryEntries)
	}
	if stats.MemorySizeBytes != 0 {
		t.Errorf("Expected 0 bytes, got %d", stats.MemorySizeBytes)
	}
	if stats.Hits != 0 {
		t.Errorf("Expected 0 hits, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("Expected 0 misses, got %d", stats.Misses)
	}

	// Add some entries
	changelog1 := "Changelog 1"
	changelog2 := "Changelog 2"
	cache.Set(ctx, "1.0.0", "2.0.0", changelog1)
	cache.Set(ctx, "2.0.0", "3.0.0", changelog2)

	// Check memory entries and size
	stats = cache.Stats(ctx)
	if stats.MemoryEntries != 2 {
		t.Errorf("Expected 2 memory entries, got %d", stats.MemoryEntries)
	}

	// Calculate expected size: key1 + value1 + key2 + value2
	expectedSize := int64(len("1.0.0->2.0.0") + len(changelog1) + len("2.0.0->3.0.0") + len(changelog2))
	if stats.MemorySizeBytes != expectedSize {
		t.Errorf("Expected %d bytes, got %d", expectedSize, stats.MemorySizeBytes)
	}

	// Test cache hits
	_, found := cache.Get(ctx, "1.0.0", "2.0.0")
	if !found {
		t.Error("Expected to find entry in cache")
	}

	stats = cache.Stats(ctx)
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}

	// Test cache miss
	_, found = cache.Get(ctx, "3.0.0", "4.0.0")
	if found {
		t.Error("Expected cache miss")
	}

	stats = cache.Stats(ctx)
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}

	// Test hit rate calculation
	if stats.HitRate != 50.0 {
		t.Errorf("Expected 50%% hit rate, got %.2f%%", stats.HitRate)
	}

	// Test storage backend stats
	if len(stats.StorageBackends) != 1 {
		t.Errorf("Expected 1 storage backend, got %d", len(stats.StorageBackends))
	}

	if stats.StorageBackends[0].Type != "FilesystemStorage" {
		t.Errorf("Expected FilesystemStorage type, got %s", stats.StorageBackends[0].Type)
	}

	if stats.StorageBackends[0].Entries != 2 {
		t.Errorf("Expected 2 entries in storage backend, got %d", stats.StorageBackends[0].Entries)
	}
}

func TestChangelogCache_HitRateCalculation(t *testing.T) {
	ctx := context.Background()
	cache := NewChangelogCache()

	// Initial hit rate should be 0
	stats := cache.Stats(ctx)
	if stats.HitRate != 0.0 {
		t.Errorf("Expected 0%% hit rate with no requests, got %.2f%%", stats.HitRate)
	}

	// Add an entry and access it multiple times
	cache.Set(ctx, "1.0.0", "2.0.0", "test")

	cache.Get(ctx, "1.0.0", "2.0.0") // hit
	cache.Get(ctx, "1.0.0", "2.0.0") // hit
	cache.Get(ctx, "1.0.0", "2.0.0") // hit
	cache.Get(ctx, "2.0.0", "3.0.0") // miss

	stats = cache.Stats(ctx)
	expectedHitRate := 75.0 // 3 hits out of 4 requests
	if stats.HitRate != expectedHitRate {
		t.Errorf("Expected %.2f%% hit rate, got %.2f%%", expectedHitRate, stats.HitRate)
	}
}

func TestChangelogCache_StorageFallback(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-cache-fallback")
	defer func() { _ = os.RemoveAll(tempDir) }()

	fsStorage, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = fsStorage.Close() }()

	// Write directly to storage backend, bypassing in-memory cache
	if err := fsStorage.Set(ctx, "from-spec", "to-spec", "changelog from storage"); err != nil {
		t.Fatalf("storage Set() error = %v", err)
	}

	// Create cache backed by this storage
	cache := NewChangelogCache(fsStorage)

	// In-memory cache is empty, so this should fall back to storage
	result, found := cache.Get(ctx, "from-spec", "to-spec")
	if !found {
		t.Fatal("Get() found = false, expected storage fallback")
	}
	if result != "changelog from storage" {
		t.Errorf("Get() = %q, want %q", result, "changelog from storage")
	}

	// Verify the entry was promoted to in-memory cache
	result2, found := cache.Get(ctx, "from-spec", "to-spec")
	if !found {
		t.Fatal("second Get() found = false after promotion")
	}
	if result2 != "changelog from storage" {
		t.Errorf("second Get() = %q, want %q", result2, "changelog from storage")
	}

	// Verify stats reflect 2 hits (both from in-memory after promotion)
	stats := cache.Stats(ctx)
	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("Expected 0 misses, got %d", stats.Misses)
	}
}

func TestChangelogCache_MissWhenNotInStorageEither(t *testing.T) {
	ctx := context.Background()
	tempDir := filepath.Join(os.TempDir(), "test-cache-total-miss")
	defer func() { _ = os.RemoveAll(tempDir) }()

	fsStorage, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{
		BaseDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = fsStorage.Close() }()

	cache := NewChangelogCache(fsStorage)

	result, found := cache.Get(ctx, "nonexistent-from", "nonexistent-to")
	if found {
		t.Error("Get() found = true for nonexistent entry")
	}
	if result != "" {
		t.Errorf("Get() = %q, want empty string", result)
	}

	stats := cache.Stats(ctx)
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
}

func TestChangelogCache_MultipleBackends(t *testing.T) {
	ctx := context.Background()

	tempDir1 := filepath.Join(os.TempDir(), "test-cache-multi-1")
	tempDir2 := filepath.Join(os.TempDir(), "test-cache-multi-2")
	defer func() { _ = os.RemoveAll(tempDir1) }()
	defer func() { _ = os.RemoveAll(tempDir2) }()

	fs1, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{BaseDir: tempDir1})
	if err != nil {
		t.Fatalf("Failed to create storage 1: %v", err)
	}
	defer func() { _ = fs1.Close() }()

	fs2, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{BaseDir: tempDir2})
	if err != nil {
		t.Fatalf("Failed to create storage 2: %v", err)
	}
	defer func() { _ = fs2.Close() }()

	cache := NewChangelogCache(fs1, fs2)

	// Set should write to both backends
	cache.Set(ctx, "1.0.0", "2.0.0", "multi-backend changelog")

	// Verify both backends have the entry
	r1, found1, err := fs1.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Fatalf("fs1.Get() error = %v", err)
	}
	if !found1 || r1 != "multi-backend changelog" {
		t.Errorf("fs1 missing entry: found=%v, result=%q", found1, r1)
	}

	r2, found2, err := fs2.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Fatalf("fs2.Get() error = %v", err)
	}
	if !found2 || r2 != "multi-backend changelog" {
		t.Errorf("fs2 missing entry: found=%v, result=%q", found2, r2)
	}

	stats := cache.Stats(ctx)
	if len(stats.StorageBackends) != 2 {
		t.Errorf("Expected 2 storage backends, got %d", len(stats.StorageBackends))
	}
}

func TestChangelogCache_SizeTracking(t *testing.T) {
	ctx := context.Background()
	cache := NewChangelogCache()

	// Initial size should be 0
	stats := cache.Stats(ctx)
	if stats.MemorySizeBytes != 0 {
		t.Errorf("Expected 0 bytes initially, got %d", stats.MemorySizeBytes)
	}

	// Add an entry
	cache.Set(ctx, "1.0.0", "2.0.0", "Small changelog")
	stats = cache.Stats(ctx)

	initialSize := stats.MemorySizeBytes
	expectedInitialSize := int64(len("1.0.0->2.0.0") + len("Small changelog"))
	if initialSize != expectedInitialSize {
		t.Errorf("Expected %d bytes, got %d", expectedInitialSize, initialSize)
	}

	// Update the same entry with larger content
	cache.Set(ctx, "1.0.0", "2.0.0", "This is a much larger changelog with more content")
	stats = cache.Stats(ctx)

	newSize := stats.MemorySizeBytes
	expectedNewSize := int64(len("1.0.0->2.0.0") + len("This is a much larger changelog with more content"))
	if newSize != expectedNewSize {
		t.Errorf("Expected %d bytes after update, got %d", expectedNewSize, newSize)
	}

	// Verify size increased
	if newSize <= initialSize {
		t.Errorf("Expected size to increase after updating with larger content: initial=%d, new=%d", initialSize, newSize)
	}

	// Add another entry
	cache.Set(ctx, "2.0.0", "3.0.0", "Another changelog")
	stats = cache.Stats(ctx)

	finalSize := stats.MemorySizeBytes
	expectedFinalSize := int64(len("1.0.0->2.0.0") + len("This is a much larger changelog with more content") +
		len("2.0.0->3.0.0") + len("Another changelog"))
	if finalSize != expectedFinalSize {
		t.Errorf("Expected %d bytes with two entries, got %d", expectedFinalSize, finalSize)
	}

	// Verify we have 2 entries
	if stats.MemoryEntries != 2 {
		t.Errorf("Expected 2 entries, got %d", stats.MemoryEntries)
	}
}

type countingStorage struct {
	count  int
	listed bool
}

func (s *countingStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
	return "", false, nil
}

func (s *countingStorage) Set(ctx context.Context, from, to, changelog string) error {
	return nil
}

func (s *countingStorage) Close() error {
	return nil
}

func (s *countingStorage) List(ctx context.Context) ([]storage.ChangelogEntry, error) {
	s.listed = true
	return nil, nil
}

func (s *countingStorage) Count(ctx context.Context) (int, error) {
	return s.count, nil
}

func TestChangelogCache_StatsPrefersCounter(t *testing.T) {
	backend := &countingStorage{count: 7}
	stats := NewChangelogCache(backend).Stats(context.Background())

	if len(stats.StorageBackends) != 1 {
		t.Fatalf("Expected 1 storage backend, got %d", len(stats.StorageBackends))
	}
	if stats.StorageBackends[0].Entries != 7 {
		t.Errorf("Entries = %d, want 7", stats.StorageBackends[0].Entries)
	}
	if stats.StorageBackends[0].Error != "" {
		t.Errorf("Error = %q, want empty", stats.StorageBackends[0].Error)
	}
	if backend.listed {
		t.Error("Stats() called List() even though Count() is available")
	}
}
