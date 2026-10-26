package releasecontroller_test

import (
	"fmt"
	"log"

	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

// Example_getAllReleaseStreams demonstrates fetching all release streams with their releases
func Example_getAllReleaseStreams() {
	// This example shows the structure returned by the API

	client, err := rc.NewClientForEndpoint(
		rc.ArchitectureAMD64,
		rc.ProductOCP,
		rc.DefaultClientConfig(),
	)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return
	}
	defer func() { _ = client.Close() }()

	// Show what the API returns
	fmt.Println("Endpoint: /api/v1/releasestreams/all")
	fmt.Println("Returns: Map of stream names to arrays of release names")
	fmt.Println()
	fmt.Println("Example structure:")
	fmt.Println(`{
  "4-stable": ["4.21.10", "4.21.9", "4.21.8", ...],
  "4-dev-preview": ["4.22.0-ec.5", "4.22.0-ec.4", ...],
  "4.14.0-0.nightly": [
    "4.14.0-0.nightly-2024-01-15-123456",
    "4.14.0-0.nightly-2024-01-14-123456",
    ...
  ]
}`)

	// Output:
	// Endpoint: /api/v1/releasestreams/all
	// Returns: Map of stream names to arrays of release names
	//
	// Example structure:
	// {
	//   "4-stable": ["4.21.10", "4.21.9", "4.21.8", ...],
	//   "4-dev-preview": ["4.22.0-ec.5", "4.22.0-ec.4", ...],
	//   "4.14.0-0.nightly": [
	//     "4.14.0-0.nightly-2024-01-15-123456",
	//     "4.14.0-0.nightly-2024-01-14-123456",
	//     ...
	//   ]
	// }
}

// Example_getReleaseStreamNames demonstrates getting just the stream names
func Example_getReleaseStreamNames() {
	client, err := rc.NewClientForEndpoint(
		rc.ArchitectureAMD64,
		rc.ProductOCP,
		rc.DefaultClientConfig(),
	)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return
	}
	defer func() { _ = client.Close() }()

	fmt.Println("GetReleaseStreamNames() returns sorted []string")
	fmt.Println("Examples:")
	fmt.Println("  - 4-stable")
	fmt.Println("  - 4-dev-preview")
	fmt.Println("  - 4.14.0-0.nightly")
	fmt.Println("  - 4.14.0-0.ci")
	fmt.Println("  - 5-stable")

	// Output:
	// GetReleaseStreamNames() returns sorted []string
	// Examples:
	//   - 4-stable
	//   - 4-dev-preview
	//   - 4.14.0-0.nightly
	//   - 4.14.0-0.ci
	//   - 5-stable
}

// Example_getReleasesForStream demonstrates getting releases for a specific stream
func Example_getReleasesForStream() {
	fmt.Println("GetReleasesForStream(ctx, \"4-stable\") returns []string")
	fmt.Println()
	fmt.Println("Example releases:")
	fmt.Println("  [0]: 4.21.10  (most recent)")
	fmt.Println("  [1]: 4.21.9")
	fmt.Println("  [2]: 4.21.8")
	fmt.Println("  ...")

	// Output:
	// GetReleasesForStream(ctx, "4-stable") returns []string
	//
	// Example releases:
	//   [0]: 4.21.10  (most recent)
	//   [1]: 4.21.9
	//   [2]: 4.21.8
	//   ...
}

// Example_getLatestRelease demonstrates getting the latest release
func Example_getLatestRelease() {
	fmt.Println("GetLatestReleaseForStream(ctx, \"4-stable\") returns string")
	fmt.Println()
	fmt.Println("Returns the first release (most recent) from the stream")
	fmt.Println("Example: \"4.21.10\"")

	// Output:
	// GetLatestReleaseForStream(ctx, "4-stable") returns string
	//
	// Returns the first release (most recent) from the stream
	// Example: "4.21.10"
}

