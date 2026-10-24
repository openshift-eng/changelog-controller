package main

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
)

const (
	stateSucceeded = "Succeeded"
	stateFailed    = "Failed"
	statePending   = "Pending"
)

const htmlPageStart = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8"><title>%s</title>
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/bootstrap/4.6.1/css/bootstrap.min.css" integrity="sha512-T584yQ/tdRR5QwOpfvDfVQUidzfgc2339Lc8uBDtcp/wYu80d7jwBgAxbyMh0a9YM9F8N3tdErpFI8iaGx6x5g==" crossorigin="anonymous">
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/bootstrap-icons/1.5.0/font/bootstrap-icons.min.css">
    <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
    <style>
        @media (max-width: 992px) {
            .container {
                width: 100%%;
                max-width: none;
            }
        }
    </style>
</head>
<body>
<div class="container">
`

const htmlPageEnd = `<p class="small">Source code for this page located on <a href="https://github.com/openshift-eng/changelog-controller">github</a></p>
<p class="small text-muted">Rendered by changelog-client (changelog-controller client)</p>
</div>
</body>
</html>`

const releasePageStyle = `
<style>
    h1 { font-size: 2rem; margin-bottom: 1rem }
    h2 { font-size: 1.5rem; margin-top: 2rem; margin-bottom: 1rem }
    h3 { font-size: 1.35rem; margin-top: 2rem; margin-bottom: 1rem }
    h4 { font-size: 1.2rem; margin-top: 2rem; margin-bottom: 1rem }
    .alert h4.alert-heading { margin-top: 0; }
    h3 a { text-transform: uppercase; font-size: 1rem; }
    .mb-custom {
      margin-bottom: 0.5rem !important;
    }
    table, th, td {
      border: 1px solid;
      padding: 5px;
    }
