package filesystem

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
)

// FilesystemStorage implements changelog storage using the local filesystem
type FilesystemStorage struct {
	baseDir string
}

// Config holds configuration for filesystem storage
type Config struct {
	// BaseDir is the base directory where changelogs will be stored
	BaseDir string
}

var _ storage.ChangelogStorage = (*FilesystemStorage)(nil)
var _ storage.ChangelogLister = (*FilesystemStorage)(nil)
var _ storage.ChangelogCounter = (*FilesystemStorage)(nil)

// NewFilesystemStorage creates a new filesystem storage backend
func NewFilesystemStorage(ctx context.Context, cfg Config) (*FilesystemStorage, error) {
	if cfg.BaseDir == "" {
		return nil, fmt.Errorf("baseDir is required")
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(cfg.BaseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &FilesystemStorage{
		baseDir: cfg.BaseDir,
	}, nil
}

// getFilePath generates a unique file path for a changelog based on from and to versions
func (s *FilesystemStorage) getFilePath(from, to string) string {
	// Use a hash to create a safe filename
	key := fmt.Sprintf("%s->%s", from, to)
	hash := sha256.Sum256([]byte(key))
	filename := fmt.Sprintf("%x.txt", hash[:16])
	return filepath.Join(s.baseDir, filename)
}

// getMetadataPath returns the path to the metadata file for a changelog
func (s *FilesystemStorage) getMetadataPath(from, to string) string {
	key := fmt.Sprintf("%s->%s", from, to)
	hash := sha256.Sum256([]byte(key))
	filename := fmt.Sprintf("%x.meta", hash[:16])
	return filepath.Join(s.baseDir, filename)
}

// Get retrieves a changelog from the filesystem
func (s *FilesystemStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
	filePath := s.getFilePath(from, to)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read changelog file: %w", err)
	}

	return string(data), true, nil
}

// Set stores a changelog to the filesystem
func (s *FilesystemStorage) Set(ctx context.Context, from, to, changelog string) error {
	filePath := s.getFilePath(from, to)
	metaPath := s.getMetadataPath(from, to)

	// Write the changelog content
	if err := os.WriteFile(filePath, []byte(changelog), 0644); err != nil {
		return fmt.Errorf("failed to write changelog file: %w", err)
	}

	// Write metadata file with version info for debugging
	metadata := fmt.Sprintf("from: %s\nto: %s\n", from, to)
	if err := os.WriteFile(metaPath, []byte(metadata), 0644); err != nil {
		// Log but don't fail if metadata write fails
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// Close performs cleanup (currently no-op for filesystem storage)
func (s *FilesystemStorage) Close() error {
	// No resources to clean up for filesystem storage
	return nil
}

// List returns all changelog entries stored in the filesystem
// Implements the ChangelogLister interface for cache warming
func (s *FilesystemStorage) List(ctx context.Context) ([]storage.ChangelogEntry, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var changelogs []storage.ChangelogEntry
	for _, entry := range entries {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		baseName := strings.TrimSuffix(entry.Name(), ".txt")

		// Read the metadata to get version info
		metaPath := filepath.Join(s.baseDir, baseName+".meta")
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			continue // Skip if metadata is missing
		}

		// Parse metadata: "from: X.X.X\nto: Y.Y.Y\n"
		var fromVersion, toVersion string
		lines := strings.SplitSeq(string(metaData), "\n")
		for line := range lines {
			if after, ok := strings.CutPrefix(line, "from: "); ok {
				fromVersion = after
			} else if after, ok := strings.CutPrefix(line, "to: "); ok {
				toVersion = after
			}
		}

		if fromVersion == "" || toVersion == "" {
			continue // Skip if metadata is incomplete
		}

		// Read the changelog content
		changelogPath := filepath.Join(s.baseDir, baseName+".txt")
		changelogData, err := os.ReadFile(changelogPath)
		if err != nil {
			continue // Skip if changelog file is missing
		}

		changelogs = append(changelogs, storage.ChangelogEntry{
			FromVersion: fromVersion,
			ToVersion:   toVersion,
			Changelog:   string(changelogData),
		})
	}

	return changelogs, nil
}

// Count returns the number of valid changelog entries without reading changelog contents
func (s *FilesystemStorage) Count(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		metaData, err := os.ReadFile(filepath.Join(s.baseDir, strings.TrimSuffix(entry.Name(), ".txt")+".meta"))
		if err != nil {
			continue
		}

		var fromVersion, toVersion string
		for line := range strings.SplitSeq(string(metaData), "\n") {
			if after, ok := strings.CutPrefix(line, "from: "); ok {
				fromVersion = after
			} else if after, ok := strings.CutPrefix(line, "to: "); ok {
				toVersion = after
			}
		}

		if fromVersion != "" && toVersion != "" {
			count++
		}
	}

	return count, nil
}