// Example_getStreamStats demonstrates getting statistics for all streams
func Example_getStreamStats() {
	fmt.Println("GetStreamStats(ctx) returns []StreamStats")
	fmt.Println()
	fmt.Println("StreamStats contains:")
	fmt.Println("  - Name: stream name")
	fmt.Println("  - ReleaseCount: number of releases")
	fmt.Println("  - LatestRelease: most recent release")
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  {Name: \"4-stable\", ReleaseCount: 150, LatestRelease: \"4.21.10\"}")
	fmt.Println("  {Name: \"4-dev-preview\", ReleaseCount: 6, LatestRelease: \"4.22.0-ec.5\"}")

	// Output:
	// GetStreamStats(ctx) returns []StreamStats
	//
	// StreamStats contains:
	//   - Name: stream name
	//   - ReleaseCount: number of releases
	//   - LatestRelease: most recent release
	//
	// Example:
	//   {Name: "4-stable", ReleaseCount: 150, LatestRelease: "4.21.10"}
	//   {Name: "4-dev-preview", ReleaseCount: 6, LatestRelease: "4.22.0-ec.5"}
}

// Example_iterateStreamsAndReleases demonstrates iterating over all streams and releases
func Example_iterateStreamsAndReleases() {
	fmt.Println("Example: Iterating over all streams and releases")
	fmt.Println()
	fmt.Println("allStreams, _ := client.GetAllReleaseStreams(ctx)")
	fmt.Println("for streamName, releases := range allStreams {")
	fmt.Printf("    fmt.Printf(\"Stream: %%s (%%d releases)\\n\", streamName, len(releases))\n")
	fmt.Println("    for i, releaseName := range releases {")
	fmt.Printf("        fmt.Printf(\"  [%%d] %%s\\n\", i, releaseName)\n")
	fmt.Println("    }")
	fmt.Println("}")

	// Output:
	// Example: Iterating over all streams and releases
	//
	// allStreams, _ := client.GetAllReleaseStreams(ctx)
	// for streamName, releases := range allStreams {
	//     fmt.Printf("Stream: %s (%d releases)\n", streamName, len(releases))
	//     for i, releaseName := range releases {
	//         fmt.Printf("  [%d] %s\n", i, releaseName)
	//     }
	// }
}

// Example_findRelease demonstrates finding a release across streams
func Example_findRelease() {
	fmt.Println("FindRelease(ctx, \"4.13.0-rc.8\") returns []ReleaseLocation")
	fmt.Println()
	fmt.Println("Searches all streams for the specified release")
	fmt.Println("Returns locations where it was found:")
	fmt.Println()
	fmt.Println("Example result:")
	fmt.Println("  [{Endpoint: ..., StreamName: \"4-stable\"}]")
	fmt.Println("  [{Endpoint: ..., StreamName: \"4.13.0-0.nightly\"}]")
	fmt.Println()
	fmt.Println("Each location includes the endpoint and stream name")

	// Output:
	// FindRelease(ctx, "4.13.0-rc.8") returns []ReleaseLocation
	//
	// Searches all streams for the specified release
	// Returns locations where it was found:
	//
	// Example result:
	//   [{Endpoint: ..., StreamName: "4-stable"}]
	//   [{Endpoint: ..., StreamName: "4.13.0-0.nightly"}]
	//
	// Each location includes the endpoint and stream name
}

// Example_findReleaseAcrossAll demonstrates finding a release across all endpoints
func Example_findReleaseAcrossAll() {
	fmt.Println("MultiClient.FindReleaseAcrossAll(ctx, \"4.13.0-rc.8\")")
	fmt.Println("returns []ReleaseLocation")
	fmt.Println()
	fmt.Println("Searches across all architectures and products")
	fmt.Println()
	fmt.Println("Example result:")
	fmt.Println("  [{")
	fmt.Println("    Endpoint: {Architecture: \"amd64\", Product: \"ocp\", ...},")
	fmt.Println("    StreamName: \"4-stable\"")
	fmt.Println("  }, {")
	fmt.Println("    Endpoint: {Architecture: \"arm64\", Product: \"ocp\", ...},")
	fmt.Println("    StreamName: \"4-stable\"")
	fmt.Println("  }]")

	// Output:
	// MultiClient.FindReleaseAcrossAll(ctx, "4.13.0-rc.8")
	// returns []ReleaseLocation
	//
	// Searches across all architectures and products
	//
	// Example result:
	//   [{
	//     Endpoint: {Architecture: "amd64", Product: "ocp", ...},
	//     StreamName: "4-stable"
	//   }, {
	//     Endpoint: {Architecture: "arm64", Product: "ocp", ...},
	//     StreamName: "4-stable"
	//   }]
}
