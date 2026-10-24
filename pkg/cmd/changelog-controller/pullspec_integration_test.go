package changelog_controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

// TestPullSpecIntegration tests the full integration with mock release-controller endpoints
func TestPullSpecIntegration(t *testing.T) {
	// Create mock release-controller server
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			// Return a simple release stream with our test releases
			w.Write([]byte(`{
				"4-stable": ["4.21.10", "4.21.9", "4.21.8"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
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
	defer rcServer.Close()

	// Create a mock release-controller client pointed at our test server
	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	rcClient, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer rcClient.Close()

	// Create cache with sample changelog
	ctx := context.Background()
	cache := NewChangelogCache()
	cache.Set(ctx, "4.21.9", "4.21.10", "# Release 4.21.10\n\nBug fixes and improvements")

	// Create options with both cache and a single client
	// Note: In the real implementation, we'd use MultiClient, but for testing
	// we'll simulate the fetchPullSpecs behavior directly
	opts := &Options{
		cache: cache,
	}

	// Test that we can fetch PullSpecs using our mock client
	t.Run("fetch pullspecs from mock server", func(t *testing.T) {
		fromPullSpec, err := rcClient.GetPullSpec(ctx, "4.21.9")
		if err != nil {
			t.Fatalf("failed to get from pullspec: %v", err)
		}
		expectedFrom := "quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64"
		if fromPullSpec != expectedFrom {
			t.Errorf("expected from pullspec %q, got %q", expectedFrom, fromPullSpec)
		}

		toPullSpec, err := rcClient.GetPullSpec(ctx, "4.21.10")
		if err != nil {
			t.Fatalf("failed to get to pullspec: %v", err)
		}
		expectedTo := "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64"
		if toPullSpec != expectedTo {
			t.Errorf("expected to pullspec %q, got %q", expectedTo, toPullSpec)
		}
	})

	// Test JSON response with PullSpecs
	t.Run("json response with pullspecs", func(t *testing.T) {
		w := httptest.NewRecorder()
		fromPS := "quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64"
		toPS := "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64"
		changelog := "# Release 4.21.10\n\nBug fixes"

		opts.writeChangelogResponse(w, "4.21.9", "4.21.10", changelog, "json", "amd64", fromPS, toPS, nil, "")

		resp := w.Result()
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if result["from"] != "4.21.9" {
			t.Errorf("expected from=4.21.9, got %v", result["from"])
		}
		if result["to"] != "4.21.10" {
			t.Errorf("expected to=4.21.10, got %v", result["to"])
		}
		if result["fromPullSpec"] != fromPS {
			t.Errorf("expected fromPullSpec=%s, got %v", fromPS, result["fromPullSpec"])
		}
		if result["toPullSpec"] != toPS {
			t.Errorf("expected toPullSpec=%s, got %v", toPS, result["toPullSpec"])
		}
		if !strings.Contains(result["changelog"].(string), "Bug fixes") {
			t.Error("changelog should contain 'Bug fixes'")
		}
	})

	// Test graceful degradation without PullSpecs
	t.Run("response without pullspecs", func(t *testing.T) {
		w := httptest.NewRecorder()
		changelog := "# Release 4.21.10\n\nBug fixes"

		opts.writeChangelogResponse(w, "4.21.9", "4.21.10", changelog, "json", "amd64", "", "", nil, "")

		resp := w.Result()
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		// Should NOT have pullSpec fields when they're empty
		if _, exists := result["fromPullSpec"]; exists {
			t.Error("should not include fromPullSpec field when empty")
		}
		if _, exists := result["toPullSpec"]; exists {
			t.Error("should not include toPullSpec field when empty")
		}

		// Should still have other fields
		if result["from"] != "4.21.9" {
			t.Errorf("expected from=4.21.9, got %v", result["from"])
		}
		if result["changelog"] == nil {
			t.Error("changelog field should be present")
		}
	})
}
