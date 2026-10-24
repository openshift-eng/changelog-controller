# Release Streams API

This document describes how to use the release streams API to query all available releases from OpenShift release-controllers.

## Overview

The `/api/v1/releasestreams/all` endpoint returns all release streams with their list of releases for a given architecture and product combination.

### What is a Release Stream?

A release stream is a collection of related releases. Examples include:
- `4-stable` - Stable releases for major version 4
- `5-stable` - Stable releases for major version 5
- `4-dev-preview` - Development preview releases for version 4
- `5-dev-preview` - Development preview releases for version 5
- `4.14.0-0.nightly` - Nightly builds for 4.14.0
- `4.14.0-0.ci` - CI builds for 4.14.0

Each stream contains an ordered list of release names, with the most recent release first.

## API Response Structure

The API returns a simple map of stream names to arrays of release names:

```json
{
  "4-stable": [
    "4.21.10",
    "4.21.9",
    "4.21.8",
    ...
  ],
  "4-dev-preview": [
    "4.22.0-ec.5",
    "4.22.0-ec.4",
    "4.22.0-ec.3",
    ...
  ],
  "4.14.0-0.nightly": [
    "4.14.0-0.nightly-2024-01-15-123456",
    "4.14.0-0.nightly-2024-01-14-123456",
    ...
  ],
  "5-stable": [
    "5.0.2",
    "5.0.1",
    "5.0.0"
  ]
}
```

## Go Types

```go
// AllReleaseStreamsResponse is a map of stream names to their list of releases
// The releases are ordered with the most recent first
type AllReleaseStreamsResponse map[string][]string

// StreamStats contains statistics about a release stream
type StreamStats struct {
    Name          string
    ReleaseCount  int
    LatestRelease string
}
```

## Usage Examples

### Fetch All Release Streams

```go
package main

import (
    "context"
    "fmt"
    "log"

    rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

func main() {
    client, err := rc.NewClientForEndpoint(
        rc.ArchitectureAMD64,
        rc.ProductOCP,
        rc.DefaultClientConfig(),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Fetch all release streams
    allStreams, err := client.GetAllReleaseStreams(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Iterate over each stream
    for streamName, releases := range allStreams {
        fmt.Printf("\nStream: %s (%d releases)\n", streamName, len(releases))
        
        // Show first 5 releases
        for i := 0; i < len(releases) && i < 5; i++ {
            fmt.Printf("  [%d] %s\n", i, releases[i])
        }
        if len(releases) > 5 {
            fmt.Printf("  ... and %d more\n", len(releases)-5)
        }
    }
}
```

### Get Just the Stream Names

```go
names, err := client.GetReleaseStreamNames(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Println("Available release streams:")
for _, name := range names {
    fmt.Println("  -", name)
}
// Output: 4-stable, 4-dev-preview, 4.14.0-0.nightly, 5-stable, etc.
```

### Get Releases for a Specific Stream

```go
// Get all releases for the 4-stable stream
releases, err := client.GetReleasesForStream(ctx, "4-stable")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Releases in 4-stable: %d\n", len(releases))
for i, release := range releases {
    fmt.Printf("  [%d] %s\n", i, release)
}
```

### Get the Latest Release

```go
// Get the most recent release for a stream
latest, err := client.GetLatestReleaseForStream(ctx, "4-stable")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Latest stable release: %s\n", latest)
// Output: Latest stable release: 4.21.10
```

### Get Stream Statistics

```go
stats, err := client.GetStreamStats(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("%-30s %10s  %s\n", "Stream", "Releases", "Latest")
fmt.Println(strings.Repeat("-", 60))

for _, stat := range stats {
    fmt.Printf("%-30s %10d  %s\n", 
        stat.Name, 
        stat.ReleaseCount, 
        stat.LatestRelease)
}
```

Example output:
```
Stream                         Releases  Latest
------------------------------------------------------------
4-dev-preview                          6  4.22.0-ec.5
4-stable                             150  4.21.10
4.14.0-0.nightly                      50  4.14.0-0.nightly-2024-01-15-123456
5-stable                               3  5.0.2
```

### Find a Specific Release

Search for a specific release across all streams within an endpoint:

```go
// Find which streams contain a specific release
locations, err := client.FindRelease(ctx, "4.13.0-rc.8")
if err != nil {
    log.Fatal(err)
}

if len(locations) == 0 {
    fmt.Println("Release not found")
} else {
    fmt.Printf("Found in %d stream(s):\n", len(locations))
    for _, loc := range locations {
        fmt.Printf("  Endpoint: %s/%s\n", loc.Endpoint.Product, loc.Endpoint.Architecture)
        fmt.Printf("  Stream: %s\n", loc.StreamName)
    }
}
```

Example output:
```
Found in 2 stream(s):
  Endpoint: ocp/amd64
  Stream: 4-stable
  Endpoint: ocp/amd64
  Stream: 4.13.0-0.nightly
```