</style>
`

func renderReleasePage(w io.Writer, tag, stream, predecessor string, tags *APIRelease, info *APIReleaseInfo, qualifiers *QualifiersResponse, changelog *ChangelogResponse, changelogPending bool) {
	fmt.Fprintf(w, htmlPageStart, template.HTMLEscapeString(fmt.Sprintf("Release %s", tag)))
	fmt.Fprint(w, releasePageStyle)

	fmt.Fprintf(w, "<p><a href=\"/\">Back to index</a></p>\n")

	// Title
	fmt.Fprintf(w, `<div class="mb-custom">`+
		`<div class="row align-items-center">`+
		`<div class="col">`+
		`<h1 class="m-0">%s</h1>`+
		`</div>`+
		`</div>`+
		`</div>`, template.HTMLEscapeString(tag))

	// Download link and install instructions
	if tags != nil {
		renderDownloadAndInstall(w, tags, tag)
	}

	// Qualifiers
	if qualifiers != nil && len(qualifiers.Qualifiers) > 0 {
		renderQualifiers(w, tag, qualifiers)
	}

	// Test results
	if info != nil && info.Results != nil {
		renderTestResults(w, info.Results)
	}

	// Upgrades from
	if info != nil && len(info.UpgradesTo) > 0 {
		renderUpgradesFrom(w, stream, info.UpgradesTo, predecessor)
	}

	// Upgrades to
	if info != nil && len(info.UpgradesFrom) > 0 {
		renderUpgradesTo(w, stream, info.UpgradesFrom)
	}

	// Changelog (from changelog controller)
	if changelog != nil && changelog.Changelog != nil {
		fmt.Fprintln(w, "<hr>")
		renderChangelog(w, predecessor, changelog)
	} else if predecessor != "" {
		fmt.Fprintln(w, "<hr>")
		if changelogPending {
			fmt.Fprintf(w, `<p class="alert alert-warning">Changelog for %s &rarr; %s is being generated. Refresh this page in a few minutes.</p>`,
				template.HTMLEscapeString(predecessor), template.HTMLEscapeString(tag))
		} else {
			fmt.Fprintf(w, `<p class="alert alert-info">Changelog data for changes from %s is not yet available.</p>`,
				template.HTMLEscapeString(predecessor))
		}
	}

	// Previous release selector
	if tags != nil && len(tags.Tags) > 1 {
		renderPreviousReleaseSelector(w, tag, stream, predecessor, tags)
	}

	fmt.Fprintln(w, htmlPageEnd)
}

func renderDownloadAndInstall(w io.Writer, tags *APIRelease, tag string) {
	var pullSpec, downloadURL string
	for _, t := range tags.Tags {
		if t.Name == tag {
			pullSpec = t.PullSpec
			downloadURL = t.DownloadURL
			break
		}
	}
	if pullSpec == "" {
		return
	}

	fmt.Fprint(w, "<p>")
	if downloadURL != "" {
		fmt.Fprintf(w, `<a href="%s">Download the installer</a> for your operating system or run `,
			template.HTMLEscapeString(downloadURL))
	} else {
		fmt.Fprint(w, "Download the installer for your operating system or run ")
	}
	fmt.Fprintf(w, `<pre class="ml-4">oc adm release extract --tools %s</pre>`,
		template.HTMLEscapeString(pullSpec))
}

func renderQualifiers(w io.Writer, tag string, q *QualifiersResponse) {
	fmt.Fprintf(w, `<p style="margin-bottom:0"><strong>Qualifiers</strong> (<a href="/api/v1/releasetag/%s/qualifiers">status api</a>):</p>`,
		template.HTMLEscapeString(tag))
	fmt.Fprint(w, `<div style="margin-left:40px;margin-bottom:16px">`)
	for _, qual := range q.Qualifiers {
		if !qual.BadgeEarned {
			continue
		}
		badgeClass := "badge-success"
		if qual.AggregateState == "Failed" {
			badgeClass = "badge-danger"
		}
		fmt.Fprintf(w, `<span class="badge %s qualifier-badge">%s</span> `,
			badgeClass, template.HTMLEscapeString(qual.BadgeName))
	}
	fmt.Fprint(w, "</div>\n")
}

func renderTestResults(w io.Writer, results *VerificationJobsSummary) {
	if results.BlockingJobs == nil && results.InformingJobs == nil {
		return
	}

	fmt.Fprint(w, "<p><strong>Tests:</strong></p>\n")

	if len(results.BlockingJobs) > 0 {
		fmt.Fprint(w, "<ul><li>Blocking jobs<ul>\n")
		renderJobList(w, results.BlockingJobs)
		fmt.Fprint(w, "</ul></li></ul>\n")
	}

	if len(results.InformingJobs) > 0 {
		fmt.Fprint(w, "<ul><li>Informing jobs<ul>\n")
		renderJobList(w, results.InformingJobs)
		fmt.Fprint(w, "</ul></li></ul>\n")
	}
}

func renderJobList(w io.Writer, jobs VerificationStatusMap) {
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		status := jobs[name]
		var style string
		switch status.State {
		case stateSucceeded:
			style = "text-success"
		case stateFailed:
			style = "text-danger"
		default:
			style = ""
		}

		if status.Retries > 0 && len(status.PreviousAttemptURLs) > 0 {
			retryLabel := "retry"
			if status.Retries > 1 {
				retryLabel = "retries"
			}
			fmt.Fprintf(w, `<li><details><summary style="cursor:pointer"><span class="%s">%s %s</span> <span class="text-warning">(%d %s)</span> %s</summary><ul>`,
				style, template.HTMLEscapeString(name),
				template.HTMLEscapeString(status.State),
				status.Retries, retryLabel,
				template.HTMLEscapeString(shortJobName(status.URL)))

			for i, prevURL := range status.PreviousAttemptURLs {
				_ = i
				fmt.Fprintf(w, `<li><a class="text-danger" href="%s">Attempt %d Failed</a></li>`,
					template.HTMLEscapeString(prevURL), i+1)
			}
			if status.URL != "" {
				finalStyle := "text-danger"
				finalState := stateFailed
				if status.State == stateSucceeded {
					finalStyle = "text-success"
					finalState = stateSucceeded
				}
				fmt.Fprintf(w, `<li><a class="%s" href="%s">Attempt %d %s</a></li>`,
					finalStyle, template.HTMLEscapeString(status.URL),
					len(status.PreviousAttemptURLs)+1, finalState)
			}
			fmt.Fprint(w, "</ul></details></li>\n")
		} else {
			if status.URL != "" {
				fmt.Fprintf(w, `<li><a class="%s" href="%s">%s %s</a> %s</li>`+"\n",
					style, template.HTMLEscapeString(status.URL),
					template.HTMLEscapeString(name),
					template.HTMLEscapeString(status.State),
					template.HTMLEscapeString(shortJobName(status.URL)))
			} else {
				fmt.Fprintf(w, `<li><span class="%s">%s %s</span></li>`+"\n",
					style, template.HTMLEscapeString(name),
					template.HTMLEscapeString(status.State))
			}
		}
	}
}

func renderUpgradesFrom(w io.Writer, stream string, upgrades []UpgradeHistory, predecessor string) {
	fmt.Fprint(w, `<p id="upgrades-from">Upgrades from:</p><ul>`)
	for _, u := range upgrades {
		var style string
		switch {
		case u.Success == 0 && u.Failure > 0:
			style = "text-danger"
		case u.Success > 0:
			style = "text-success"
		}

		fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasestream/%s/release/%s">%s</a>`,
			style,
			template.HTMLEscapeString(stream),
			template.HTMLEscapeString(u.From),
			template.HTMLEscapeString(u.From))

		if predecessor == "" || u.From != predecessor {
			fmt.Fprintf(w, ` (<a href="/releasestream/%s/release/%s?from=%s">changes</a>)`,
				template.HTMLEscapeString(stream),
				template.HTMLEscapeString(u.To),
				template.HTMLEscapeString(u.From))
		}

		if u.Total > 0 {
			fmt.Fprint(w, " - ")
			renderUpgradeResults(w, u.History)
		}

		fmt.Fprint(w, "</li>")
	}
	fmt.Fprint(w, "</ul>")
}

