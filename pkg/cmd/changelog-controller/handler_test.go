package changelog_controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/utils/clock"
)

func newTestRecorder() events.Recorder {
	return events.NewInMemoryRecorder("test", clock.RealClock{})
}

func TestHandleChangelog_Validation(t *testing.T) {
	// Create mock release-controller server
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{
				"4-stable": ["2.0.0", "1.0.0"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{
						"name": "1.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/test/release:1.0.0-x86_64",
						"downloadURL": "https://example.com/1.0.0"
					},
					{
						"name": "2.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/test/release:2.0.0-x86_64",
						"downloadURL": "https://example.com/2.0.0"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	// Create release-controller client
	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer client.Close()

	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{
				"4-stable": ["2.0.0", "1.0.0"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{
						"name": "1.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/origin/release:1.0.0-x86_64",
						"downloadURL": "https://example.com/origin/1.0.0"
					},
					{
						"name": "2.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/origin/release:2.0.0-x86_64",
						"downloadURL": "https://example.com/origin/2.0.0"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer originServer.Close()

	originEndpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOrigin,
		URL:          originServer.URL,
	}

	originClient, err := rc.NewClient(originEndpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create origin client: %v", err)
	}
	defer originClient.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)
	multiClient.AddClientForTesting(rc.ProductOrigin, rc.ArchitectureAMD64, originClient)

	ctx := context.Background()
	cache := NewChangelogCache()

	// Add test changelogs to the cache using PullSpecs as keys
	cache.Set(ctx, "quay.io/test/release:1.0.0-x86_64", "quay.io/test/release:2.0.0-x86_64", "# Changelog\n\nTest changes")
	cache.Set(ctx, "quay.io/origin/release:1.0.0-x86_64", "quay.io/origin/release:2.0.0-x86_64", "# Changelog\n\nOrigin changes")

	opts := &Options{
		cache:    cache,
		rcClient: multiClient,
	}

	tests := []struct {
		name                 string
		queryParams          string
		expectedStatus       int
		expectedContentType  string
		expectedBodyContains string
	}{
		{
			name:                 "default format is json",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "application/json",
			expectedBodyContains: `"from":"1.0.0"`,
		},
		{
			name:                 "explicit json format",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&format=json",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "application/json",
			expectedBodyContains: `"changelog":`,
		},
		{
			name:                 "markdown format",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&format=md",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "text/markdown",
			expectedBodyContains: "# Changelog",
		},
		{
			name:                 "html format",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&format=html",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "text/html",
			expectedBodyContains: "Changelog",
		},
		{
			name:                 "invalid format",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&format=xml",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "missing from parameter",
			queryParams:          "to=2.0.0&arch=amd64",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "missing to parameter",
			queryParams:          "from=1.0.0&arch=amd64",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "missing arch parameter",
			queryParams:          "from=1.0.0&to=2.0.0",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "invalid arch parameter",
			queryParams:          "from=1.0.0&to=2.0.0&arch=invalid",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "invalid from semver",
			queryParams:          "from=invalid&to=2.0.0&arch=amd64",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "invalid to semver",
			queryParams:          "from=1.0.0&to=invalid&arch=amd64",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
		{
			name:                 "product=ocp explicit",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&product=ocp",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "application/json",
			expectedBodyContains: `"fromPullSpec":"quay.io/test/release:1.0.0-x86_64"`,
		},
		{
			name:                 "product=origin",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&product=origin",
			expectedStatus:       http.StatusOK,
			expectedContentType:  "application/json",
			expectedBodyContains: `"fromPullSpec":"quay.io/origin/release:1.0.0-x86_64"`,
		},
		{
			name:                 "invalid product",
			queryParams:          "from=1.0.0&to=2.0.0&arch=amd64&product=invalid",
			expectedStatus:       http.StatusBadRequest,
			expectedContentType:  "application/json",
			expectedBodyContains: `"error":`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/changelog?"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			opts.handleChangelog(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, tt.expectedContentType) {
				t.Errorf("Expected Content-Type %q, got %q", tt.expectedContentType, contentType)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.expectedBodyContains) {
				t.Errorf("Expected body to contain %q, got: %s", tt.expectedBodyContains, body)
			}
		})
	}
}

func TestHandleChangelog_JSONEscaping(t *testing.T) {
	// Create mock release-controller server
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{
				"4-stable": ["2.0.0", "1.0.0"]
			}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{
						"name": "1.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/test/release:1.0.0-x86_64",
						"downloadURL": "https://example.com/1.0.0"
					},
					{
						"name": "2.0.0",
						"phase": "Accepted",
						"pullSpec": "quay.io/test/release:2.0.0-x86_64",
						"downloadURL": "https://example.com/2.0.0"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	// Create release-controller client
	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer client.Close()

	// Create a MultiClient for testing with just this one client
	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	ctx := context.Background()
	cache := NewChangelogCache()

	// Add a changelog with special characters that need escaping
	changelog := `Line 1
Line 2 with "quotes"
Line 3 with \backslash
Line 4 with	tab`

	cache.Set(ctx, "quay.io/test/release:1.0.0-x86_64", "quay.io/test/release:2.0.0-x86_64", changelog)

	opts := &Options{
		cache:    cache,
		rcClient: multiClient,
	}

	req := httptest.NewRequest("GET", "/changelog?from=1.0.0&to=2.0.0&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleChangelog(w, req)

	body := w.Body.String()

	// Verify escaping
	if !strings.Contains(body, `\n`) {
		t.Error("Expected newlines to be escaped as \\n")
	}
	if !strings.Contains(body, `\"`) {
		t.Error("Expected quotes to be escaped as \\\"")
	}
	if !strings.Contains(body, `\\`) {
		t.Error("Expected backslashes to be escaped as \\\\")
	}
	if !strings.Contains(body, `\t`) {
		t.Error("Expected tabs to be escaped as \\t")
	}
}

func TestWriteChangelogResponse(t *testing.T) {
	opts := &Options{}

	tests := []struct {
		name            string
		from            string
		to              string
		changelog       string
		format          string
		fromPullSpec    string
		toPullSpec      string
		expectedCT      string
		expectedContain string
	}{
		{
			name:            "json without pullspecs",
			from:            "1.0.0",
			to:              "2.0.0",
			changelog:       "Simple changelog",
			format:          "json",
			fromPullSpec:    "",
			toPullSpec:      "",
			expectedCT:      "application/json",
			expectedContain: `"changelog":"Simple changelog"`,
		},
		{
			name:            "json with pullspecs",
			from:            "4.21.9",
			to:              "4.21.10",
			changelog:       "Bugfixes",
			format:          "json",
			fromPullSpec:    "quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64",
			toPullSpec:      "quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64",
			expectedCT:      "application/json",
			expectedContain: `"fromPullSpec":"quay.io/openshift-release-dev/ocp-release:4.21.9-x86_64"`,
		},
		{
			name:            "markdown format",
			from:            "1.0.0",
			to:              "2.0.0",
			changelog:       "Raw changelog text",
			format:          "md",
			fromPullSpec:    "",
			toPullSpec:      "",
			expectedCT:      "text/markdown",
			expectedContain: "Raw changelog text",
		},
		{
			name:            "html format",
			from:            "1.0.0",
			to:              "2.0.0",
			changelog:       "Raw changelog text",
			format:          "html",
			fromPullSpec:    "",
			toPullSpec:      "",
			expectedCT:      "text/html",
			expectedContain: "Raw changelog text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			opts.writeChangelogResponse(w, tt.from, tt.to, tt.changelog, tt.format, "amd64", tt.fromPullSpec, tt.toPullSpec, nil, "")

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, tt.expectedCT) {
				t.Errorf("Expected Content-Type %q, got %q", tt.expectedCT, contentType)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.expectedContain) {
				t.Errorf("Expected body to contain %q, got: %s", tt.expectedContain, body)
			}
		})
	}
}

func TestWriteChangelogResponse_StructuredJSON(t *testing.T) {
	opts := &Options{}

	changelogJSON := `{"from":{"name":"4.18.2","created":"2026-01-01T00:00:00Z","digest":"sha256:abc"},"to":{"name":"4.18.3","created":"2026-01-02T00:00:00Z","digest":"sha256:def"},"components":[{"name":"Kubernetes","version":"v1.31.0","from":"v1.30.0"}],"updatedImages":[{"name":"cluster-etcd-operator","path":"https://github.com/openshift/cluster-etcd-operator","commits":[{"subject":"Fix backup","pullID":123}]}]}`

	w := httptest.NewRecorder()
	opts.writeChangelogResponse(w, "4.18.2", "4.18.3", changelogJSON, "json", "amd64", "registry/from", "registry/to", nil, "")

	resp := w.Result()
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	changelog, ok := result["changelog"].(map[string]any)
	if !ok {
		t.Fatal("changelog field is not a JSON object")
	}

	// Verify the changelog is structured (has typed from/to objects, not raw string)
	fromObj, ok := changelog["from"].(map[string]any)
	if !ok {
		t.Fatal("changelog.from is not a JSON object")
	}
	if fromObj["name"] != "4.18.2" {
		t.Errorf("changelog.from.name = %v, want 4.18.2", fromObj["name"])
	}

	toObj, ok := changelog["to"].(map[string]any)
	if !ok {
		t.Fatal("changelog.to is not a JSON object")
	}
	if toObj["name"] != "4.18.3" {
		t.Errorf("changelog.to.name = %v, want 4.18.3", toObj["name"])
	}

	components, ok := changelog["components"].([]any)
	if !ok {
		t.Fatal("changelog.components is not an array")
	}
	if len(components) != 1 {
		t.Errorf("changelog.components has %d entries, want 1", len(components))
	}

	updatedImages, ok := changelog["updatedImages"].([]any)
	if !ok {
		t.Fatal("changelog.updatedImages is not an array")
	}
	if len(updatedImages) != 1 {
		t.Errorf("changelog.updatedImages has %d entries, want 1", len(updatedImages))
	}
}

func TestWriteChangelogResponse_StructuredHTML(t *testing.T) {
	opts := &Options{}

	changelogJSON := `{"from":{"name":"4.18.2","created":"2026-01-01T00:00:00Z","digest":"sha256:abc"},"to":{"name":"4.18.3","created":"2026-01-02T00:00:00Z","digest":"sha256:def"},"components":[{"name":"Kubernetes","version":"v1.31.1","from":"v1.31.0","diffUrl":"https://github.com/kubernetes/kubernetes/compare/v1.31.0...v1.31.1"}]}`

	rpmDiff := &changelog.RpmDiff{
		Changed: map[string]changelog.RpmChangedDiff{
			"kernel": {Old: "5.14.0-427", New: "5.14.0-428"},
		},
	}

	w := httptest.NewRecorder()
	opts.writeChangelogResponse(w, "4.18.2", "4.18.3", changelogJSON, "html", "amd64", "", "", rpmDiff, "")

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Expected Content-Type text/html, got %q", ct)
	}

	body := w.Body.String()
	checks := []string{
		"<h1>4.18.2",
		"<strong>Kubernetes</strong>",
		"<h2>RPM Package Changes</h2>",
		"<strong>kernel</strong>",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("HTML missing %q\ngot:\n%s", want, body)
		}
	}
}

func TestWriteChangelogResponse_CoreOSEnrichment(t *testing.T) {
	opts := &Options{}

	changelogJSON := `{"from":{"name":"4.18.2","created":"2026-01-01T00:00:00Z","digest":"sha256:abc"},"to":{"name":"4.18.3","created":"2026-01-02T00:00:00Z","digest":"sha256:def"},"components":[{"name":"Red Hat Enterprise Linux CoreOS","version":"418.94.202501010000-0","from":"418.94.202412010000-0"},{"name":"Kubernetes","version":"v1.31.0","from":"v1.30.0"}]}`

	w := httptest.NewRecorder()
	opts.writeChangelogResponse(w, "4.18.2", "4.18.3", changelogJSON, "json", "amd64", "registry/from", "registry/to", nil, "")

	body := w.Body.String()

	// RHCOS component should have enrichment URLs
	if !strings.Contains(body, "releases-rhcos") {
		t.Error("expected RHCOS release browser URL in response")
	}
	if !strings.Contains(body, "versionUrl") {
		t.Error("expected versionUrl field in RHCOS component")
	}
	if !strings.Contains(body, "diff.html") {
		t.Error("expected diff URL in RHCOS component")
	}

	// Kubernetes component should not have URLs
	var result map[string]any
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	changelog := result["changelog"].(map[string]any)
	components := changelog["components"].([]any)
	k8s := components[1].(map[string]any)
	if _, ok := k8s["versionUrl"]; ok {
		t.Error("Kubernetes component should not have versionUrl")
	}
}

func TestHandleKernel(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["4.22.6"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [{
					"name": "4.22.6",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/release:4.22.6-x86_64",
					"downloadURL": "https://example.com/4.22.6"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer client.Close()

	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"5-scos-next": ["5.0.0"]}`))
		case "/api/v1/releasestream/5-scos-next/tags":
			w.Write([]byte(`{
				"name": "5-scos-next",
				"tags": [{
					"name": "5.0.0",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/origin:5.0.0-x86_64",
					"downloadURL": "https://example.com/5.0.0"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer originServer.Close()

	originEndpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOrigin,
		URL:          originServer.URL,
	}

	originClient, err := rc.NewClient(originEndpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create origin client: %v", err)
	}
	defer originClient.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)
	multiClient.AddClientForTesting(rc.ProductOrigin, rc.ArchitectureAMD64, originClient)

	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache["quay.io/test/release:4.22.6-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:4.22.6-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{
				"kernel":    "5.14.0-427.22.1.el9_4",
				"kernel-rt": "5.14.0-427.22.1.rt14.479.el9_4",
				"bash":      "5.2.26-4.el9",
			},
		},
	}
	extractor.cache["quay.io/test/origin:5.0.0-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/origin:5.0.0-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{
				"kernel": "6.12.0-0.el10",
				"bash":   "5.2.32-1.el10",
			},
		},
	}

	opts := &Options{
		rcClient:  multiClient,
		extractor: extractor,
	}

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid kernel lookup",
			queryParams:    "release=4.22.6&arch=amd64",
			expectedStatus: http.StatusOK,
			expectedBody:   `"kernel":"5.14.0-427.22.1.el9_4"`,
		},
		{
			name:           "includes kernel-rt",
			queryParams:    "release=4.22.6&arch=amd64",
			expectedStatus: http.StatusOK,
			expectedBody:   `"kernelRT":"5.14.0-427.22.1.rt14.479.el9_4"`,
		},
		{
			name:           "default product is ocp",
			queryParams:    "release=4.22.6&arch=amd64",
			expectedStatus: http.StatusOK,
			expectedBody:   `"product":"ocp"`,
		},
		{
			name:           "explicit product ocp",
			queryParams:    "release=4.22.6&arch=amd64&product=ocp",
			expectedStatus: http.StatusOK,
			expectedBody:   `"product":"ocp"`,
		},
		{
			name:           "product origin",
			queryParams:    "release=5.0.0&arch=amd64&product=origin",
			expectedStatus: http.StatusOK,
			expectedBody:   `"kernel":"6.12.0-0.el10"`,
		},
		{
			name:           "product origin includes product field",
			queryParams:    "release=5.0.0&arch=amd64&product=origin",
			expectedStatus: http.StatusOK,
			expectedBody:   `"product":"origin"`,
		},
		{
			name:           "invalid product",
			queryParams:    "release=4.22.6&arch=amd64&product=invalid",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `"error"`,
		},
		{
			name:           "missing release parameter",
			queryParams:    "arch=amd64",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `"error"`,
		},
		{
			name:           "missing arch parameter",
			queryParams:    "release=4.22.6",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `"error"`,
		},
		{
			name:           "invalid arch",
			queryParams:    "release=4.22.6&arch=mips",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `"error"`,
		},
		{
			name:           "invalid semver",
			queryParams:    "release=not-a-version&arch=amd64",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `"error"`,
		},
		{
			name:           "release not found",
			queryParams:    "release=9.99.99&arch=amd64",
			expectedStatus: http.StatusNotFound,
			expectedBody:   `"error"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/kernel?"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			opts.handleKernel(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("Expected body to contain %q, got: %s", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestHandleKernel_KernelRTInExtensions(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["4.22.6"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [{
					"name": "4.22.6",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/release:4.22.6-x86_64",
					"downloadURL": "https://example.com/4.22.6"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache["quay.io/test/release:4.22.6-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:4.22.6-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{
				"kernel": "5.14.0-427.22.1.el9_4",
			},
			Extensions: map[string]string{
				"kernel-rt": "5.14.0-427.22.1.rt14.479.el9_4",
			},
		},
	}

	opts := &Options{
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/kernel?release=4.22.6&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleKernel(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, `"kernelRT":"5.14.0-427.22.1.rt14.479.el9_4"`) {
		t.Errorf("Expected kernel-rt from Extensions in response, got: %s", body)
	}
}

func TestHandleKernel_NoKernelPackage(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["4.22.6"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [{
					"name": "4.22.6",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/release:4.22.6-x86_64",
					"downloadURL": "https://example.com/4.22.6"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}

	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create release-controller client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache["quay.io/test/release:4.22.6-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:4.22.6-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{
				"bash":      "5.2.26-4.el9",
				"coreutils": "9.1-8.el9",
			},
		},
	}

	opts := &Options{
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/kernel?release=4.22.6&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleKernel(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for release with no kernel package, got %d", w.Code)
	}
}

func TestHandleKernel_202AcceptedWhenRPMNotCached(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["4.22.6"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [{
					"name": "4.22.6",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/release:4.22.6-x86_64",
					"downloadURL": "https://example.com/4.22.6"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	// Extractor with empty cache — no RPM data
	extractor := NewReleaseExtractor(nil, 0)

	opts := &Options{
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/kernel?release=4.22.6&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleKernel(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "accepted" {
		t.Errorf("Expected status=accepted, got %v", result["status"])
	}
	if result["release"] != "4.22.6" {
		t.Errorf("Expected release=4.22.6, got %v", result["release"])
	}
}

func TestHandleKernel_202WhenSnapshotHasNilRPMList(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["4.22.6"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [{
					"name": "4.22.6",
					"phase": "Accepted",
					"pullSpec": "quay.io/test/release:4.22.6-x86_64",
					"downloadURL": "https://example.com/4.22.6"
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	// Snapshot exists but RPMList is nil (manifest-only extraction)
	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache["quay.io/test/release:4.22.6-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:4.22.6-x86_64",
		Manifest: []byte(`{}`),
		RPMList:  nil,
	}

	opts := &Options{
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/kernel?release=4.22.6&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleKernel(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202 when RPMList is nil, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleKernel_MissingBothParams(t *testing.T) {
	opts := &Options{
		rcClient:  &rc.MultiClient{},
		extractor: NewReleaseExtractor(nil, 0),
	}

	req := httptest.NewRequest("GET", "/kernel", nil)
	w := httptest.NewRecorder()

	opts.handleKernel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 when both params missing, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "'release' and 'arch'") {
		t.Errorf("Expected error mentioning both params, got: %s", body)
	}
}

func TestHandleChangelog_CacheMiss202(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["2.0.0", "1.0.0"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{"name": "1.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:1.0.0-x86_64"},
					{"name": "2.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:2.0.0-x86_64"}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	cache := NewChangelogCache() // empty cache

	recorder := newTestRecorder()
	controller := NewReleasePayloadChangelogController(recorder, cache)

	opts := &Options{
		cache:      cache,
		rcClient:   multiClient,
		controller: controller,
	}

	req := httptest.NewRequest("GET", "/changelog?from=1.0.0&to=2.0.0&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleChangelog(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202 on cache miss, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "accepted" {
		t.Errorf("Expected status=accepted, got %v", result["status"])
	}
	if result["from"] != "1.0.0" {
		t.Errorf("Expected from=1.0.0, got %v", result["from"])
	}
	if result["to"] != "2.0.0" {
		t.Errorf("Expected to=2.0.0, got %v", result["to"])
	}

	// Verify item was enqueued
	stats := controller.QueueStats()
	if stats.Depth != 1 {
		t.Errorf("Expected 1 item in queue, got %d", stats.Depth)
	}
}

func TestHandleChangelog_WithRpmDiff(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["2.0.0", "1.0.0"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{"name": "1.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:1.0.0-x86_64"},
					{"name": "2.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:2.0.0-x86_64"}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	ctx := context.Background()
	cache := NewChangelogCache()
	cache.Set(ctx, "quay.io/test/release:1.0.0-x86_64", "quay.io/test/release:2.0.0-x86_64", `{"from":{"name":"1.0.0"},"to":{"name":"2.0.0"}}`)

	// Pre-populate extractor with RPM data for both releases
	extractor := NewReleaseExtractor(nil, 0)
	extractor.cache["quay.io/test/release:1.0.0-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:1.0.0-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-284.el9", "bash": "5.2.26-4.el9"},
		},
	}
	extractor.cache["quay.io/test/release:2.0.0-x86_64"] = &storage.ReleaseSnapshot{
		PullSpec: "quay.io/test/release:2.0.0-x86_64",
		RPMList: &storage.RpmList{
			Packages: map[string]string{"kernel": "5.14.0-427.el9", "bash": "5.2.26-4.el9"},
		},
	}

	opts := &Options{
		cache:     cache,
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/changelog?from=1.0.0&to=2.0.0&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleChangelog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	rpmDiff, ok := result["rpmDiff"].(map[string]any)
	if !ok {
		t.Fatal("Expected rpmDiff field in response")
	}
	changed, ok := rpmDiff["changed"].(map[string]any)
	if !ok {
		t.Fatal("Expected rpmDiff.changed field")
	}
	kernelDiff, ok := changed["kernel"].(map[string]any)
	if !ok {
		t.Fatal("Expected kernel entry in rpmDiff.changed")
	}
	if kernelDiff["old"] != "5.14.0-284.el9" {
		t.Errorf("Expected kernel old=5.14.0-284.el9, got %v", kernelDiff["old"])
	}
	if kernelDiff["new"] != "5.14.0-427.el9" {
		t.Errorf("Expected kernel new=5.14.0-427.el9, got %v", kernelDiff["new"])
	}

	// rpmDiffStatus should be absent when diff is present
	if _, hasStatus := result["rpmDiffStatus"]; hasStatus {
		t.Error("Expected rpmDiffStatus to be omitted when rpmDiff is present")
	}
}

func TestHandleChangelog_RpmDiffStatusWhenMissing(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["2.0.0", "1.0.0"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{"name": "1.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:1.0.0-x86_64"},
					{"name": "2.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:2.0.0-x86_64"}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	ctx := context.Background()
	cache := NewChangelogCache()
	cache.Set(ctx, "quay.io/test/release:1.0.0-x86_64", "quay.io/test/release:2.0.0-x86_64", "changelog data")

	// Extractor with no RPM data cached
	extractor := NewReleaseExtractor(nil, 0)

	opts := &Options{
		cache:     cache,
		rcClient:  multiClient,
		extractor: extractor,
	}

	req := httptest.NewRequest("GET", "/changelog?from=1.0.0&to=2.0.0&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleChangelog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// rpmDiff should be absent
	if _, hasDiff := result["rpmDiff"]; hasDiff {
		t.Error("Expected rpmDiff to be omitted when RPM data is missing")
	}

	// rpmDiffStatus should be present with a useful message
	status, hasStatus := result["rpmDiffStatus"].(string)
	if !hasStatus {
		t.Fatal("Expected rpmDiffStatus field when RPM data is missing")
	}
	if !strings.Contains(status, "extraction") {
		t.Errorf("Expected rpmDiffStatus to mention extraction, got: %s", status)
	}
}

func TestHandleChangelog_NoControllerReturns500(t *testing.T) {
	rcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/api/v1/releasestreams/all":
			w.Write([]byte(`{"4-stable": ["2.0.0", "1.0.0"]}`))
		case "/api/v1/releasestream/4-stable/tags":
			w.Write([]byte(`{
				"name": "4-stable",
				"tags": [
					{"name": "1.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:1.0.0-x86_64"},
					{"name": "2.0.0", "phase": "Accepted", "pullSpec": "quay.io/test/release:2.0.0-x86_64"}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcServer.Close()

	endpoint := &rc.Endpoint{
		Architecture: rc.ArchitectureAMD64,
		Product:      rc.ProductOCP,
		URL:          rcServer.URL,
	}
	client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	multiClient := &rc.MultiClient{}
	multiClient.AddClientForTesting(rc.ProductOCP, rc.ArchitectureAMD64, client)

	opts := &Options{
		cache:      NewChangelogCache(), // empty
		rcClient:   multiClient,
		controller: nil, // no controller
	}

	req := httptest.NewRequest("GET", "/changelog?from=1.0.0&to=2.0.0&arch=amd64", nil)
	w := httptest.NewRecorder()

	opts.handleChangelog(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 when controller is nil, got %d", w.Code)
	}
}

func TestWriteChangelogResponse_FallbackForNonChangelogJSON(t *testing.T) {
	opts := &Options{}

	// Valid JSON but not a ChangeLog (no from/to names)
	arbitraryJSON := `{"key":"value","nested":{"a":1}}`

	w := httptest.NewRecorder()
	opts.writeChangelogResponse(w, "1.0.0", "2.0.0", arbitraryJSON, "json", "amd64", "", "", nil, "")

	var result map[string]any
	if err := json.NewDecoder(w.Result().Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should preserve the raw JSON, not convert to an empty ChangeLog
	changelog, ok := result["changelog"].(map[string]any)
	if !ok {
		t.Fatal("changelog field is not a JSON object")
	}
	if changelog["key"] != "value" {
		t.Errorf("arbitrary JSON field not preserved: got %v", changelog["key"])
	}
}
