package bigquery

import (
	"context"
	"testing"
)

func TestNewBigQueryStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "missing project ID",
			cfg: Config{
				DatasetID: "test-dataset",
				TableID:   "test-table",
			},
			wantErr: true,
		},
		{
			name: "missing dataset ID",
			cfg: Config{
				ProjectID: "test-project",
				TableID:   "test-table",
			},
			wantErr: true,
		},
		{
			name: "missing table ID",
			cfg: Config{
				ProjectID: "test-project",
				DatasetID: "test-dataset",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := NewBigQueryStorage(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBigQueryStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestBigQueryStorage_GetSet(t *testing.T) {
	// This is an integration test that requires actual BigQuery access
	// Skip if BIGQUERY_PROJECT_ID is not set
	projectID := "test-project"
	if projectID == "test-project" {
		t.Skip("Skipping BigQuery integration test - requires real BigQuery credentials and BIGQUERY_PROJECT_ID")
	}

	ctx := context.Background()
	storage, err := NewBigQueryStorage(ctx, Config{
		ProjectID: projectID,
		DatasetID: "test_dataset",
		TableID:   "test_changelogs",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Test Set
	testChangelog := "Test changelog content"
	err = storage.Set(ctx, "1.0.0", "2.0.0", testChangelog)
	if err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Test Get after Set
	changelog, found, err := storage.Get(ctx, "1.0.0", "2.0.0")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true after Set")
	}
	if changelog != testChangelog {
		t.Errorf("Get() changelog = %q, want %q", changelog, testChangelog)
	}
}