### Find a Release Across All Endpoints

Search for a release across all architectures and products:

```go
mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
if err != nil {
    log.Fatal(err)
}
defer mc.Close()

locations, err := mc.FindReleaseAcrossAll(ctx, "4.21.10")
if err != nil {
    log.Fatal(err)
}

for _, loc := range locations {
    fmt.Printf("%s/%s - Stream: %s\n",
        loc.Endpoint.Product,
        loc.Endpoint.Architecture,
        loc.StreamName)
}
```

Example output:
```
ocp/amd64 - Stream: 4-stable
ocp/arm64 - Stream: 4-stable-arm64
ocp/multi - Stream: 4-stable-multi
ocp/ppc64le - Stream: 4-stable-ppc64le
ocp/s390x - Stream: 4-stable-s390x
```

### Query Multiple Architectures

```go
package main

import (
    "context"
    "fmt"
    "log"

    rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

func main() {
    ctx := context.Background()

    architectures := []rc.Architecture{
        rc.ArchitectureAMD64,
        rc.ArchitectureARM64,
    }

    for _, arch := range architectures {
        client, err := rc.NewClientForEndpoint(arch, rc.ProductOCP, rc.DefaultClientConfig())
        if err != nil {
            log.Printf("Failed to create client for %s: %v", arch, err)
            continue
        }
        defer client.Close()

        latest, err := client.GetLatestReleaseForStream(ctx, "4-stable")
        if err != nil {
            log.Printf("Failed to get latest for %s: %v", arch, err)
            continue
        }

        fmt.Printf("%s latest stable: %s\n", arch, latest)
    }
}
```

### Use MultiClient

```go
mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
if err != nil {
    log.Fatal(err)
}
defer mc.Close()

ctx := context.Background()

// Get statistics from all endpoints
for key, client := range mc.GetAllClients() {
    endpoint := client.GetEndpoint()
    
    stats, err := client.GetStreamStats(ctx)
    if err != nil {
        log.Printf("%s: error - %v\n", key, err)
        continue
    }

    totalReleases := 0
    for _, stat := range stats {
        totalReleases += stat.ReleaseCount
    }

    fmt.Printf("%s/%s: %d streams, %d total releases\n",
        endpoint.Product, endpoint.Architecture, len(stats), totalReleases)
}
```

## Command-Line Tool

A CLI tool is provided to query release streams:

```bash
# Build the tool
go build -o release-streams ./pkg/release-controller/cmd/release-streams

# Query OCP amd64
./release-streams -arch amd64 -product ocp

# Limit output to first 5 releases per stream
./release-streams -arch amd64 -product ocp -limit 5

# Query a specific stream
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
```

### CLI Flags

- `-arch` - Architecture (amd64, arm64, s390x, ppc64le, multi) [default: amd64]
- `-product` - Product (ocp, origin) [default: ocp]
- `-all` - Query all valid endpoint combinations
- `-stream` - Filter to specific release stream (e.g., "4-stable", "4.14.0-0.nightly")
- `-stats` - Show statistics for all streams
- `-limit` - Limit number of releases shown per stream (0 = all) [default: 0]
- `-find` - Find which streams contain the specified release
- `-v` - Verbose output (shows URLs and additional details)

### Example Output

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

4.14.0-0.nightly (50 releases):
  [0] 4.14.0-0.nightly-2024-01-15-123456
  [1] 4.14.0-0.nightly-2024-01-14-123456
  [2] 4.14.0-0.nightly-2024-01-13-123456
  [3] 4.14.0-0.nightly-2024-01-12-123456
  [4] 4.14.0-0.nightly-2024-01-11-123456
  ... and 45 more releases
```

```
$ ./release-streams -stream "4-stable" -limit 10
4-stable (150 releases):
  [0] 4.21.10
  [1] 4.21.9
  [2] 4.21.8
  [3] 4.21.7
  [4] 4.21.6
  [5] 4.21.5
  [6] 4.21.4
  [7] 4.21.3
  [8] 4.21.2
  [9] 4.21.1
  ... and 140 more releases
```

```
$ ./release-streams -stats
Stream Statistics for ocp/amd64 (32 streams):

Stream Name                    Releases  Latest Release
--------------------------------------------------------------------------------
4-dev-preview                          6  4.22.0-ec.5
4-stable                             150  4.21.10
4.11.0-0.ci                           20  4.11.0-0.ci-2024-01-15-123456
4.11.0-0.nightly                      50  4.11.0-0.nightly-2024-01-15-123456
4.14.0-0.ci                           20  4.14.0-0.ci-2024-01-15-123456
4.14.0-0.nightly                      50  4.14.0-0.nightly-2024-01-15-123456
5-stable                               3  5.0.2
...
```

```
$ ./release-streams -all -stats
Querying 6 endpoints...

