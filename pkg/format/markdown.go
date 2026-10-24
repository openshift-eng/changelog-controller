package format

import (
	"fmt"
	"strings"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
)

// ChangeLogMarkdown renders a structured ChangeLog as markdown.
func ChangeLogMarkdown(cl *changelog.ChangeLog) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s → %s\n\n", cl.From.Name, cl.To.Name)

	if len(cl.Components) > 0 {
		b.WriteString("## Components\n\n")
		for _, c := range cl.Components {
			if c.From != "" {
				fmt.Fprintf(&b, "- **%s**: %s → %s", c.Name, c.From, c.Version)
			} else {
				fmt.Fprintf(&b, "- **%s**: %s", c.Name, c.Version)
			}
			if c.DiffURL != "" {
				fmt.Fprintf(&b, " ([diff](%s))", c.DiffURL)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	writeImageSection(&b, "New Images", cl.NewImages)
	writeImageSection(&b, "Removed Images", cl.RemovedImages)
	writeImageSection(&b, "Rebuilt Images", cl.RebuiltImages)
	writeImageSection(&b, "Updated Images", cl.UpdatedImages)

	return b.String()
}

func writeImageSection(b *strings.Builder, title string, images []changelog.ImageInfo) {
	if len(images) == 0 {
		return
	}

	fmt.Fprintf(b, "## %s\n\n", title)
	for _, img := range images {
		if img.Path != "" {
			fmt.Fprintf(b, "### [%s](%s)\n\n", img.Name, img.Path)
		} else {
			fmt.Fprintf(b, "### %s\n\n", img.Name)
		}
		for _, c := range img.Commits {
			if c.PullURL != "" {
				fmt.Fprintf(b, "- [%s](%s)", c.Subject, c.PullURL)
			} else {
				fmt.Fprintf(b, "- %s", c.Subject)
			}
			for id, url := range c.Bugs {
				fmt.Fprintf(b, " [%s](%s)", id, url)
			}
			b.WriteString("\n")
		}
		if img.FullChangeLog != "" {
			fmt.Fprintf(b, "\n[Full changelog](%s)\n", img.FullChangeLog)
		}
		b.WriteString("\n")
	}
}

// RpmDiffMarkdown renders an RpmDiff as a markdown section.
func RpmDiffMarkdown(diff *changelog.RpmDiff) string {
	if diff == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("## RPM Package Changes\n\n")

	if len(diff.Changed) > 0 {
		b.WriteString("### Changed\n\n")
		for pkg, d := range diff.Changed {
			fmt.Fprintf(&b, "- **%s**: %s → %s\n", pkg, d.Old, d.New)
		}
		b.WriteString("\n")
	}

	if len(diff.Added) > 0 {
		b.WriteString("### Added\n\n")
		for pkg, ver := range diff.Added {
			fmt.Fprintf(&b, "- **%s**: %s\n", pkg, ver)
		}
		b.WriteString("\n")
	}

	if len(diff.Removed) > 0 {
		b.WriteString("### Removed\n\n")
		for pkg, ver := range diff.Removed {
			fmt.Fprintf(&b, "- **%s**: %s\n", pkg, ver)
		}
		b.WriteString("\n")
	}

	return b.String()
}