func renderUpgradesTo(w io.Writer, stream string, upgrades []UpgradeHistory) {
	fmt.Fprintf(w, `<p id="upgrades-to">Upgrades to:</p><ul>`)
	for _, u := range upgrades {
		var style string
		switch {
		case u.Success == 0 && u.Failure > 0:
			style = "text-danger"
		case u.Success > 0:
			style = "text-success"
		}

		fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasestream/%s/release/%s">%s</a>`,
			style,
			template.HTMLEscapeString(stream),
			template.HTMLEscapeString(u.To),
			template.HTMLEscapeString(u.To))

		fmt.Fprintf(w, ` (<a href="/releasestream/%s/release/%s?from=%s">changes</a>)`,
			template.HTMLEscapeString(stream),
			template.HTMLEscapeString(u.To),
			template.HTMLEscapeString(u.From))

		if u.Total > 0 {
			fmt.Fprint(w, " - ")
			renderUpgradeResults(w, u.History)
		}

		fmt.Fprint(w, "</li>")
	}
	fmt.Fprint(w, "</ul>")
}

func renderUpgradeResults(w io.Writer, history map[string]UpgradeResult) {
	urls := make([]string, 0, len(history))
	for u := range history {
		urls = append(urls, u)
	}
	sort.Strings(urls)

	compact := len(urls) > 2
	for _, u := range urls {
		result := history[u]
		switch result.State {
		case stateSucceeded:
			if compact {
				fmt.Fprintf(w, ` <a class="text-success" href="%s">S</a>`, template.HTMLEscapeString(u))
			} else {
				fmt.Fprintf(w, ` <a class="text-success" href="%s">Success</a>`, template.HTMLEscapeString(u))
			}
		case stateFailed:
			if compact {
				fmt.Fprintf(w, ` <a class="text-danger" href="%s">F</a>`, template.HTMLEscapeString(u))
			} else {
				fmt.Fprintf(w, ` <a class="text-danger" href="%s">Failed</a>`, template.HTMLEscapeString(u))
			}
		default:
			if compact {
				fmt.Fprintf(w, ` <a href="%s">P</a>`, template.HTMLEscapeString(u))
			} else {
				fmt.Fprintf(w, ` <a href="%s">Pending</a>`, template.HTMLEscapeString(u))
			}
		}
	}
}

