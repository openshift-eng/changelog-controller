package changelog_controller

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
)

const (
	rhelCoreOs         = "Red Hat Enterprise Linux CoreOS"
	rhelCoreOs10       = "Red Hat Enterprise Linux CoreOS 10"
	centosStreamCoreOs = "CentOS Stream CoreOS"

	rhcosServiceScheme = "https"
	rhcosServiceHost   = "releases-rhcos--prod-pipeline.apps.int.prod-stable-spoke1-dc-iad2.itup.redhat.com"

	// When CoreOS switched to a new bucket prefix
	changeoverTimestamp = 202212000000
)

var (
	// Regex for RHCOS in < 4.19, where the version string was e.g. 418.94.202410090804-0
	reOcpCoreOsVersion = regexp.MustCompile(`((\d)(\d+))\.(\d+)\.(\d+)-(\d+)`)

	// Regex for RHCOS in >= 4.19, where the version string was e.g. 9.6.20250121-0
	reRhelCoreOsVersion = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)-(\d+)`)
)

// rhcosArchitecture maps the arch query parameter to the architecture name
// and extension used by the RHCOS release browser. The release-controller
// uses the same mapping in http_changelog.go.
func rhcosArchitecture(arch string) (architecture, extension string) {
	switch arch {
	case "amd64":
		return "x86_64", ""
	case "arm64":
		architecture = "aarch64"
		return architecture, fmt.Sprintf("-%s", architecture)
	default:
		return arch, fmt.Sprintf("-%s", arch)
	}
}

// enrichCoreOSComponents adds RHCOS release browser URLs to CoreOS
// component entries in the changelog. Non-CoreOS components are unchanged.
func enrichCoreOSComponents(cl *changelog.ChangeLog, arch string) {
	if cl == nil || len(cl.Components) == 0 {
		return
	}

	architecture, extension := rhcosArchitecture(arch)

	for i, component := range cl.Components {
		switch component.Name {
		case rhelCoreOs, rhelCoreOs10, centosStreamCoreOs:
			cl.Components[i] = enrichCoreOSComponentJSON(component, architecture, extension)
		}
	}
}

// enrichCoreOSComponentJSON builds RHCOS release browser URLs for a single
// CoreOS component entry
func enrichCoreOSComponentJSON(component changelog.ComponentInfo, architecture, architectureExtension string) changelog.ComponentInfo {
	if len(component.Version) == 0 {
		return component
	}

	toStream, ok := getRHCoSReleaseStream(component.Version, architectureExtension)
	if !ok {
		return component
	}

	toURL := url.URL{
		Scheme:   rhcosServiceScheme,
		Host:     rhcosServiceHost,
		Path:     "/",
		Fragment: component.Version,
		RawQuery: (url.Values{
			"stream":  []string{toStream},
			"arch":    []string{architecture},
			"release": []string{component.Version},
		}).Encode(),
	}
	component.VersionURL = toURL.String()

	if len(component.From) > 0 {
		fromStream, ok := getRHCoSReleaseStream(component.From, architectureExtension)
		if !ok {
			return component
		}

		fromURL := url.URL{
			Scheme:   rhcosServiceScheme,
			Host:     rhcosServiceHost,
			Path:     "/",
			Fragment: component.From,
			RawQuery: (url.Values{
				"stream":  []string{fromStream},
				"arch":    []string{architecture},
				"release": []string{component.From},
			}).Encode(),
		}
		component.FromURL = fromURL.String()

		diffURL := url.URL{
			Scheme: rhcosServiceScheme,
			Host:   rhcosServiceHost,
			Path:   "/diff.html",
			RawQuery: (url.Values{
				"first_stream":   []string{fromStream},
				"first_release":  []string{component.From},
				"second_stream":  []string{toStream},
				"second_release": []string{component.Version},
				"arch":           []string{architecture},
			}).Encode(),
		}
		component.DiffURL = diffURL.String()
	}

	return component
}

// getRHCoSReleaseStream maps a CoreOS version string to the RHCOS release
// stream path used by the release browser
func getRHCoSReleaseStream(version, architectureExtension string) (string, bool) {
	if strings.HasPrefix(version, "4") {
		if m := reOcpCoreOsVersion.FindStringSubmatch(version); m != nil {
			ts, err := strconv.Atoi(m[5])
			if err != nil {
				return "", false
			}
			minor, err := strconv.Atoi(m[3])
			if err != nil {
				return "", false
			}
			switch {
			case ts > changeoverTimestamp && minor >= 9:
				versionMap := map[string]string{
					"92": "9.2",
					"94": "9.4",
					"96": "9.6",
				}
				if rhelVer, ok := versionMap[m[4]]; ok {
					return fmt.Sprintf("prod/streams/%s.%s-%s", m[2], m[3], rhelVer), true
				}
				return fmt.Sprintf("prod/streams/%s.%s", m[2], m[3]), true
			default:
				return fmt.Sprintf("releases/rhcos-%s.%s%s", m[2], m[3], architectureExtension), true
			}
		}
	} else {
		if m := reRhelCoreOsVersion.FindStringSubmatch(version); m != nil {
			return fmt.Sprintf("prod/streams/rhel-%s.%s", m[1], m[2]), true
		}
	}
	return "", false
}
