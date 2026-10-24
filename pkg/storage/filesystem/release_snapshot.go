package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
)

// SnapshotStorage implements per-release snapshot storage using the local filesystem
type SnapshotStorage struct {
	baseDir string
}

var _ storage.ReleaseSnapshotStorage = (*SnapshotStorage)(nil)
var _ storage.ReleaseSnapshotLister = (*SnapshotStorage)(nil)

// NewSnapshotStorage creates a new filesystem snapshot storage backend
func NewSnapshotStorage(ctx context.Context, baseDir string) (*SnapshotStorage, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("baseDir is required")
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	return &SnapshotStorage{baseDir: baseDir}, nil
}

func (s *SnapshotStorage) filePath(pullSpec string) string {
	hash := sha256.Sum256([]byte(pullSpec))
	return filepath.Join(s.baseDir, fmt.Sprintf("%x.json", hash[:16]))
}

// Get retrieves a snapshot from the filesystem
func (s *SnapshotStorage) Get(ctx context.Context, pullSpec string) (*storage.ReleaseSnapshot, bool, error) {
	data, err := os.ReadFile(s.filePath(pullSpec))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	var snapshot storage.ReleaseSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, true, nil
}

// Set stores a snapshot to the filesystem
func (s *SnapshotStorage) Set(ctx context.Context, pullSpec string, snapshot *storage.ReleaseSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	if err := os.WriteFile(s.filePath(pullSpec), data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}

	return nil
}

// List returns all snapshots stored in the filesystem
func (s *SnapshotStorage) List(ctx context.Context) ([]*storage.ReleaseSnapshot, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot directory: %w", err)
	}

	var snapshots []*storage.ReleaseSnapshot
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if err != nil {
			continue
		}

		var snapshot storage.ReleaseSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			continue
		}

		snapshots = append(snapshots, &snapshot)
	}

	return snapshots, nil
}

// Close performs cleanup (no-op for filesystem storage)
func (s *SnapshotStorage) Close() error {
	return nil
}
