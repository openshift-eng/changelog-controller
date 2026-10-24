# Release Controller Client

A Go client library for interacting with OpenShift release-controller endpoints.

## Overview

This package provides a structured client for accessing release-controller APIs across different architectures and products. The release-controller endpoints follow the pattern:

```
https://<ARCHITECTURE>.<PRODUCT>.releases.ci.openshift.org/
```

## Supported Configurations

### Architectures
- `amd64` - x86_64/AMD64
- `arm64` - ARM64/aarch64
- `s390x` - IBM System z
- `ppc64le` - PowerPC 64-bit Little Endian
- `multi` - Multi-architecture

### Products
- `ocp` - OpenShift Container Platform (supports all architectures)
- `origin` - Origin (supports **only** amd64)

## API Methods

### Release Streams API

The `/api/v1/releasestreams/all` endpoint returns all release streams with their list of releases. The response is a map of stream names to arrays of release names.

#### GetAllReleaseStreams
Fetches all release streams with their releases:

```go
allStreams, err := client.GetAllReleaseStreams(ctx)
if err != nil {
    log.Fatal(err)
}

// Iterate over each stream
// allStreams is map[string][]string
for streamName, releases := range allStreams {
    fmt.Printf("Stream: %s (%d releases)\n", streamName, len(releases))
    
    // releases is []string - ordered with most recent first
    for i, releaseName := range releases {
        fmt.Printf("  [%d] %s\n", i, releaseName)
    }
}
```

#### GetReleaseStreamNames
Get just the names of available release streams (sorted):

```go
names, err := client.GetReleaseStreamNames(ctx)
if err != nil {
    log.Fatal(err)
}

for _, name := range names {
    fmt.Println(name)  // e.g., "4-stable", "4.14.0-0.nightly", "5-dev-preview"
}
```

#### GetReleasesForStream
Get all release names for a specific stream:

```go
releases, err := client.GetReleasesForStream(ctx, "4-stable")
if err != nil {
    log.Fatal(err)
}

// releases is []string - most recent first
for i, releaseName := range releases {
    fmt.Printf("[%d] %s\n", i, releaseName)
}
```

#### GetLatestReleaseForStream
Get the most recent release for a stream:

```go
latest, err := client.GetLatestReleaseForStream(ctx, "4-stable")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Latest stable: %s\n", latest)
// Output: Latest stable: 4.21.10
```

#### GetStreamStats
Get statistics for all streams:

```go
stats, err := client.GetStreamStats(ctx)
if err != nil {
    log.Fatal(err)
}

for _, stat := range stats {
    fmt.Printf("%s: %d releases, latest: %s\n", 
        stat.Name, stat.ReleaseCount, stat.LatestRelease)
}
```

#### FindRelease
Find which streams contain a specific release:

```go
locations, err := client.FindRelease(ctx, "4.13.0-rc.8")
if err != nil {
    log.Fatal(err)
}

for _, loc := range locations {
    fmt.Printf("%s/%s: %s\n", 
        loc.Endpoint.Product, loc.Endpoint.Architecture, loc.StreamName)
}
// Output: ocp/amd64: 4-stable
```

#### FindReleaseAcrossAll (MultiClient)
Find a release across all endpoints:

```go
mc, _ := rc.NewMultiClient(rc.DefaultClientConfig())
defer mc.Close()

locations, _ := mc.FindReleaseAcrossAll(ctx, "4.21.10")
for _, loc := range locations {
    fmt.Printf("%s/%s: %s\n", 
        loc.Endpoint.Product, loc.Endpoint.Architecture,
        loc.StreamName)
}
// Output: ocp/amd64: 4-stable
//         ocp/arm64: 4-stable-arm64
//         ...
```

#### GetReleaseInfo
Get detailed information about a specific release:

```go
info, err := client.GetReleaseInfo(ctx, "4-stable", "4.21.10")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Release: %s\n", info.Name)
fmt.Printf("Phase: %s\n", info.Phase)

// Access changelog information
if info.ChangeLogJson != nil {
    if info.ChangeLogJson.From != nil && info.ChangeLogJson.To != nil {
        fmt.Printf("Changelog: %s → %s\n", 
            info.ChangeLogJson.From.Name, 
            info.ChangeLogJson.To.Name)
    }
    
    // Iterate over updated components
    for _, comp := range info.ChangeLogJson.Components {
        if comp.From != "" && comp.Version != "" {
            fmt.Printf("  %s: %s → %s\n", comp.Name, comp.From, comp.Version)
        }
    }
    
    // Access updated images with commit history
    for _, img := range info.ChangeLogJson.UpdatedImages {
        fmt.Printf("Updated: %s (%d commits)\n", img.Name, len(img.Commits))
        for _, commit := range img.Commits {
            fmt.Printf("  - %s\n", commit.Subject)
        }
    }
}

// Access job results
if info.Results != nil {
    for name, job := range info.Results.BlockingJobs {
        fmt.Printf("Blocking job %s: %s\n", name, job.State)
    }
}

// Access upgrade history
for _, upgrade := range info.UpgradesTo {
    successRate := float64(upgrade.Success) / float64(upgrade.Total) * 100
    fmt.Printf("Upgrade %s → %s: %.1f%% success\n", 
        upgrade.From, upgrade.To, successRate)
}
```

