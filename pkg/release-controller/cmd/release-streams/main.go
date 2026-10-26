package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

func main() {
	var (
		arch              = flag.String("arch", "amd64", "Architecture (amd64, arm64, s390x, ppc64le, multi)")
		product           = flag.String("product", "ocp", "Product (ocp, origin)")
		all               = flag.Bool("all", false, "Query all endpoints")
		verbose           = flag.Bool("v", false, "Verbose output")
		streamName        = flag.String("stream", "", "Filter to specific release stream")
		stats             = flag.Bool("stats", false, "Show statistics for all streams")
		limit             = flag.Int("limit", 0, "Limit number of releases shown per stream (0 = all)")
		findRelease       = flag.String("find", "", "Find which streams contain the specified release")
		releaseInfo       = flag.String("info", "", "Get detailed info for a specific release")
		releaseInfoStream = flag.String("info-stream", "", "Stream name for -info (required with -info)")
		pullSpec          = flag.String("pullspec", "", "Get the pull specification for a specific release")
	)
	flag.Parse()

	ctx := context.Background()

	if *pullSpec != "" {
		if *all {
			getPullSpecAcrossAll(ctx, *pullSpec)
		} else {
			getPullSpec(ctx, *arch, *product, *pullSpec)
		}
		return
	}

	if *releaseInfo != "" {
		if *releaseInfoStream == "" {
			log.Fatal("Error: -info-stream is required when using -info")
		}
		getReleaseInfo(ctx, *arch, *product, *releaseInfoStream, *releaseInfo, *verbose)
		return
	}

	if *all {
		if *findRelease != "" {
			findReleaseAcrossAll(ctx, *findRelease)
			return
		}
		queryAllEndpoints(ctx, *verbose, *streamName, *stats, *limit)
		return
	}

	// Parse architecture
	var architecture rc.Architecture
	switch strings.ToLower(*arch) {
	case "amd64":
		architecture = rc.ArchitectureAMD64
	case "arm64":
		architecture = rc.ArchitectureARM64
	case "s390x":
		architecture = rc.ArchitectureS390x
	case "ppc64le":
		architecture = rc.ArchitecturePPC64le
	case "multi":
		architecture = rc.ArchitectureMulti
	default:
		log.Fatalf("Invalid architecture: %s", *arch)
	}

	// Parse product
	var prod rc.Product
	switch strings.ToLower(*product) {
	case "ocp":
		prod = rc.ProductOCP
	case "origin":
		prod = rc.ProductOrigin
	default:
		log.Fatalf("Invalid product: %s", *product)
	}

	if *findRelease != "" {
		findReleaseInEndpoint(ctx, architecture, prod, *findRelease)
		return
	}

	querySingleEndpoint(ctx, architecture, prod, *verbose, *streamName, *stats, *limit)
}

func querySingleEndpoint(ctx context.Context, arch rc.Architecture, product rc.Product, verbose bool, streamFilter string, showStats bool, limit int) {
	client, err := rc.NewClientForEndpoint(arch, product, rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	endpoint := client.GetEndpoint()
	if verbose {
		fmt.Printf("Querying: %s\n", endpoint.URL)
		fmt.Printf("Endpoint: /api/v1/releasestreams/all\n\n")
	}

	if showStats {
		displayStats(ctx, client, product, arch)
		return
	}

	// If specific stream requested
	if streamFilter != "" {
		releases, err := client.GetReleasesForStream(ctx, streamFilter)
		if err != nil {
			log.Fatalf("Failed to get releases for stream %s: %v", streamFilter, err)
		}
		displayStreamReleases(streamFilter, releases, limit)
		return
	}

	// Display all streams
	allStreams, err := client.GetAllReleaseStreams(ctx)
	if err != nil {
		log.Fatalf("Failed to get release streams: %v", err)
	}

	totalReleases := 0
	for _, releases := range allStreams {
		totalReleases += len(releases)
	}

	fmt.Printf("Release Streams for %s/%s (%d streams, %d total releases):\n\n", product, arch, len(allStreams), totalReleases)

	// Sort stream names for consistent output
	streamNames := make([]string, 0, len(allStreams))
	for name := range allStreams {
		streamNames = append(streamNames, name)
	}
	sort.Strings(streamNames)

	for _, name := range streamNames {
		releases := allStreams[name]
		displayStreamReleases(name, releases, limit)
		fmt.Println()
	}
}

func displayStreamReleases(name string, releases []string, limit int) {
	fmt.Printf("%s (%d releases):\n", name, len(releases))

	if len(releases) == 0 {
		fmt.Println("  (no releases)")
		return
	}

	displayCount := len(releases)
	if limit > 0 && limit < displayCount {
		displayCount = limit
	}

	for i := 0; i < displayCount; i++ {
		fmt.Printf("  [%d] %s\n", i, releases[i])
	}

	if limit > 0 && limit < len(releases) {
		fmt.Printf("  ... and %d more releases\n", len(releases)-limit)
	}
}

func displayStats(ctx context.Context, client rc.Client, product rc.Product, arch rc.Architecture) {
	stats, err := client.GetStreamStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get stream stats: %v", err)
	}

	fmt.Printf("Stream Statistics for %s/%s (%d streams):\n\n", product, arch, len(stats))
	fmt.Printf("%-30s %10s  %s\n", "Stream Name", "Releases", "Latest Release")
	fmt.Println(strings.Repeat("-", 80))

	for _, stat := range stats {
		latest := stat.LatestRelease
		if latest == "" {
			latest = "(none)"
		}
		fmt.Printf("%-30s %10d  %s\n", stat.Name, stat.ReleaseCount, latest)
	}
}

