# Changelog Storage Backends

This package provides a pluggable storage interface for persisting generated changelogs.

## Architecture

The storage system uses a layered approach:

1. **In-Memory Cache** - Fast first-level cache for immediate responses
2. **Persistent Storage** - Pluggable backends for durable storage (Filesystem, SQLite, Firestore+GCS, BigQuery)

When a changelog is requested:
- Check in-memory cache first (fastest)
- On cache miss, query storage backends in order
- On storage hit, populate in-memory cache and return
- On complete miss, enqueue for generation

When a changelog is generated:
- Store in in-memory cache
- Store in all configured storage backends

## Interface

All storage backends implement the `ChangelogStorage` interface:

```go
type ChangelogStorage interface {
    // Get retrieves a changelog for the given version range
    Get(ctx context.Context, from, to string) (string, bool, error)
    
    // Set stores a changelog for the given version range
    Set(ctx context.Context, from, to, changelog string) error
    
    // Close cleanly shuts down the storage backend
    Close() error
}
```

## Available Backends

### Filesystem

No external dependencies required.

**Configuration**:
```go
storage, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{
    BaseDir: "/tmp/changelog-storage",
})
```

**Features**:
- Simple file-based storage
- No external dependencies
- Hash-based filenames for safety
- Metadata files for debugging
- Automatic directory creation
- Ideal for simple deployments or local development

**Storage Format**:
- Each changelog is stored as a text file
- Filename is SHA256 hash of "from->to" version pair
- Metadata file (`.meta`) stores version information
- Example: `abc123def456.txt` and `abc123def456.meta`

### SQLite

Requires `github.com/mattn/go-sqlite3`.

**Configuration**:
```go
storage, err := sqlite.NewSQLiteStorage(ctx, sqlite.Config{
    DatabasePath: "/var/lib/changelogs.db",
    // Or use ":memory:" for in-memory (testing only)
})
```

**Features**:
- Self-contained local database
- No external dependencies
- Automatic schema initialization
- UPSERT support for updates
- Ideal for development and small deployments

### Firestore + GCS

Requires `cloud.google.com/go/firestore` and `cloud.google.com/go/storage`.

Uses Firestore as an index and Google Cloud Storage (GCS) as the content store. Firestore documents hold version metadata and a reference to the GCS object containing the actual changelog content.

**Configuration**:
```go
storage, err := firestore.NewFirestoreStorage(ctx, firestore.Config{
    ProjectID:    "my-gcp-project",
    DatabaseID:   "changelog-controller",
    CollectionID: "changelogs",
    BucketName:   "my-changelog-bucket",
})
```

**Features**:
- Firestore acts as an index; changelog content is stored as GCS objects
- GCS objects stored under `changelogs/` with hash-based filenames
- Scalable for production deployments
- No document size constraints (content lives in GCS)

### BigQuery

Requires `cloud.google.com/go/bigquery`.

**Configuration**:
```go
storage, err := bigquery.NewBigQueryStorage(ctx, bigquery.Config{
    ProjectID: "my-gcp-project",
    DatasetID: "changelogs",
    TableID:   "release_changelogs",
})
```

**Table Schema**:
```sql
CREATE TABLE changelogs (
    from_version STRING NOT NULL,
    to_version STRING NOT NULL,
    changelog STRING NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Features**:
- Scalable cloud-based storage
- Automatic schema handling
- Parameterized queries for safety
- Ideal for production deployments

### ReleaseSnapshot Storage

The storage layer also supports per-release metadata via `ReleaseSnapshotStorage`:

```go
type ReleaseSnapshotStorage interface {
    GetSnapshot(ctx context.Context, pullSpec string) (*ReleaseSnapshot, bool, error)
    SetSnapshot(ctx context.Context, snap *ReleaseSnapshot) error
    ListSnapshots(ctx context.Context) ([]*ReleaseSnapshot, error)
}
```

`ReleaseSnapshot` stores the component manifest and RPM package list extracted from a release image. Currently implemented by the filesystem backend (`pkg/storage/filesystem/release_snapshot.go`), stored under `{base-dir}/snapshots/`.

## Adding a New Backend

1. Create a new package under `storage/` (e.g., `storage/postgres/`)

2. Implement the `ChangelogStorage` interface:

```go
package postgres

import (
    "context"
    "github.com/openshift-eng/changelog-controller/pkg/storage"
)

type PostgresStorage struct {
    // Your fields
}

var _ storage.ChangelogStorage = (*PostgresStorage)(nil)

func NewPostgresStorage(ctx context.Context, cfg Config) (*PostgresStorage, error) {
    // Initialize your storage
}

func (s *PostgresStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
    // Query implementation
}

func (s *PostgresStorage) Set(ctx context.Context, from, to, changelog string) error {
    // Insert/update implementation
}

func (s *PostgresStorage) Close() error {
    // Cleanup
}
```

3. Add tests in `<backend>_test.go`:

```go
func TestNewPostgresStorage(t *testing.T) {
    // Test initialization
}

func TestPostgresStorage_GetSet(t *testing.T) {
    // Test full lifecycle
}
```

4. Configure in `cmd.go`:

```go
postgresStorage, err := postgres.NewPostgresStorage(ctx, postgres.Config{
    ConnectionString: "postgres://...",
})
if err != nil {
    klog.Warningf("Failed to initialize Postgres storage: %v", err)
} else {
    storages = append(storages, postgresStorage)
    defer postgresStorage.Close()
}

o.cache = NewChangelogCache(storages...)
```

## Configuration

Storage backends are configured in `pkg/cmd/changelog-controller/cmd.go`. 

Example multi-backend setup:

```go
var storages []storage.ChangelogStorage

// Primary storage: BigQuery
bqStorage, err := bigquery.NewBigQueryStorage(ctx, bigquery.Config{
    ProjectID: "my-project",
    DatasetID: "changelogs",
    TableID:   "release_changelogs",
})
if err == nil {
    storages = append(storages, bqStorage)
    defer bqStorage.Close()
}

// Fallback storage: SQLite
sqliteStorage, err := sqlite.NewSQLiteStorage(ctx, sqlite.Config{
    DatabasePath: "/var/lib/changelogs.db",
})
if err == nil {
    storages = append(storages, sqliteStorage)
    defer sqliteStorage.Close()
}

cache := NewChangelogCache(storages...)
```

## Testing

Run all storage tests:
```bash
go test ./pkg/storage/... -v
```

Run specific backend tests:
```bash
go test ./pkg/storage/bigquery -v
go test ./pkg/storage/sqlite -v
go test ./pkg/storage/firestore -v
```

## Best Practices

1. **Error Handling**: Storage backends should return descriptive errors but not crash on failures
2. **Timeouts**: Use context deadlines for long-running operations
3. **Idempotency**: `Set` operations should be idempotent (safe to call multiple times)
4. **Schema Management**: Auto-create schemas on initialization when possible
5. **Testing**: Provide both unit tests and integration tests
6. **Graceful Degradation**: Cache continues working even if storage backends fail

## Performance Considerations

- In-memory cache provides sub-millisecond responses
- Storage backends are queried only on cache miss
- Storage writes happen on the worker goroutine after changelog generation
- Multiple backends are queried sequentially (first successful response wins)
