package releasecontroller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetReleaseInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/api/v1/releasestream/4-stable/release/4.21.10"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, expected %s", r.URL.Path, expectedPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"name": "4.21.10",
			"phase": "Accepted",
			"changeLog": "This is the changelog text",
			"changeLogJson": {
				"from": {
					"name": "4.21.9",
					"created": "2026-04-03T09:39:33Z",
					"digest": "sha256:7000f41b49c638e64d008be885b8d9e68c34f3558acb6b7bf920f4aaf22da425"
				},
				"to": {
					"name": "4.21.10",
					"created": "2026-04-08T10:15:20Z",
					"digest": "sha256:8111f41b49c638e64d008be885b8d9e68c34f3558acb6b7bf920f4aaf22da426"
				},
				"components": [
					{
						"name": "Kubernetes",
						"version": "v1.30.1",
						"from": "v1.30.0"
					}
				],
				"updatedImages": [
					{
						"name": "cluster-etcd-operator",
						"path": "https://github.com/openshift/cluster-etcd-operator/tree/abc123",
						"commits": [
							{
								"subject": "Fix etcd backup",
								"pullID": 123,
								"pullURL": "https://github.com/openshift/cluster-etcd-operator/pull/123"
							}
						],
						"fullChangeLog": "https://github.com/openshift/cluster-etcd-operator/compare/old...new"
					}
				],
				"rebuiltImages": [
					{
						"name": "cli",
						"path": "https://github.com/openshift/oc",
						"shortCommit": "abc123",
						"commit": "abc123def456",
						"imageRef": "sha256:abcdef"
					}
				]
			},
			"results": {
				"blockingJobs": {
					"upgrade": {
						"state": "Succeeded",
						"url": "https://example.com/job1"
					}
				},
				"informingJobs": {
					"aws-ovn-serial": {
						"state": "Succeeded",
						"url": "https://example.com/job2",
						"retries": 1,
						"previousAttemptURLs": ["https://example.com/job2-retry1"]
					}
				}
			},
			"upgradesTo": [
				{
					"From": "4.21.3",
					"To": "4.21.10",
					"Success": 5,
					"Failure": 1,
					"Total": 6
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

	// Test GetReleaseInfo
	info, err := client.GetReleaseInfo(ctx, "4-stable", "4.21.10")
	if err != nil {
		t.Fatalf("GetReleaseInfo failed: %v", err)
	}

	// Verify basic fields
	if info.Name != "4.21.10" {
		t.Errorf("expected name 4.21.10, got %s", info.Name)
	}

	if info.Phase != "Accepted" {
		t.Errorf("expected phase Accepted, got %s", info.Phase)
	}

	if info.ChangeLog != "This is the changelog text" {
		t.Errorf("unexpected changelog: %s", info.ChangeLog)
	}

	// Verify changeLogJson
	if info.ChangeLogJson == nil {
		t.Fatal("changeLogJson is nil")
	}

	if info.ChangeLogJson.From == nil {
		t.Fatal("changeLogJson.From is nil")
	}

	if info.ChangeLogJson.From.Name != "4.21.9" {
		t.Errorf("expected from 4.21.9, got %s", info.ChangeLogJson.From.Name)
	}

	if info.ChangeLogJson.To == nil {
		t.Fatal("changeLogJson.To is nil")
	}

	if info.ChangeLogJson.To.Name != "4.21.10" {
		t.Errorf("expected to 4.21.10, got %s", info.ChangeLogJson.To.Name)
	}

	if len(info.ChangeLogJson.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(info.ChangeLogJson.Components))
	} else {
		comp := info.ChangeLogJson.Components[0]
		if comp.Name != "Kubernetes" {
			t.Errorf("expected component Kubernetes, got %s", comp.Name)
		}
		if comp.Version != "v1.30.1" {
			t.Errorf("expected version v1.30.1, got %s", comp.Version)
		}
		if comp.From != "v1.30.0" {
			t.Errorf("expected from v1.30.0, got %s", comp.From)
		}
	}

	// Verify updatedImages
	if len(info.ChangeLogJson.UpdatedImages) != 1 {
		t.Errorf("expected 1 updated image, got %d", len(info.ChangeLogJson.UpdatedImages))
	} else {
		img := info.ChangeLogJson.UpdatedImages[0]
		if img.Name != "cluster-etcd-operator" {
			t.Errorf("expected image cluster-etcd-operator, got %s", img.Name)
		}
		if len(img.Commits) != 1 {
			t.Errorf("expected 1 commit, got %d", len(img.Commits))
		} else {
			commit := img.Commits[0]
			if commit.Subject != "Fix etcd backup" {
				t.Errorf("unexpected commit subject: %s", commit.Subject)
			}
			if commit.PullID != 123 {
				t.Errorf("expected pullID 123, got %d", commit.PullID)
			}
		}
	}

	// Verify rebuiltImages
	if len(info.ChangeLogJson.RebuiltImages) != 1 {
		t.Errorf("expected 1 rebuilt image, got %d", len(info.ChangeLogJson.RebuiltImages))
	} else {
		img := info.ChangeLogJson.RebuiltImages[0]
		if img.Name != "cli" {
			t.Errorf("expected image cli, got %s", img.Name)
		}
		if img.ShortCommit != "abc123" {
			t.Errorf("expected shortCommit abc123, got %s", img.ShortCommit)
		}
	}

	// Verify results
	if info.Results == nil {
		t.Fatal("results is nil")
	}

	if len(info.Results.BlockingJobs) != 1 {
		t.Errorf("expected 1 blocking job, got %d", len(info.Results.BlockingJobs))
	}

	if job, ok := info.Results.BlockingJobs["upgrade"]; ok {
		if job.State != JobStateSucceeded {
			t.Errorf("expected Succeeded, got %s", job.State)
		}
		if job.URL != "https://example.com/job1" {
			t.Errorf("unexpected URL: %s", job.URL)
		}
	} else {
		t.Error("upgrade job not found in blocking jobs")
	}

	if len(info.Results.InformingJobs) != 1 {
		t.Errorf("expected 1 informing job, got %d", len(info.Results.InformingJobs))
	}

	if job, ok := info.Results.InformingJobs["aws-ovn-serial"]; ok {
		if job.Retries != 1 {
			t.Errorf("expected 1 retry, got %d", job.Retries)
		}
		if len(job.PreviousAttemptURLs) != 1 {
			t.Errorf("expected 1 previous attempt URL, got %d", len(job.PreviousAttemptURLs))
		}
	} else {
		t.Error("aws-ovn-serial job not found in informing jobs")
	}

	// Verify upgradesTo
	if len(info.UpgradesTo) != 1 {
		t.Errorf("expected 1 upgrade history, got %d", len(info.UpgradesTo))
	} else {
		upgrade := info.UpgradesTo[0]
		if upgrade.From != "4.21.3" {
			t.Errorf("expected from 4.21.3, got %s", upgrade.From)
		}
		if upgrade.Success != 5 {
			t.Errorf("expected 5 successes, got %d", upgrade.Success)
		}
		if upgrade.Total != 6 {
			t.Errorf("expected 6 total, got %d", upgrade.Total)
		}
	}
}

func TestGetReleaseInfoForLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"name": "4.21.10",
			"phase": "Accepted"
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

	// Create a release location
	location := ReleaseLocation{
		Endpoint:   endpoint,
		StreamName: "4-stable",
	}

	// Test GetReleaseInfoForLocation
	info, err := client.GetReleaseInfoForLocation(ctx, location, "4.21.10")
	if err != nil {
		t.Fatalf("GetReleaseInfoForLocation failed: %v", err)
	}

	if info.Name != "4.21.10" {
		t.Errorf("expected name 4.21.10, got %s", info.Name)
	}
}

func TestGetReleaseInfoNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
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

	_, err = client.GetReleaseInfo(ctx, "4-stable", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent release, got nil")
	}
}