#### GetReleaseInfoForLocation
Convenience method using a ReleaseLocation from FindRelease:

```go
locations, _ := client.FindRelease(ctx, "4.21.10")
if len(locations) > 0 {
    info, _ := client.GetReleaseInfoForLocation(ctx, locations[0], "4.21.10")
    fmt.Printf("Release %s is %s\n", info.Name, info.Phase)
}
```

#### GetPullSpec
Get the pull specification for a specific release:

```go
// Single endpoint - automatically finds the stream
pullSpec, err := client.GetPullSpec(ctx, "4.21.10")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Pull spec: %s\n", pullSpec)
// Output: Pull spec: quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64
```

#### GetTagsForStream
Get all tags for a specific stream:

```go
tags, err := client.GetTagsForStream(ctx, "4-stable")
if err != nil {
    log.Fatal(err)
}

for _, tag := range tags {
    fmt.Printf("%s: %s (%s)\n", tag.Name, tag.PullSpec, tag.Phase)
}
```

#### GetPullSpecAcrossAll (MultiClient)
Find a release across all endpoints and get its pull specification:

```go
mc, _ := rc.NewMultiClient(rc.DefaultClientConfig())
defer mc.Close()

pullSpec, location, _ := mc.GetPullSpecAcrossAll(ctx, "4.21.10")
fmt.Printf("PullSpec: %s\n", pullSpec)
fmt.Printf("From: %s/%s stream %s\n", 
    location.Endpoint.Product, 
    location.Endpoint.Architecture,
    location.StreamName)
// Output: PullSpec: quay.io/openshift-release-dev/ocp-release:4.21.10-x86_64
//         From: ocp/amd64 stream 4-stable
```

#### GetTagForRelease
Get the complete Tag information for a release:

```go
tag, err := client.GetTagForRelease(ctx, "4.21.10")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Name: %s\n", tag.Name)
fmt.Printf("Phase: %s\n", tag.Phase)
fmt.Printf("PullSpec: %s\n", tag.PullSpec)
fmt.Printf("DownloadURL: %s\n", tag.DownloadURL)
```

## Usage

### Basic Client Usage

Create a client for a specific endpoint:

```go
package main

import (
    "context"
    "fmt"
    "log"

    rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

func main() {
    // Create a client for OCP amd64
    client, err := rc.NewClientForEndpoint(
        rc.ArchitectureAMD64,
        rc.ProductOCP,
        rc.DefaultClientConfig(),
    )
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    ctx := context.Background()

    // Make a simple GET request
    body, err := client.Get(ctx, "/api/v1/releasestream/4.14.0-0.nightly")
    if err != nil {
        log.Fatalf("Failed to get data: %v", err)
    }
    defer body.Close()

    // Process the response...
}
```

### Getting JSON Responses

```go
type ReleaseInfo struct {
    Name    string `json:"name"`
    Phase   string `json:"phase"`
    Version string `json:"version"`
}

var release ReleaseInfo
err := client.GetJSON(ctx, "/api/v1/release/4.14.0", &release)
if err != nil {
    log.Fatalf("Failed to get JSON: %v", err)
}

fmt.Printf("Release: %s, Phase: %s\n", release.Version, release.Phase)
```

### Using MultiClient for Multiple Endpoints

When you need to access multiple release-controller endpoints:

```go
// Create a multi-client that manages all valid endpoint combinations
mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
if err != nil {
    log.Fatalf("Failed to create multi-client: %v", err)
}
defer mc.Close()

// Get a specific client
ocpAMD64Client, err := mc.GetClient(rc.ProductOCP, rc.ArchitectureAMD64)
if err != nil {
    log.Fatalf("Failed to get client: %v", err)
}

// Use the client
body, err := ocpAMD64Client.Get(ctx, "/api/v1/releasestreams")
// ...

// Get all clients and iterate
for key, client := range mc.GetAllClients() {
    endpoint := client.GetEndpoint()
    fmt.Printf("Endpoint %s: %s\n", key, endpoint.URL)
}
```

### Custom Configuration

```go
config := &rc.ClientConfig{
    Timeout:   60 * time.Second,
    UserAgent: "my-custom-client/1.0",
    // Optionally provide custom HTTP transport
    // Transport: myCustomTransport,
}

client, err := rc.NewClientForEndpoint(
    rc.ArchitectureARM64,
    rc.ProductOCP,
    config,
)
```

### Working with Endpoints Directly

```go
// Create an endpoint
endpoint, err := rc.NewEndpoint(rc.ArchitectureAMD64, rc.ProductOCP)
if err != nil {
    log.Fatalf("Failed to create endpoint: %v", err)
}

fmt.Println(endpoint.URL) // https://amd64.ocp.releases.ci.openshift.org

// Create a client from the endpoint
client, err := rc.NewClient(endpoint, rc.DefaultClientConfig())
if err != nil {
    log.Fatalf("Failed to create client: %v", err)
}
defer client.Close()
```