func queryAllEndpoints(ctx context.Context, verbose bool, streamFilter string, showStats bool, limit int) {
	mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create multi-client: %v", err)
	}
	defer func() { _ = mc.Close() }()

	allClients := mc.GetAllClients()
	fmt.Printf("Querying %d endpoints...\n\n", len(allClients))

	// Sort client keys for consistent output
	keys := make([]string, 0, len(allClients))
	for key := range allClients {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		client := allClients[key]
		endpoint := client.GetEndpoint()

		if verbose {
			fmt.Printf("=== %s ===\n", key)
			fmt.Printf("URL: %s\n", endpoint.URL)
		} else {
			fmt.Printf("=== %s/%s ===\n", endpoint.Product, endpoint.Architecture)
		}

		if showStats {
			displayStats(ctx, client, endpoint.Product, endpoint.Architecture)
		} else if streamFilter != "" {
			// Show only the requested stream
			releases, err := client.GetReleasesForStream(ctx, streamFilter)
			if err != nil {
				fmt.Printf("Stream %s not found or error: %v\n", streamFilter, err)
			} else {
				displayStreamReleases(streamFilter, releases, limit)
			}
		} else {
			// Show summary
			allStreams, err := client.GetAllReleaseStreams(ctx)
			if err != nil {
				fmt.Printf("Error: %v\n\n", err)
				continue
			}

			totalReleases := 0
			for _, releases := range allStreams {
				totalReleases += len(releases)
			}

			fmt.Printf("Streams: %d, Total Releases: %d\n", len(allStreams), totalReleases)

			if verbose {
				// Sort and display all streams
				streamNames := make([]string, 0, len(allStreams))
				for name := range allStreams {
					streamNames = append(streamNames, name)
				}
				sort.Strings(streamNames)

				for _, name := range streamNames {
					releases := allStreams[name]
					fmt.Printf("\n")
					displayStreamReleases(name, releases, limit)
				}
			}
		}

		fmt.Println()
	}
}

