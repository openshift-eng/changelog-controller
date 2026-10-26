package releasecontroller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetAllReleaseStreams(t *testing.T) {
	// Create a test server that returns a mock response
	// The response contains release streams with arrays of release names
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/releasestreams/all" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": [
				"4.21.10",
				"4.21.9",
				"4.21.8"
			],
			"4-dev-preview": [
				"4.22.0-ec.5",
				"4.22.0-ec.4"
			],
			"4.14.0-0.nightly": [
				"4.14.0-0.nightly-2024-01-15-123456",
				"4.14.0-0.nightly-2024-01-14-123456",
				"4.14.0-0.nightly-2024-01-13-123456"
			]
		}`))
	}))
	defer server.Close()

	// Create client pointing to test server
	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Test GetAllReleaseStreams
	streams, err := client.GetAllReleaseStreams(ctx)
	if err != nil {
		t.Fatalf("GetAllReleaseStreams failed: %v", err)
	}

	// Verify we got all three streams
	if len(streams) != 3 {
		t.Errorf("expected 3 streams, got %d", len(streams))
	}

	// Verify 4-stable stream
	stableReleases, ok := streams["4-stable"]
	if !ok {
		t.Fatal("4-stable stream not found")
	}
	if len(stableReleases) != 3 {
		t.Errorf("expected 3 releases in 4-stable, got %d", len(stableReleases))
	}
	if stableReleases[0] != "4.21.10" {
		t.Errorf("expected first release to be 4.21.10, got %s", stableReleases[0])
	}

	// Verify 4-dev-preview stream
	devPreviewReleases, ok := streams["4-dev-preview"]
	if !ok {
		t.Fatal("4-dev-preview stream not found")
	}
	if len(devPreviewReleases) != 2 {
		t.Errorf("expected 2 releases in 4-dev-preview, got %d", len(devPreviewReleases))
	}

	// Verify 4.14.0-0.nightly stream
	nightlyReleases, ok := streams["4.14.0-0.nightly"]
	if !ok {
		t.Fatal("4.14.0-0.nightly stream not found")
	}
	if len(nightlyReleases) != 3 {
		t.Errorf("expected 3 releases in nightly, got %d", len(nightlyReleases))
	}
}

func TestGetReleaseStreamNames(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": ["4.21.10"],
			"4-dev-preview": ["4.22.0-ec.5"],
			"4.14.0-0.nightly": []
		}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Test GetReleaseStreamNames
	names, err := client.GetReleaseStreamNames(ctx)
	if err != nil {
		t.Fatalf("GetReleaseStreamNames failed: %v", err)
	}

	if len(names) != 3 {
		t.Errorf("expected 3 stream names, got %d", len(names))
	}

	// Names should be sorted
	expected := []string{"4-dev-preview", "4-stable", "4.14.0-0.nightly"}
	if !reflect.DeepEqual(names, expected) {
		t.Errorf("expected %v, got %v", expected, names)
	}
}

func TestGetReleasesForStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": [
				"4.21.10",
				"4.21.9",
				"4.21.8"
			],
			"4-dev-preview": [
				"4.22.0-ec.5"
			]
		}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Test getting releases for a specific stream
	releases, err := client.GetReleasesForStream(ctx, "4-stable")
	if err != nil {
		t.Fatalf("GetReleasesForStream failed: %v", err)
	}

	expected := []string{"4.21.10", "4.21.9", "4.21.8"}
	if !reflect.DeepEqual(releases, expected) {
		t.Errorf("expected %v, got %v", expected, releases)
	}

	// Test getting releases for non-existent stream
	_, err = client.GetReleasesForStream(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent stream, got nil")
	}
}

func TestGetLatestReleaseForStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": [
				"4.21.10",
				"4.21.9"
			],
			"empty-stream": []
		}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Test getting latest release
	latest, err := client.GetLatestReleaseForStream(ctx, "4-stable")
	if err != nil {
		t.Fatalf("GetLatestReleaseForStream failed: %v", err)
	}

	if latest != "4.21.10" {
		t.Errorf("expected 4.21.10, got %s", latest)
	}

	// Test empty stream
	_, err = client.GetLatestReleaseForStream(ctx, "empty-stream")
	if err == nil {
		t.Error("expected error for empty stream, got nil")
	}

	// Test non-existent stream
	_, err = client.GetLatestReleaseForStream(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent stream, got nil")
	}
}

func TestGetStreamStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": [
				"4.21.10",
				"4.21.9",
				"4.21.8"
			],
			"4-dev-preview": [
				"4.22.0-ec.5",
				"4.22.0-ec.4"
			],
			"empty-stream": []
		}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	stats, err := client.GetStreamStats(ctx)
	if err != nil {
		t.Fatalf("GetStreamStats failed: %v", err)
	}

	if len(stats) != 3 {
		t.Errorf("expected 3 stats entries, got %d", len(stats))
	}

	// Stats should be sorted by name
	if stats[0].Name != "4-dev-preview" {
		t.Errorf("expected first stat to be 4-dev-preview, got %s", stats[0].Name)
	}

	// Check 4-stable stats
	var stableStats *StreamStats
	for i := range stats {
		if stats[i].Name == "4-stable" {
			stableStats = &stats[i]
			break
		}
	}

	if stableStats == nil {
		t.Fatal("4-stable stats not found")
	}

	if stableStats.ReleaseCount != 3 {
		t.Errorf("expected 3 releases in 4-stable, got %d", stableStats.ReleaseCount)
	}

	if stableStats.LatestRelease != "4.21.10" {
		t.Errorf("expected latest release 4.21.10, got %s", stableStats.LatestRelease)
	}

	// Check empty stream
	var emptyStats *StreamStats
	for i := range stats {
		if stats[i].Name == "empty-stream" {
			emptyStats = &stats[i]
			break
		}
	}

	if emptyStats == nil {
		t.Fatal("empty-stream stats not found")
	}

	if emptyStats.ReleaseCount != 0 {
		t.Errorf("expected 0 releases in empty-stream, got %d", emptyStats.ReleaseCount)
	}

	if emptyStats.LatestRelease != "" {
		t.Errorf("expected empty latest release, got %s", emptyStats.LatestRelease)
	}
}

func TestGetAllReleaseStreamsEmpty(t *testing.T) {
	// Create a test server that returns an empty response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	streams, err := client.GetAllReleaseStreams(ctx)
	if err != nil {
		t.Fatalf("GetAllReleaseStreams failed: %v", err)
	}

	if len(streams) != 0 {
		t.Errorf("expected 0 streams, got %d", len(streams))
	}
}

func TestGetAllReleaseStreamsError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	_, err = client.GetAllReleaseStreams(ctx)
	if err == nil {
		t.Error("expected error, got nil")
	}

	_, err = client.GetReleaseStreamNames(ctx)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestFindRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"4-stable": [
				"4.21.10",
				"4.21.9",
				"4.13.0-rc.8",
				"4.21.8"
			],
			"4-dev-preview": [
				"4.22.0-ec.5",
				"4.22.0-ec.4"
			],
			"4.13.0-0.nightly": [
				"4.13.0-0.nightly-2024-01-15-123456",
				"4.13.0-rc.8",
				"4.13.0-rc.7"
			]
		}`))
	}))
	defer server.Close()

	endpoint := &Endpoint{
		Architecture: ArchitectureAMD64,
		Product:      ProductOCP,
		URL:          server.URL,
	}

	client, err := NewClient(endpoint, DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Test finding a release that exists in multiple streams
	locations, err := client.FindRelease(ctx, "4.13.0-rc.8")
	if err != nil {
		t.Fatalf("FindRelease failed: %v", err)
	}

	if len(locations) != 2 {
		t.Errorf("expected 2 locations, got %d", len(locations))
	}

	// Verify the locations (sorted by stream name)
	// Check stream names
	streamNames := []string{locations[0].StreamName, locations[1].StreamName}
	expectedStreams := []string{"4-stable", "4.13.0-0.nightly"}
	if !reflect.DeepEqual(streamNames, expectedStreams) {
		t.Errorf("expected streams %+v, got %+v", expectedStreams, streamNames)
	}

	// Verify endpoint is set
	for i, loc := range locations {
		if loc.Endpoint == nil {
			t.Errorf("location %d missing endpoint", i)
		} else if loc.Endpoint.Architecture != ArchitectureAMD64 {
			t.Errorf("expected amd64, got %s", loc.Endpoint.Architecture)
		}
	}

	// Test finding a release that exists in only one stream
	locations, err = client.FindRelease(ctx, "4.21.10")
	if err != nil {
		t.Fatalf("FindRelease failed: %v", err)
	}

	if len(locations) != 1 {
		t.Errorf("expected 1 location, got %d", len(locations))
	}

	if locations[0].StreamName != "4-stable" {
		t.Errorf("expected 4-stable, got %s", locations[0].StreamName)
	}

	// Test finding a release that doesn't exist
	locations, err = client.FindRelease(ctx, "nonexistent-release")
	if err != nil {
		t.Fatalf("FindRelease failed: %v", err)
	}

	if len(locations) != 0 {
		t.Errorf("expected 0 locations for nonexistent release, got %d", len(locations))
	}
}
