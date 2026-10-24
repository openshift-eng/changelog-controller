package changelog_controller

import (
	"context"
	"errors"
	"testing"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
)

// warmingTestStorage is a ChangelogStorage/ChangelogLister that records
// writes so tests can assert whether cache warming re-persisted entries.
type warmingTestStorage struct {
	entries  []storage.ChangelogEntry
	listErr  error
	setCalls int
}

func (s *warmingTestStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
	return "", false, nil
}

func (s *warmingTestStorage) Set(ctx context.Context, from, to, changelog string) error {
	s.setCalls++
	return nil
}

func (s *warmingTestStorage) Close() error {
	return nil
}

func (s *warmingTestStorage) List(ctx context.Context) ([]storage.ChangelogEntry, error) {
	return s.entries, s.listErr
}

func TestChangelogCache_SeedIsMemoryOnly(t *testing.T) {
	backend := &warmingTestStorage{}
	cache := NewChangelogCache(backend)

	cache.Seed("4.18.0", "4.18.1", "changelog content")

	if backend.setCalls != 0 {
		t.Errorf("Seed() wrote to storage backend %d time(s), want 0", backend.setCalls)
	}

	got, found := cache.Get(context.Background(), "4.18.0", "4.18.1")
	if !found {
		t.Fatal("Seed() did not populate the in-memory cache")
	}
	if got != "changelog content" {
		t.Errorf("Get() = %q, want %q", got, "changelog content")
	}

	stats := cache.Stats(context.Background())
	if stats.MemoryEntries != 1 {
		t.Errorf("MemoryEntries = %d, want 1", stats.MemoryEntries)
	}
	wantSize := int64(len("4.18.0->4.18.1") + len("changelog content"))
	if stats.MemorySizeBytes != wantSize {
		t.Errorf("MemorySizeBytes = %d, want %d", stats.MemorySizeBytes, wantSize)
	}

	// Seeding the same key again should replace the value without double
	// counting its size.
	cache.Seed("4.18.0", "4.18.1", "updated")
	stats = cache.Stats(context.Background())
	wantSize = int64(len("4.18.0->4.18.1") + len("updated"))
	if stats.MemorySizeBytes != wantSize {
		t.Errorf("MemorySizeBytes after re-seed = %d, want %d", stats.MemorySizeBytes, wantSize)
	}
}

func TestWarmCacheDoesNotWriteBackToStorage(t *testing.T) {
	backend := &warmingTestStorage{
		entries: []storage.ChangelogEntry{
			{FromVersion: "4.18.0", ToVersion: "4.18.1", Changelog: "first"},
			{FromVersion: "4.18.1", ToVersion: "4.18.2", Changelog: "second"},
		},
	}
	cache := NewChangelogCache(backend)
	o := &Options{cache: cache}

	if err := o.warmCache(context.Background(), []storage.ChangelogStorage{backend}); err != nil {
		t.Fatalf("warmCache() error = %v", err)
	}

	if backend.setCalls != 0 {
		t.Errorf("warmCache() wrote %d entries back to storage, want 0", backend.setCalls)
	}

	for _, entry := range backend.entries {
		got, found := cache.Get(context.Background(), entry.FromVersion, entry.ToVersion)
		if !found {
			t.Errorf("cache.Get(%s, %s) not found after warming", entry.FromVersion, entry.ToVersion)
			continue
		}
		if got != entry.Changelog {
			t.Errorf("cache.Get(%s, %s) = %q, want %q", entry.FromVersion, entry.ToVersion, got, entry.Changelog)
		}
	}
}

func TestWarmCacheContinuesAfterListError(t *testing.T) {
	broken := &warmingTestStorage{listErr: errors.New("list failed")}
	healthy := &warmingTestStorage{
		entries: []storage.ChangelogEntry{
			{FromVersion: "4.19.0", ToVersion: "4.19.1", Changelog: "content"},
		},
	}
	cache := NewChangelogCache(broken, healthy)
	o := &Options{cache: cache}

	err := o.warmCache(context.Background(), []storage.ChangelogStorage{broken, healthy})
	if err == nil {
		t.Fatal("warmCache() error = nil, want an error for the failing backend")
	}

	if _, found := cache.Get(context.Background(), "4.19.0", "4.19.1"); !found {
		t.Error("warmCache() did not load entries from the healthy backend")
	}
	if healthy.setCalls != 0 {
		t.Errorf("warmCache() wrote %d entries back to storage, want 0", healthy.setCalls)
	}
}