func renderChangelog(w io.Writer, predecessor string, resp *ChangelogResponse) {
	cl := resp.Changelog

	fmt.Fprintf(w, `<h2>Changes from <a target="_blank" href="/releasetag/%s">%s</a></h2>`+"\n",
		template.HTMLEscapeString(predecessor), template.HTMLEscapeString(predecessor))

	if !cl.To.Created.IsZero() {
		fmt.Fprintf(w, "<p>Created: %s</p>\n", cl.To.Created.UTC().Format("2006-01-02 15:04:05 +0000 UTC"))
	}
	if cl.To.Digest != "" {
		fmt.Fprintf(w, "<p>Image Digest: <code>%s</code></p>\n", template.HTMLEscapeString(cl.To.Digest))
	}

	// Components
	if len(cl.Components) > 0 {
		fmt.Fprint(w, "<h3>Components</h3>\n<ul>\n")
		for _, c := range cl.Components {
			if c.From != "" {
				if c.DiffURL != "" {
					fmt.Fprintf(w, "<li>%s upgraded from ", template.HTMLEscapeString(c.Name))
					if c.FromURL != "" {
						fmt.Fprintf(w, `<a target="_blank" href="%s">%s</a>`, template.HTMLEscapeString(c.FromURL), template.HTMLEscapeString(c.From))
					} else {
						fmt.Fprint(w, template.HTMLEscapeString(c.From))
					}
					fmt.Fprint(w, " to ")
					if c.VersionURL != "" {
						fmt.Fprintf(w, `<a target="_blank" href="%s">%s</a>`, template.HTMLEscapeString(c.VersionURL), template.HTMLEscapeString(c.Version))
					} else {
						fmt.Fprint(w, template.HTMLEscapeString(c.Version))
					}
					fmt.Fprintf(w, ` (<a target="_blank" href="%s">diff</a>)`, template.HTMLEscapeString(c.DiffURL))
					fmt.Fprint(w, "</li>\n")
				} else {
					fmt.Fprintf(w, "<li>%s %s</li>\n",
						template.HTMLEscapeString(c.Name), template.HTMLEscapeString(c.Version))
				}
			} else {
				fmt.Fprintf(w, "<li>%s %s</li>\n",
					template.HTMLEscapeString(c.Name), template.HTMLEscapeString(c.Version))
			}
		}
		fmt.Fprint(w, "</ul>\n")
	}

	// Rebuilt images
	if len(cl.RebuiltImages) > 0 {
		fmt.Fprint(w, "<h3>Rebuilt images without code change</h3>\n<ul>\n")
		for _, img := range cl.RebuiltImages {
			fmt.Fprint(w, "<li>")
			if img.Path != "" {
				fmt.Fprintf(w, `<a target="_blank" href="%s">%s</a>`,
					template.HTMLEscapeString(img.Path), template.HTMLEscapeString(img.Name))
			} else {
				fmt.Fprint(w, template.HTMLEscapeString(img.Name))
			}
			if img.ShortCommit != "" {
				if img.Path != "" && img.Commit != "" {
					fmt.Fprintf(w, ` git <a target="_blank" href="%s/commit/%s">%s</a>`,
						template.HTMLEscapeString(img.Path),
						template.HTMLEscapeString(img.Commit),
						template.HTMLEscapeString(img.ShortCommit))
				} else {
					fmt.Fprintf(w, " git %s", template.HTMLEscapeString(img.ShortCommit))
				}
			}
			if img.ImageRef != "" {
				fmt.Fprintf(w, " <code>%s</code>", template.HTMLEscapeString(img.ImageRef))
			}
			fmt.Fprint(w, "</li>\n")
		}
		fmt.Fprint(w, "</ul>\n")
	}

	// New images
	if len(cl.NewImages) > 0 {
		fmt.Fprint(w, "<h3>New images</h3>\n<ul>\n")
		for _, img := range cl.NewImages {
			fmt.Fprintf(w, "<li>%s</li>\n", template.HTMLEscapeString(img.Name))
		}
		fmt.Fprint(w, "</ul>\n")
	}

	// Removed images
	if len(cl.RemovedImages) > 0 {
		fmt.Fprint(w, "<h3>Removed images</h3>\n<ul>\n")
		for _, img := range cl.RemovedImages {
			fmt.Fprintf(w, "<li>%s</li>\n", template.HTMLEscapeString(img.Name))
		}
		fmt.Fprint(w, "</ul>\n")
	}

	// Updated images (per-component commits)
	for _, img := range cl.UpdatedImages {
		if len(img.Commits) == 0 {
			continue
		}
		if img.Path != "" {
			fmt.Fprintf(w, "<h3><a target=\"_blank\" href=\"%s\">%s</a></h3>\n",
				template.HTMLEscapeString(img.Path),
				template.HTMLEscapeString(img.Name))
		} else {
			fmt.Fprintf(w, "<h3>%s</h3>\n", template.HTMLEscapeString(img.Name))
		}
		fmt.Fprint(w, "<ul>\n")
		for _, commit := range img.Commits {
			renderCommit(w, commit)
		}
		if img.FullChangeLog != "" {
			fmt.Fprintf(w, `<li><a target="_blank" href="%s">Full changelog</a></li>`+"\n",
				template.HTMLEscapeString(img.FullChangeLog))
		}
		fmt.Fprint(w, "</ul>\n")
	}

	// RPM diff
	if resp.RpmDiff != nil {
		renderRpmDiff(w, resp.RpmDiff)
	}

	fmt.Fprintln(w, "<hr>")
}

