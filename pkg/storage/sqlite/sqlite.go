package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/openshift-eng/changelog-controller/pkg/storage"

	// SQLite driver
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage implements changelog storage using SQLite
type SQLiteStorage struct {
	db *sql.DB
}

// Config holds configuration for SQLite storage
type Config struct {
	// DatabasePath is the path to the SQLite database file
	// Use ":memory:" for in-memory database (testing only)
	DatabasePath string
}

var _ storage.ChangelogStorage = (*SQLiteStorage)(nil)
var _ storage.ChangelogLister = (*SQLiteStorage)(nil)
var _ storage.ChangelogCounter = (*SQLiteStorage)(nil)

// NewSQLiteStorage creates a new SQLite storage backend
func NewSQLiteStorage(ctx context.Context, cfg Config) (*SQLiteStorage, error) {
	if cfg.DatabasePath == "" {
		return nil, fmt.Errorf("databasePath is required")
	}

	db, err := sql.Open("sqlite3", cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	s := &SQLiteStorage{db: db}

	// Initialize schema
	if err := s.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return s, nil
}

// initSchema creates the changelogs table if it doesn't exist
func (s *SQLiteStorage) initSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS changelogs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_version TEXT NOT NULL,
		to_version TEXT NOT NULL,
		changelog TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(from_version, to_version)
	);
	CREATE INDEX IF NOT EXISTS idx_versions ON changelogs(from_version, to_version);
	`

	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Get retrieves a changelog from SQLite
func (s *SQLiteStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
	query := `SELECT changelog FROM changelogs WHERE from_version = ? AND to_version = ? LIMIT 1`

	var changelog string
	err := s.db.QueryRowContext(ctx, query, from, to).Scan(&changelog)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to query changelog: %w", err)
	}

	return changelog, true, nil
}

// Set stores a changelog in SQLite
func (s *SQLiteStorage) Set(ctx context.Context, from, to, changelog string) error {
	query := `
	INSERT INTO changelogs (from_version, to_version, changelog)
	VALUES (?, ?, ?)
	ON CONFLICT(from_version, to_version)
	DO UPDATE SET changelog = excluded.changelog, created_at = CURRENT_TIMESTAMP
	`

	_, err := s.db.ExecContext(ctx, query, from, to, changelog)
	if err != nil {
		return fmt.Errorf("failed to insert changelog: %w", err)
	}

	return nil
}

// Close closes the SQLite database connection
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// List returns all changelog entries stored in SQLite
// Implements the ChangelogLister interface for cache warming
func (s *SQLiteStorage) List(ctx context.Context) ([]storage.ChangelogEntry, error) {
	query := `SELECT from_version, to_version, changelog FROM changelogs ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query changelogs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []storage.ChangelogEntry
	for rows.Next() {
		var entry storage.ChangelogEntry
		if err := rows.Scan(&entry.FromVersion, &entry.ToVersion, &entry.Changelog); err != nil {
			return nil, fmt.Errorf("failed to scan changelog row: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating changelog rows: %w", err)
	}

	return entries, nil
}

// Count returns the number of changelog entries without fetching their contents
func (s *SQLiteStorage) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM changelogs`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count changelogs: %w", err)
	}
	return count, nil
}
