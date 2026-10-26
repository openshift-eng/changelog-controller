package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type Client struct {
	rcURL        string
	changelogURL string
	http         *http.Client
}

func NewClient(rcURL, changelogURL string, timeout time.Duration) *Client {
	return &Client{
		rcURL:        rcURL,
		changelogURL: changelogURL,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GetReleaseTags(stream string) (*APIRelease, error) {
	u := fmt.Sprintf("%s/api/v1/releasestream/%s/tags", c.rcURL, url.PathEscape(stream))
	var result APIRelease
	if err := c.getJSON(u, &result); err != nil {
		return nil, fmt.Errorf("fetching tags for stream %s: %w", stream, err)
	}
	return &result, nil
}

func (c *Client) GetReleaseInfo(stream, tag string) (*APIReleaseInfo, error) {
	u := fmt.Sprintf("%s/api/v1/releasestream/%s/release/%s",
		c.rcURL, url.PathEscape(stream), url.PathEscape(tag))
	var result APIReleaseInfo
	if err := c.getJSON(u, &result); err != nil {
		return nil, fmt.Errorf("fetching release info for %s/%s: %w", stream, tag, err)
	}
	return &result, nil
}

func (c *Client) GetChangelog(from, to, arch string) (*ChangelogResponse, error) {
	if arch == "" {
		arch = "amd64"
	}
	u := fmt.Sprintf("%s/changelog?from=%s&to=%s&arch=%s&format=json",
		c.changelogURL, url.QueryEscape(from), url.QueryEscape(to), url.QueryEscape(arch))
	var result ChangelogResponse
	if err := c.getJSON(u, &result); err != nil {
		return nil, fmt.Errorf("fetching changelog %s -> %s: %w", from, to, err)
	}
	return &result, nil
}

func (c *Client) getJSON(rawURL string, target any) error {
	resp, err := c.http.Get(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted {
		return ErrChangelogPending
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, rawURL, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// ErrChangelogPending indicates the changelog is queued for generation.
var ErrChangelogPending = fmt.Errorf("changelog generation in progress")

// GetReleaseStreams fetches the list of known release streams from the RC API.
// The "accepted" variant returns only streams with at least one accepted tag.
func (c *Client) GetReleaseStreams() ([]string, error) {
	u := fmt.Sprintf("%s/api/v1/releasestreams/accepted", c.rcURL)
	var result map[string]json.RawMessage
	if err := c.getJSON(u, &result); err != nil {
		return nil, fmt.Errorf("fetching release streams: %w", err)
	}
	streams := make([]string, 0, len(result))
	for name := range result {
		streams = append(streams, name)
	}
	sort.Strings(streams)
	return streams, nil
}

// GetQualifiers fetches qualifier badges for a specific release tag.
func (c *Client) GetQualifiers(tag string) (*QualifiersResponse, error) {
	u := fmt.Sprintf("%s/api/v1/releasetag/%s/qualifiers", c.rcURL, url.PathEscape(tag))
	var result QualifiersResponse
	if err := c.getJSON(u, &result); err != nil {
		return nil, fmt.Errorf("fetching qualifiers for %s: %w", tag, err)
	}
	return &result, nil
}