func renderCommit(w io.Writer, c CommitInfo) {
	fmt.Fprint(w, "<li>")

	// Bug references
	for id, bugURL := range c.Bugs {
		fmt.Fprintf(w, `<a target="_blank" href="%s">%s</a>: `,
			template.HTMLEscapeString(bugURL), template.HTMLEscapeString(id))
	}

	// Subject
	fmt.Fprint(w, template.HTMLEscapeString(c.Subject))

	// PR link
	if c.PullURL != "" {
		fmt.Fprintf(w, ` <a target="_blank" href="%s">#%d</a>`,
			template.HTMLEscapeString(c.PullURL), c.PullID)
	}

	fmt.Fprint(w, "</li>\n")
}

func renderRpmDiff(w io.Writer, diff *RpmDiff) {
	if len(diff.Changed) == 0 && len(diff.Added) == 0 && len(diff.Removed) == 0 {
		return
	}

	fmt.Fprint(w, "<h2>RPM Package Changes</h2>\n")

	if len(diff.Changed) > 0 {
		fmt.Fprint(w, "<h3>Changed</h3>\n<ul>\n")
		for pkg, d := range diff.Changed {
			fmt.Fprintf(w, "<li><strong>%s</strong>: %s &rarr; %s</li>\n",
				template.HTMLEscapeString(pkg),
				template.HTMLEscapeString(d.Old),
				template.HTMLEscapeString(d.New))
		}
		fmt.Fprint(w, "</ul>\n")
	}

	if len(diff.Added) > 0 {
		fmt.Fprint(w, "<h3>Added</h3>\n<ul>\n")
		for pkg, ver := range diff.Added {
			fmt.Fprintf(w, "<li><strong>%s</strong>: %s</li>\n",
				template.HTMLEscapeString(pkg), template.HTMLEscapeString(ver))
		}
		fmt.Fprint(w, "</ul>\n")
	}

	if len(diff.Removed) > 0 {
		fmt.Fprint(w, "<h3>Removed</h3>\n<ul>\n")
		for pkg, ver := range diff.Removed {
			fmt.Fprintf(w, "<li><strong>%s</strong>: %s</li>\n",
				template.HTMLEscapeString(pkg), template.HTMLEscapeString(ver))
		}
		fmt.Fprint(w, "</ul>\n")
	}
}

