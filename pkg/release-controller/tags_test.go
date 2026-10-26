package releasecontroller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTagsForStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/api/v1/releasestream/4-stable/tags"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, expected %s", r.URL.Path, expectedPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"name": "4-stable",
			"tags": [
				{
					"name": "4.21.10",
					"phase": "Accepted",
					"pullSpec": "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64",
					"downloadURL": "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.10"
				},
				{
					"name": "4.21.9",
					"phase": "Accepted",
					"pullSpec": "quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64",
					"downloadURL": "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.9"
				}
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

	tags, err := client.GetTagsForStream(ctx, "4-stable")
	if err != nil {
		t.Fatalf("GetTagsForStream failed: %v", err)
	}

	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// Verify first tag
	if tags[0].Name != "4.21.10" {
		t.Errorf("expected tag name 4.21.10, got %s", tags[0].Name)
	}

	if tags[0].Phase != "Accepted" {
		t.Errorf("expected phase Accepted, got %s", tags[0].Phase)
	}

	expectedPullSpec := "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64"
	if tags[0].PullSpec != expectedPullSpec {
		t.Errorf("expected pullSpec %s, got %s", expectedPullSpec, tags[0].PullSpec)
	}

	expectedDownloadURL := "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.10"
	if tags[0].DownloadURL != expectedDownloadURL {
		t.Errorf("expected downloadURL %s, got %s", expectedDownloadURL, tags[0].DownloadURL)
	}
}

func TestGetPullSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			// Return a simple release stream with our test release
			_, _ = w.Write([]byte(`{
				"4-stable": ["4.21.10", "4.21.9"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			_, _ = w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{
						"name": "4.21.10",
						"phase": "Accepted",
						"pullSpec": "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64",
						"downloadURL": "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.10"
					},
					{
						"name": "4.21.9",
						"phase": "Accepted",
						"pullSpec": "quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64",
						"downloadURL": "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.9"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	pullSpec, err := client.GetPullSpec(ctx, "4.21.10")
	if err != nil {
		t.Fatalf("GetPullSpec failed: %v", err)
	}

	expectedPullSpec := "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64"
	if pullSpec != expectedPullSpec {
		t.Errorf("expected pullSpec %s, got %s", expectedPullSpec, pullSpec)
	}
}

func TestGetTagForRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			_, _ = w.Write([]byte(`{
				"4-stable": ["4.21.10", "4.21.9"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			_, _ = w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{
						"name": "4.21.10",
						"phase": "Accepted",
						"pullSpec": "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64",
						"downloadURL": "https://openshift-release-artifacts.apps.ci.l2s4.p1.openshiftapps.com/4.21.10"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	tag, err := client.GetTagForRelease(ctx, "4.21.10")
	if err != nil {
		t.Fatalf("GetTagForRelease failed: %v", err)
	}

	if tag.Name != "4.21.10" {
		t.Errorf("expected tag name 4.21.10, got %s", tag.Name)
	}

	if tag.Phase != "Accepted" {
		t.Errorf("expected phase Accepted, got %s", tag.Phase)
	}

	expectedPullSpec := "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64"
	if tag.PullSpec != expectedPullSpec {
		t.Errorf("expected pullSpec %s, got %s", expectedPullSpec, tag.PullSpec)
	}
}

func TestGetPullSpecNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	_, err = client.GetPullSpec(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent release, got nil")
	}
}
