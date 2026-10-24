package bigquery

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"google.golang.org/api/iterator"
)

// BigQueryStorage implements changelog storage using Google BigQuery
type BigQueryStorage struct {
	projectID string
	datasetID string
	tableID   string
	client    *bigquery.Client
}

// ChangelogRow represents a row in the BigQuery changelogs table
type ChangelogRow struct {
	FromVersion string    `bigquery:"from_version"`
	ToVersion   string    `bigquery:"to_version"`
	Changelog   string    `bigquery:"changelog"`
	CreatedAt   time.Time `bigquery:"created_at"`
}

// Config holds configuration for BigQuery storage
type Config struct {
	ProjectID string
	DatasetID string
	TableID   string
}

var _ storage.ChangelogStorage = (*BigQueryStorage)(nil)
var _ storage.ChangelogLister = (*BigQueryStorage)(nil)
var _ storage.ChangelogCounter = (*BigQueryStorage)(nil)

// NewBigQueryStorage creates a new BigQuery storage backend
func NewBigQueryStorage(ctx context.Context, cfg Config) (*BigQueryStorage, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("projectID is required")
	}
	if cfg.DatasetID == "" {
		return nil, fmt.Errorf("datasetID is required")
	}
	if cfg.TableID == "" {
		return nil, fmt.Errorf("tableID is required")
	}

	client, err := bigquery.NewClient(ctx, cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}

	return &BigQueryStorage{
		projectID: cfg.ProjectID,
		datasetID: cfg.DatasetID,
		tableID:   cfg.TableID,
		client:    client,
	}, nil
}

// Get retrieves a changelog from BigQuery
func (s *BigQueryStorage) Get(ctx context.Context, from, to string) (string, bool, error) {
	q := s.client.Query(fmt.Sprintf(`
		SELECT changelog
		FROM `+"`%s.%s.%s`"+`
		WHERE from_version = @from AND to_version = @to
		LIMIT 1
	`, s.projectID, s.datasetID, s.tableID))
	q.Parameters = []bigquery.QueryParameter{
		{Name: "from", Value: from},
		{Name: "to", Value: to},
	}

	it, err := q.Read(ctx)
	if err != nil {
		return "", false, fmt.Errorf("failed to query BigQuery: %w", err)
	}

	var row ChangelogRow
	err = it.Next(&row)
	if err == iterator.Done {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read query result: %w", err)
	}

	return row.Changelog, true, nil
}

// Set stores a changelog in BigQuery
func (s *BigQueryStorage) Set(ctx context.Context, from, to, changelog string) error {
	inserter := s.client.Dataset(s.datasetID).Table(s.tableID).Inserter()
	items := []*ChangelogRow{
		{
			FromVersion: from,
			ToVersion:   to,
			Changelog:   changelog,
			CreatedAt:   time.Now(),
		},
	}
	if err := inserter.Put(ctx, items); err != nil {
		return fmt.Errorf("failed to insert into BigQuery: %w", err)
	}
	return nil
}

// Close closes the BigQuery client
func (s *BigQueryStorage) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// List returns all changelog entries stored in BigQuery
// Implements the ChangelogLister interface for cache warming
func (s *BigQueryStorage) List(ctx context.Context) ([]storage.ChangelogEntry, error) {
	q := s.client.Query(fmt.Sprintf(`
		SELECT from_version, to_version, changelog
		FROM `+"`%s.%s.%s`"+`
		ORDER BY created_at DESC
	`, s.projectID, s.datasetID, s.tableID))

	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query BigQuery: %w", err)
	}

	var entries []storage.ChangelogEntry
	for {
		var row ChangelogRow
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read query result: %w", err)
		}

		entries = append(entries, storage.ChangelogEntry{
			FromVersion: row.FromVersion,
			ToVersion:   row.ToVersion,
			Changelog:   row.Changelog,
		})
	}

	return entries, nil
}

// Count returns the number of changelog entries without fetching their contents
func (s *BigQueryStorage) Count(ctx context.Context) (int, error) {
	q := s.client.Query(fmt.Sprintf(`
		SELECT COUNT(*) AS count
		FROM `+"`%s.%s.%s`"+`
	`, s.projectID, s.datasetID, s.tableID))

	it, err := q.Read(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query BigQuery count: %w", err)
	}

	var row struct {
		Count int64 `bigquery:"count"`
	}
	if err := it.Next(&row); err != nil {
		return 0, fmt.Errorf("failed to read BigQuery count: %w", err)
	}

	return int(row.Count), nil
}