func findReleaseInEndpoint(ctx context.Context, arch rc.Architecture, product rc.Product, releaseName string) {
	client, err := rc.NewClientForEndpoint(arch, product, rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	fmt.Printf("Searching for release '%s' in %s/%s...\n\n", releaseName, product, arch)

	locations, err := client.FindRelease(ctx, releaseName)
	if err != nil {
		log.Fatalf("Failed to find release: %v", err)
	}

	if len(locations) == 0 {
		fmt.Printf("Release '%s' not found in %s/%s\n", releaseName, product, arch)
		return
	}

	fmt.Printf("Found release '%s' in %d stream(s):\n\n", releaseName, len(locations))
	for _, loc := range locations {
		fmt.Printf("  Stream: %s\n", loc.StreamName)
		fmt.Printf("  URL: %s\n\n", loc.Endpoint.URL)
	}
}

func findReleaseAcrossAll(ctx context.Context, releaseName string) {
	mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create multi-client: %v", err)
	}
	defer func() { _ = mc.Close() }()

	fmt.Printf("Searching for release '%s' across all endpoints...\n\n", releaseName)

	locations, err := mc.FindReleaseAcrossAll(ctx, releaseName)
	if err != nil {
		log.Fatalf("Failed to find release: %v", err)
	}

	if len(locations) == 0 {
		fmt.Printf("Release '%s' not found in any endpoint\n", releaseName)
		return
	}

	fmt.Printf("Found release '%s' in %d location(s):\n\n", releaseName, len(locations))

	// Group by endpoint for better display
	endpointMap := make(map[string][]rc.ReleaseLocation)
	for _, loc := range locations {
		key := fmt.Sprintf("%s/%s", loc.Endpoint.Product, loc.Endpoint.Architecture)
		endpointMap[key] = append(endpointMap[key], loc)
	}

	// Sort endpoint keys
	endpointKeys := make([]string, 0, len(endpointMap))
	for key := range endpointMap {
		endpointKeys = append(endpointKeys, key)
	}
	sort.Strings(endpointKeys)

	for _, key := range endpointKeys {
		locs := endpointMap[key]
		endpoint := locs[0].Endpoint

		fmt.Printf("Endpoint: %s/%s\n", endpoint.Product, endpoint.Architecture)
		fmt.Printf("URL: %s\n", endpoint.URL)
		fmt.Printf("Streams (%d):\n", len(locs))

		for _, loc := range locs {
			fmt.Printf("  - %s\n", loc.StreamName)
		}
		fmt.Println()
	}
}

