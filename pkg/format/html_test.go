package format

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
)

func TestChangeLogHTML(t *testing.T) {
	cl := &changelog.ChangeLog{
		From: changelog.ReleaseInfo{
			Name:    "4.18.2",
			Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Digest:  "sha256:abc",
		},
		To: changelog.ReleaseInfo{
			Name:    "4.18.3",
			Created: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			Digest:  "sha256:def",
		},
		Components: []changelog.ComponentInfo{
			{Name: "Kubernetes", Version: "v1.31.1", From: "v1.31.0", DiffURL: "https://github.com/kubernetes/kubernetes/compare/v1.31.0...v1.31.1"},
		},
		UpdatedImages: []changelog.ImageInfo{
			{
				Name: "cluster-etcd-operator",
				Path: "https://github.com/openshift/cluster-etcd-operator",
				Commits: []changelog.CommitInfo{
					{Subject: "Fix backup rotation", PullURL: "https://github.com/openshift/cluster-etcd-operator/pull/123"},
				},
			},
		},
	}

	html, err := ChangeLogHTML(cl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"<h1>4.18.2 → 4.18.3</h1>",
		"<h2>Components</h2>",
		"<strong>Kubernetes</strong>",
		"<a href=\"https://github.com/kubernetes/kubernetes/compare/v1.31.0...v1.31.1\">diff</a>",
		"<h2>Updated Images</h2>",
		"<a href=\"https://github.com/openshift/cluster-etcd-operator\">cluster-etcd-operator</a>",
		"<a href=\"https://github.com/openshift/cluster-etcd-operator/pull/123\">Fix backup rotation</a>",
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q\ngot:\n%s", want, html)
		}
	}
}

func TestChangeLogHTML_Empty(t *testing.T) {
	cl := &changelog.ChangeLog{
		From: changelog.ReleaseInfo{Name: "4.18.2"},
		To:   changelog.ReleaseInfo{Name: "4.18.3"},
	}

	html, err := ChangeLogHTML(cl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(html, "<h1>4.18.2 → 4.18.3</h1>") {
		t.Errorf("expected header, got:\n%s", html)
	}
	if strings.Contains(html, "<h2>Components</h2>") {
		t.Error("should not include Components section when empty")
	}
}

func TestRpmDiffHTML(t *testing.T) {
	diff := &changelog.RpmDiff{
		Changed: map[string]changelog.RpmChangedDiff{
			"kernel": {Old: "5.14.0-427", New: "5.14.0-428"},
		},
		Added: map[string]string{
			"new-pkg": "1.0.0",
		},
		Removed: map[string]string{
			"old-pkg": "2.0.0",
		},
	}

	html, err := RpmDiffHTML(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"<h2>RPM Package Changes</h2>",
		"<h3>Changed</h3>",
		"<strong>kernel</strong>",
		"<h3>Added</h3>",
		"<strong>new-pkg</strong>",
		"<h3>Removed</h3>",
		"<strong>old-pkg</strong>",
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q\ngot:\n%s", want, html)
		}
	}
}

func TestRpmDiffHTML_Nil(t *testing.T) {
	html, err := RpmDiffHTML(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if html != "" {
		t.Errorf("expected empty string for nil diff, got: %q", html)
	}
}
