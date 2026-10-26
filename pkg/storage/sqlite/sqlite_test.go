package sqlite

import (
	"context"
	"testing"
)

func TestNewSQLiteStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid in-memory config",
			cfg: Config{
				DatabasePath: ":memory:",
			},
			wantErr: false,
		},
		{
			name: "valid file config",
			cfg: Config{
				DatabasePath: "/tmp/test-sqlite-changelog.db",
			},
			wantErr: false,
		},
		{
			name:    "missing database path",
			cfg:     Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			storage, err := NewSQLiteStorage(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSQLiteStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if storage != nil {
				_ = storage.Close()
			}
		})
	}
}

// TestSQLiteStorage_GetSet tests the full lifecycle
func TestSQLiteStorage_GetSet(t *testing.T) {
	ctx := context.Background()
	storage, err := NewSQLiteStorage(ctx, Config{
		DatabasePath: ":memory:",
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
	testChangelog := "This is a test changelog"
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

	// Test Update (Set with same from/to should update)
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

func TestSQLiteStorage_Close(t *testing.T) {
	ctx := context.Background()
	storage, err := NewSQLiteStorage(ctx, Config{
		DatabasePath: ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	err = storage.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should fail
	_, _, err = storage.Get(ctx, "1.0.0", "2.0.0")
	if err == nil {
		t.Error("Get() after Close() should error")
	}
}

func TestSQLiteStorage_Count(t *testing.T) {
	ctx := context.Background()
	storage, err := NewSQLiteStorage(ctx, Config{DatabasePath: ":memory:"})
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

	count, err := storage.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 2 {
		t.Errorf("Count() = %d, want 2", count)
	}
}
