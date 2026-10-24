package changelog_controller

import (
	"strings"
	"testing"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
)

func TestRhcosArchitecture(t *testing.T) {
	tests := []struct {
		arch          string
		wantArch      string
		wantExtension string
	}{
		{"amd64", "x86_64", ""},
		{"arm64", "aarch64", "-aarch64"},
		{"s390x", "s390x", "-s390x"},
		{"ppc64le", "ppc64le", "-ppc64le"},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			arch, ext := rhcosArchitecture(tt.arch)
			if arch != tt.wantArch {
				t.Errorf("architecture = %q, want %q", arch, tt.wantArch)
			}
			if ext != tt.wantExtension {
				t.Errorf("extension = %q, want %q", ext, tt.wantExtension)
			}
		})
	}
}

func TestGetRHCoSReleaseStream(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		archExt    string
		wantStream string
		wantOK     bool
	}{
		{
			name:       "old format pre-changeover",
			version:    "415.92.202211010000-0",
			archExt:    "",
			wantStream: "releases/rhcos-4.15",
			wantOK:     true,
		},
		{
			name:       "old format post-changeover with 9.4 minor",
			version:    "418.94.202501010000-0",
			archExt:    "",
			wantStream: "prod/streams/4.18-9.4",
			wantOK:     true,
		},
		{
			name:       "old format post-changeover generic minor",
			version:    "418.98.202501010000-0",
			archExt:    "",
			wantStream: "prod/streams/4.18",
			wantOK:     true,
		},
		{
			name:       "new RHEL format",
			version:    "9.6.20250121-0",
			archExt:    "",
			wantStream: "prod/streams/rhel-9.6",
			wantOK:     true,
		},
		{
			name:       "old format with arm64 extension",
			version:    "415.92.202211010000-0",
			archExt:    "-aarch64",
			wantStream: "releases/rhcos-4.15-aarch64",
			wantOK:     true,
		},
		{
			name:    "invalid version",
			version: "not-a-version",
			archExt: "",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream, ok := getRHCoSReleaseStream(tt.version, tt.archExt)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && stream != tt.wantStream {
				t.Errorf("stream = %q, want %q", stream, tt.wantStream)
			}
		})
	}
}

func TestEnrichCoreOSComponents(t *testing.T) {
	t.Run("enriches RHCOS component", func(t *testing.T) {
		cl := &changelog.ChangeLog{
			Components: []changelog.ComponentInfo{
				{
					Name:    "Red Hat Enterprise Linux CoreOS",
					Version: "418.94.202501010000-0",
					From:    "418.94.202412010000-0",
				},
				{
					Name:    "Kubernetes",
					Version: "v1.31.0",
					From:    "v1.30.0",
				},
			},
		}

		enrichCoreOSComponents(cl, "amd64")

		// RHCOS component should have URLs
		rhcos := cl.Components[0]
		if rhcos.VersionURL == "" {
			t.Error("expected VersionURL to be set for RHCOS component")
		}
		if !strings.Contains(rhcos.VersionURL, "releases-rhcos") {
			t.Errorf("VersionURL should point to RHCOS release browser, got %q", rhcos.VersionURL)
		}
		if rhcos.FromURL == "" {
			t.Error("expected FromURL to be set for RHCOS component")
		}
		if rhcos.DiffURL == "" {
			t.Error("expected DiffURL to be set for RHCOS component")
		}
		if !strings.Contains(rhcos.DiffURL, "diff.html") {
			t.Errorf("DiffURL should point to diff page, got %q", rhcos.DiffURL)
		}

		// Kubernetes component should be unchanged
		k8s := cl.Components[1]
		if k8s.VersionURL != "" {
			t.Errorf("non-CoreOS component should not have VersionURL, got %q", k8s.VersionURL)
		}
		if k8s.FromURL != "" {
			t.Errorf("non-CoreOS component should not have FromURL, got %q", k8s.FromURL)
		}
	})

	t.Run("handles nil changelog", func(t *testing.T) {
		enrichCoreOSComponents(nil, "amd64")
	})

	t.Run("handles empty components", func(t *testing.T) {
		cl := &changelog.ChangeLog{}
		enrichCoreOSComponents(cl, "amd64")
	})

	t.Run("handles version-only component (no from)", func(t *testing.T) {
		cl := &changelog.ChangeLog{
			Components: []changelog.ComponentInfo{
				{
					Name:    "Red Hat Enterprise Linux CoreOS",
					Version: "9.6.20250121-0",
				},
			},
		}

		enrichCoreOSComponents(cl, "amd64")

		rhcos := cl.Components[0]
		if rhcos.VersionURL == "" {
			t.Error("expected VersionURL to be set")
		}
		if rhcos.FromURL != "" {
			t.Error("expected FromURL to be empty when no From version")
		}
		if rhcos.DiffURL != "" {
			t.Error("expected DiffURL to be empty when no From version")
		}
	})

	t.Run("uses correct architecture in URLs", func(t *testing.T) {
		cl := &changelog.ChangeLog{
			Components: []changelog.ComponentInfo{
				{
					Name:    "Red Hat Enterprise Linux CoreOS",
					Version: "9.6.20250121-0",
				},
			},
		}

		enrichCoreOSComponents(cl, "arm64")

		rhcos := cl.Components[0]
		if !strings.Contains(rhcos.VersionURL, "aarch64") {
			t.Errorf("expected URL to contain aarch64 for arm64, got %q", rhcos.VersionURL)
		}
	})

	t.Run("enriches CentOS Stream CoreOS", func(t *testing.T) {
		cl := &changelog.ChangeLog{
			Components: []changelog.ComponentInfo{
				{
					Name:    "CentOS Stream CoreOS",
					Version: "418.94.202501010000-0",
				},
			},
		}

		enrichCoreOSComponents(cl, "amd64")

		if cl.Components[0].VersionURL == "" {
			t.Error("expected VersionURL to be set for CentOS Stream CoreOS")
		}
	})

	t.Run("enriches RHCOS 10", func(t *testing.T) {
		cl := &changelog.ChangeLog{
			Components: []changelog.ComponentInfo{
				{
					Name:    "Red Hat Enterprise Linux CoreOS 10",
					Version: "10.1.20260101-0",
				},
			},
		}

		enrichCoreOSComponents(cl, "amd64")

		if cl.Components[0].VersionURL == "" {
			t.Error("expected VersionURL to be set for RHCOS 10")
		}
	})
}
