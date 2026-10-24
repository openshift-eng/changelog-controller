package format

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
)

func TestChangeLogMarkdown(t *testing.T) {
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

	md := ChangeLogMarkdown(cl)

	checks := []string{
		"# 4.18.2 → 4.18.3",
		"## Components",
		"**Kubernetes**: v1.31.0 → v1.31.1",
		"[diff](",
		"## Updated Images",
		"### [cluster-etcd-operator](",
		"- [Fix backup rotation](",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\ngot:\n%s", want, md)
		}
	}
}

func TestChangeLogMarkdown_Empty(t *testing.T) {
	cl := &changelog.ChangeLog{
		From: changelog.ReleaseInfo{Name: "4.18.2"},
		To:   changelog.ReleaseInfo{Name: "4.18.3"},
	}

	md := ChangeLogMarkdown(cl)
	if !strings.Contains(md, "# 4.18.2 → 4.18.3") {
		t.Errorf("expected header, got:\n%s", md)
	}
	if strings.Contains(md, "## Components") {
		t.Error("should not include Components section when empty")
	}
}

func TestRpmDiffMarkdown(t *testing.T) {
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

	md := RpmDiffMarkdown(diff)

	checks := []string{
		"## RPM Package Changes",
		"### Changed",
		"**kernel**: 5.14.0-427 → 5.14.0-428",
		"### Added",
		"**new-pkg**: 1.0.0",
		"### Removed",
		"**old-pkg**: 2.0.0",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\ngot:\n%s", want, md)
		}
	}
}

func TestRpmDiffMarkdown_Nil(t *testing.T) {
	if got := RpmDiffMarkdown(nil); got != "" {
		t.Errorf("expected empty string for nil diff, got: %q", got)
	}
}
