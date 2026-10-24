package format

import (
	"bytes"

	"github.com/openshift-eng/changelog-controller/pkg/changelog"
	"github.com/yuin/goldmark"
)

// ChangeLogHTML renders a structured ChangeLog as HTML.
func ChangeLogHTML(cl *changelog.ChangeLog) (string, error) {
	return markdownToHTML(ChangeLogMarkdown(cl))
}

// RpmDiffHTML renders an RpmDiff as HTML.
func RpmDiffHTML(diff *changelog.RpmDiff) (string, error) {
	return markdownToHTML(RpmDiffMarkdown(diff))
}

func markdownToHTML(md string) (string, error) {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