func getReleaseInfo(ctx context.Context, arch, product, streamName, releaseName string, verbose bool) {
	// Parse architecture
	var architecture rc.Architecture
	switch strings.ToLower(arch) {
	case "amd64":
		architecture = rc.ArchitectureAMD64
	case "arm64":
		architecture = rc.ArchitectureARM64
	case "s390x":
		architecture = rc.ArchitectureS390x
	case "ppc64le":
		architecture = rc.ArchitecturePPC64le
	case "multi":
		architecture = rc.ArchitectureMulti
	default:
		log.Fatalf("Invalid architecture: %s", arch)
	}

	// Parse product
	var prod rc.Product
	switch strings.ToLower(product) {
	case "ocp":
		prod = rc.ProductOCP
	case "origin":
		prod = rc.ProductOrigin
	default:
		log.Fatalf("Invalid product: %s", product)
	}

	client, err := rc.NewClientForEndpoint(architecture, prod, rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	fmt.Printf("Fetching release info for '%s' in stream '%s' from %s/%s...\n\n", releaseName, streamName, prod, architecture)

	info, err := client.GetReleaseInfo(ctx, streamName, releaseName)
	if err != nil {
		log.Fatalf("Failed to get release info: %v", err)
	}

	// Display basic info
	fmt.Printf("Release: %s\n", info.Name)
	fmt.Printf("Phase: %s\n", info.Phase)
	fmt.Println()

	// Display changelog summary
	if info.ChangeLogJson != nil {
		fromName := ""
		toName := ""
		if info.ChangeLogJson.From != nil {
			fromName = info.ChangeLogJson.From.Name
		}
		if info.ChangeLogJson.To != nil {
			toName = info.ChangeLogJson.To.Name
		}
		if fromName != "" && toName != "" {
			fmt.Printf("Changelog: %s → %s\n", fromName, toName)
		}
		if len(info.ChangeLogJson.Components) > 0 {
			fmt.Printf("Components updated: %d\n", len(info.ChangeLogJson.Components))
			if verbose {
				fmt.Println("\nComponents:")
				for _, comp := range info.ChangeLogJson.Components {
					if comp.From != "" && comp.Version != "" {
						fmt.Printf("  - %s: %s → %s\n", comp.Name, comp.From, comp.Version)
					} else {
						fmt.Printf("  - %s\n", comp.Name)
					}
				}
			}
		}
		if len(info.ChangeLogJson.UpdatedImages) > 0 {
			fmt.Printf("Updated images: %d\n", len(info.ChangeLogJson.UpdatedImages))
		}
		if len(info.ChangeLogJson.RebuiltImages) > 0 {
			fmt.Printf("Rebuilt images: %d\n", len(info.ChangeLogJson.RebuiltImages))
		}
		fmt.Println()
	}

	// Display test results
	if info.Results != nil {
		fmt.Printf("Blocking Jobs: %d\n", len(info.Results.BlockingJobs))
		if verbose && len(info.Results.BlockingJobs) > 0 {
			for name, job := range info.Results.BlockingJobs {
				fmt.Printf("  - %s: %s\n", name, job.State)
				if job.Retries > 0 {
					fmt.Printf("    Retries: %d\n", job.Retries)
				}
			}
		}

		fmt.Printf("Informing Jobs: %d\n", len(info.Results.InformingJobs))
		if verbose && len(info.Results.InformingJobs) > 0 {
			for name, job := range info.Results.InformingJobs {
				fmt.Printf("  - %s: %s\n", name, job.State)
				if job.Retries > 0 {
					fmt.Printf("    Retries: %d\n", job.Retries)
				}
			}
		}

		// Count successes and failures
		blockingSuccess := 0
		blockingFailed := 0
		for _, job := range info.Results.BlockingJobs {
			switch job.State {
			case "Succeeded":
				blockingSuccess++
			case "Failed":
				blockingFailed++
			}
		}

		informingSuccess := 0
		informingFailed := 0
		for _, job := range info.Results.InformingJobs {
			switch job.State {
			case "Succeeded":
				informingSuccess++
			case "Failed":
				informingFailed++
			}
		}

		fmt.Printf("\nJob Summary:\n")
		fmt.Printf("  Blocking: %d succeeded, %d failed\n", blockingSuccess, blockingFailed)
		fmt.Printf("  Informing: %d succeeded, %d failed\n", informingSuccess, informingFailed)
		fmt.Println()
	}

	// Display upgrade history
	if len(info.UpgradesTo) > 0 {
		fmt.Printf("Upgrade History: %d records\n", len(info.UpgradesTo))
		if verbose {
			for _, upgrade := range info.UpgradesTo {
				successRate := 0.0
				if upgrade.Total > 0 {
					successRate = float64(upgrade.Success) / float64(upgrade.Total) * 100
				}
				fmt.Printf("  - %s → %s: %d/%d (%.1f%% success)\n",
					upgrade.From, upgrade.To, upgrade.Success, upgrade.Total, successRate)
			}
		}
	}
}

func getPullSpec(ctx context.Context, arch, product, releaseName string) {
	// Parse architecture
	var architecture rc.Architecture
	switch strings.ToLower(arch) {
	case "amd64":
		architecture = rc.ArchitectureAMD64
	case "arm64":
		architecture = rc.ArchitectureARM64
	case "s390x":
		architecture = rc.ArchitectureS390x
	case "ppc64le":
		architecture = rc.ArchitecturePPC64le
	case "multi":
		architecture = rc.ArchitectureMulti
	default:
		log.Fatalf("Invalid architecture: %s", arch)
	}

	// Parse product
	var prod rc.Product
	switch strings.ToLower(product) {
	case "ocp":
		prod = rc.ProductOCP
	case "origin":
		prod = rc.ProductOrigin
	default:
		log.Fatalf("Invalid product: %s", product)
	}

	client, err := rc.NewClientForEndpoint(architecture, prod, rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	fmt.Printf("Searching for release '%s' in %s/%s...\n\n", releaseName, prod, architecture)

	pullSpec, err := client.GetPullSpec(ctx, releaseName)
	if err != nil {
		log.Fatalf("Failed to get pull spec: %v", err)
	}

	fmt.Printf("Release: %s\n", releaseName)
	fmt.Printf("PullSpec: %s\n", pullSpec)
}

func getPullSpecAcrossAll(ctx context.Context, releaseName string) {
	mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create multi-client: %v", err)
	}
	defer func() { _ = mc.Close() }()

	fmt.Printf("Searching for release '%s' across all endpoints...\n\n", releaseName)

	pullSpec, location, err := mc.GetPullSpecAcrossAll(ctx, releaseName)
	if err != nil {
		log.Fatalf("Failed to get pull spec: %v", err)
	}

	fmt.Printf("Release: %s\n", releaseName)
	fmt.Printf("Endpoint: %s/%s\n", location.Endpoint.Product, location.Endpoint.Architecture)
	fmt.Printf("Stream: %s\n", location.StreamName)
	fmt.Printf("PullSpec: %s\n", pullSpec)
}