func renderPreviousReleaseSelector(w io.Writer, tag, stream, predecessor string, tags *APIRelease) {
	sorted := sortTagsDescending(tags.Tags)

	var options []string
	for _, t := range sorted {
		if t.Name == tag {
			continue
		}
		selected := ""
		if t.Name == predecessor {
			selected = ` selected="true"`
		}
		options = append(options, fmt.Sprintf(`<option%s>%s</option>`, selected, template.HTMLEscapeString(t.Name)))
	}

	if len(options) == 0 {
		return
	}

	fmt.Fprint(w, `<div class="alert alert-info"><form class="form-inline" method="GET">`)
	if predecessor != "" {
		fmt.Fprintf(w, `<a href="/changelog?from=%s&to=%s">View changelog in Markdown</a>`+
			`<span>&nbsp;or&nbsp;</span><label for="from">change previous release:&nbsp;</label>`,
			template.HTMLEscapeString(predecessor),
			template.HTMLEscapeString(tag))
	} else {
		fmt.Fprint(w, `<label for="from">change previous release:&nbsp;</label>`)
	}
	fmt.Fprintf(w, `<select onchange="this.form.submit()" id="from" class="form-control" name="from">%s</select> `+
		`<input class="btn btn-link" type="submit" value="Compare">`, strings.Join(options, ""))
	fmt.Fprint(w, `</form></div>`)
}

func renderIndexPage(w io.Writer, rcURL string, streams []string) {
	fmt.Fprintf(w, htmlPageStart, "Release Controller Lite")

	fmt.Fprint(w, "<h1>Release Controller Lite</h1>\n")
	fmt.Fprintf(w, `<p class="text-muted">A lightweight client that renders changelog data from the `+
		`<a href="https://changelog.apps.engineering.openshift.org">changelog-controller</a>, `+
		`using release metadata from the <a href="%s">release-controller</a>.</p>`+"\n",
		template.HTMLEscapeString(rcURL))

	fmt.Fprint(w, "<h2>Release Streams</h2>\n<ul>\n")
	for _, s := range streams {
		fmt.Fprintf(w, `<li><a href="/releasestream/%s">%s</a></li>`+"\n",
			template.HTMLEscapeString(s), template.HTMLEscapeString(s))
	}
	fmt.Fprint(w, "</ul>\n")

	fmt.Fprintln(w, htmlPageEnd)
}

func renderStreamPage(w io.Writer, stream string, tags *APIRelease) {
	fmt.Fprintf(w, htmlPageStart, template.HTMLEscapeString(fmt.Sprintf("Stream %s", stream)))

	fmt.Fprintf(w, "<p><a href=\"/\">Back to index</a></p>\n")
	fmt.Fprintf(w, "<h1>%s</h1>\n", template.HTMLEscapeString(stream))

	if tags == nil || len(tags.Tags) == 0 {
		fmt.Fprint(w, `<p class="alert alert-warning">No tags found for this stream.</p>`)
		fmt.Fprintln(w, htmlPageEnd)
		return
	}

	sorted := sortTagsDescending(tags.Tags)

	fmt.Fprint(w, "<table class=\"table table-sm\">\n<thead><tr><th>Tag</th><th>Phase</th></tr></thead>\n<tbody>\n")
	for _, t := range sorted {
		var style string
		switch t.Phase {
		case "Accepted":
			style = "text-success"
		case "Failed":
			style = "text-danger"
		case "Rejected":
			style = "text-danger"
		}
		fmt.Fprintf(w, `<tr><td><a class="%s" href="/releasestream/%s/release/%s">%s</a></td><td class="%s">%s</td></tr>`+"\n",
			style,
			template.HTMLEscapeString(stream),
			template.HTMLEscapeString(t.Name),
			template.HTMLEscapeString(t.Name),
			style,
			template.HTMLEscapeString(t.Phase))
	}
	fmt.Fprint(w, "</tbody>\n</table>\n")

	fmt.Fprintln(w, htmlPageEnd)
}

// sortTagsDescending returns a copy of tags sorted from latest to oldest
// using reverse lexicographic order on the tag name. This works for
// OpenShift version strings (e.g., 4.19.1 > 4.19.0 > 4.18.51).
func sortTagsDescending(tags []APITag) []APITag {
	sorted := make([]APITag, len(tags))
	copy(sorted, tags)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name > sorted[j].Name
	})
	return sorted
}

func shortJobName(u string) string {
	parts := strings.Split(u, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return u
}