## Validation

The client automatically validates endpoint combinations:

```go
// This will return an error because origin only supports amd64
_, err := rc.NewEndpoint(rc.ArchitectureARM64, rc.ProductOrigin)
// Error: product origin only supports architecture amd64
```

## Helper Functions

Get all supported architectures and products:

```go
// Get all architectures
archs := rc.AllArchitectures()
for _, arch := range archs {
    fmt.Println(arch)
}

// Get all products
products := rc.AllProducts()
for _, product := range products {
    fmt.Println(product)
}
```

## Error Handling

The client returns descriptive errors for common issues:

```go
body, err := client.Get(ctx, "/api/v1/nonexistent")
if err != nil {
    // Handles:
    // - Network errors
    // - HTTP status errors (non-200)
    // - Context cancellation
    // - Invalid URLs
    log.Printf("Error: %v", err)
}
```

## Best Practices

1. **Always close clients**: Use `defer client.Close()` to ensure resources are released
2. **Use context**: Pass context for cancellation and timeout control
3. **Reuse clients**: Create clients once and reuse them for multiple requests
4. **Use MultiClient**: When working with multiple endpoints, use `MultiClient` to manage them efficiently
5. **Check endpoint validity**: The client validates endpoint combinations at creation time

## Command-Line Tool

A CLI tool is provided for querying release-controller endpoints from the command line:

```bash
# Build the tool
go build -o release-streams ./pkg/release-controller/cmd/release-streams

# Query a specific endpoint
./release-streams -arch amd64 -product ocp

# Query with verbose output
./release-streams -arch amd64 -product ocp -v

# Limit output to first 5 releases per stream
./release-streams -arch amd64 -product ocp -limit 5

# Filter to a specific release stream
./release-streams -arch amd64 -product ocp -stream "4-stable"

# Show statistics
./release-streams -arch amd64 -product ocp -stats

# Query all endpoints
./release-streams -all

# Query all endpoints with statistics
./release-streams -all -stats

# Query specific stream across all endpoints
./release-streams -all -stream "4-stable"

# Find which streams contain a specific release
./release-streams -find "4.13.0-rc.8"

# Find a release across all endpoints
./release-streams -all -find "4.21.10"

# Get detailed info for a specific release
./release-streams -info "4.21.9" -info-stream "4-stable"

# Get detailed info with verbose output (shows all components, job details, etc.)
./release-streams -info "4.21.9" -info-stream "4-stable" -v

# Get pull specification for a release
./release-streams -pullspec "4.21.9"

# Get pull specification from a specific architecture
./release-streams -arch arm64 -pullspec "4.21.9"

# Get pull specification searching across all endpoints
./release-streams -all -pullspec "4.21.9"
```

Available flags:
- `-arch` - Architecture (amd64, arm64, s390x, ppc64le, multi) [default: amd64]
- `-product` - Product (ocp, origin) [default: ocp]
- `-all` - Query all valid endpoint combinations
- `-stream` - Filter to specific release stream (e.g., "4-stable", "4.14.0-0.nightly")
- `-stats` - Show statistics for all streams
- `-limit` - Limit number of releases shown per stream (0 = all) [default: 0]
- `-find` - Find which streams contain the specified release
- `-info` - Get detailed information for a specific release
- `-info-stream` - Stream name for -info (required with -info)
- `-pullspec` - Get the pull specification for a specific release
- `-v` - Verbose output (shows URLs and additional details)

Example output:
```
$ ./release-streams -arch amd64 -product ocp -limit 5
Release Streams for ocp/amd64 (32 streams, 1250 total releases):

4-dev-preview (6 releases):
  [0] 4.22.0-ec.5
  [1] 4.22.0-ec.4
  [2] 4.22.0-ec.3
  [3] 4.22.0-ec.2
  [4] 4.22.0-ec.1
  ... and 1 more releases

4-stable (150 releases):
  [0] 4.21.10
  [1] 4.21.9
  [2] 4.21.8
  [3] 4.21.7
  [4] 4.21.6
  ... and 145 more releases

$ ./release-streams -stats
Stream Statistics for ocp/amd64 (32 streams):

Stream Name                    Releases  Latest Release
--------------------------------------------------------------------------------
4-dev-preview                          6  4.22.0-ec.5
4-stable                             150  4.21.10
4.14.0-0.nightly                      50  4.14.0-0.nightly-2024-01-15-123456
5-stable                               3  5.0.2
...
```

## Testing

Run the tests:

```bash
go test -v ./pkg/release-controller/...
```

## Examples

See the following for comprehensive examples:
- `client_test.go` - Client usage examples
- `releasestreams_test.go` - Release streams API tests
- `RELEASE_STREAMS.md` - Detailed documentation on the Release Streams API with usage examples