=== ocp/amd64 ===
Stream Statistics for ocp/amd64 (32 streams):
...

=== ocp/arm64 ===
Stream Statistics for ocp/arm64 (28 streams):
...
```

```
$ ./release-streams -find "4.13.0-rc.8"
Searching for release '4.13.0-rc.8' in ocp/amd64...

Found release '4.13.0-rc.8' in 1 stream(s):

  Stream: 4-stable
  URL: https://amd64.ocp.releases.ci.openshift.org
```

```
$ ./release-streams -all -find "4.21.10"
Searching for release '4.21.10' across all endpoints...

Found release '4.21.10' in 5 location(s):

Endpoint: ocp/amd64
URL: https://amd64.ocp.releases.ci.openshift.org
Streams (1):
  - 4-stable

Endpoint: ocp/arm64
URL: https://arm64.ocp.releases.ci.openshift.org
Streams (1):
  - 4-stable-arm64

Endpoint: ocp/multi
URL: https://multi.ocp.releases.ci.openshift.org
Streams (1):
  - 4-stable-multi

Endpoint: ocp/ppc64le
URL: https://ppc64le.ocp.releases.ci.openshift.org
Streams (1):
  - 4-stable-ppc64le

Endpoint: ocp/s390x
URL: https://s390x.ocp.releases.ci.openshift.org
Streams (1):
  - 4-stable-s390x
```

## Integration with Controllers

### Discovering Latest Releases

```go
type Controller struct {
    client rc.Client
}

func (c *Controller) getLatestStable(ctx context.Context) (string, error) {
    latest, err := c.client.GetLatestReleaseForStream(ctx, "4-stable")
    if err != nil {
        return "", fmt.Errorf("failed to get latest stable: %w", err)
    }
    return latest, nil
}
```

### Monitoring Specific Streams

```go
func (c *Controller) processNightlyBuilds(ctx context.Context) error {
    releases, err := c.client.GetReleasesForStream(ctx, "4.14.0-0.nightly")
    if err != nil {
        return err
    }

    // Process only the latest 10 releases
    limit := 10
    if len(releases) < limit {
        limit = len(releases)
    }

    for i := 0; i < limit; i++ {
        if err := c.processRelease(releases[i]); err != nil {
            log.Printf("Failed to process %s: %v", releases[i], err)
        }
    }

    return nil
}
```

### Comparing Streams Across Architectures

```go
func (c *Controller) compareLatestAcrossArchitectures(ctx context.Context, streamName string) error {
    mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
    if err != nil {
        return err
    }
    defer mc.Close()

    for key, client := range mc.GetAllClients() {
        endpoint := client.GetEndpoint()
        
        // Skip non-OCP endpoints
        if endpoint.Product != rc.ProductOCP {
            continue
        }

        latest, err := client.GetLatestReleaseForStream(ctx, streamName)
        if err != nil {
            log.Printf("%s: no stream %s: %v", endpoint.Architecture, streamName, err)
            continue
        }

        fmt.Printf("%s: %s = %s\n", endpoint.Architecture, streamName, latest)
    }

    return nil
}
```

## Best Practices

1. **Reuse clients**: Create the client once and reuse it for multiple requests
2. **Use context**: Always pass a context for timeout and cancellation control
3. **Handle errors**: The API may be temporarily unavailable, implement retry logic
4. **Cache results**: The response can be large; consider caching if polling frequently
5. **Use helper methods**: Use `GetLatestReleaseForStream` instead of fetching all releases when you only need the latest
6. **Limit data**: Use array slicing to limit the number of releases you process
7. **Use MultiClient**: When querying multiple endpoints, use MultiClient for better resource management
8. **Stream naming**: Stream names follow patterns like `{major}-{type}` or `{version}.0-0.{type}`

## Error Handling

```go
allStreams, err := client.GetAllReleaseStreams(ctx)
if err != nil {
    // Possible errors:
    // - Network errors
    // - HTTP status errors (non-200)
    // - JSON parsing errors
    // - Context cancellation
    log.Printf("Failed to fetch streams: %v", err)
    return
}

// Check if a specific stream exists
releases, err := client.GetReleasesForStream(ctx, "my-stream")
if err != nil {
    // Will return error if stream doesn't exist
    log.Printf("Stream not found: %v", err)
}

// Check if stream has releases
latest, err := client.GetLatestReleaseForStream(ctx, "my-stream")
if err != nil {
    // Will return error if stream doesn't exist or has no releases
    log.Printf("No latest release: %v", err)
}
```

## Testing

All functionality is thoroughly tested. Run tests with:

```bash
go test -v ./pkg/release-controller/...
```

## Real-World Example

Query the actual API:

```bash
# Fetch all streams
curl -s https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/all | jq 'keys'

# Get releases for 4-stable
curl -s https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/all | jq '.["4-stable"][:5]'
```
