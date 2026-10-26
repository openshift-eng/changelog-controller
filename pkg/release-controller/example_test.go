package releasecontroller_test

import (
	"context"
	"fmt"
	"log"

	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
)

// Example_basicUsage demonstrates creating a client and making a simple request
func Example_basicUsage() {
	// Create a client for OCP amd64
	client, err := rc.NewClientForEndpoint(
		rc.ArchitectureAMD64,
		rc.ProductOCP,
		rc.DefaultClientConfig(),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Get the endpoint information
	endpoint := client.GetEndpoint()
	fmt.Printf("Connected to: %s\n", endpoint.URL)
	fmt.Printf("Architecture: %s\n", endpoint.Architecture)
	fmt.Printf("Product: %s\n", endpoint.Product)

	// Output:
	// Connected to: https://amd64.ocp.releases.ci.openshift.org
	// Architecture: amd64
	// Product: ocp
}

// Example_multiClient demonstrates working with multiple endpoints
func Example_multiClient() {
	// Create a multi-client for all endpoints
	mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create multi-client: %v", err)
	}
	defer func() { _ = mc.Close() }()

	// Access specific endpoints
	products := []rc.Product{rc.ProductOCP, rc.ProductOrigin}
	architectures := []rc.Architecture{rc.ArchitectureAMD64}

	for _, product := range products {
		for _, arch := range architectures {
			client, err := mc.GetClient(product, arch)
			if err != nil {
				log.Printf("Skipping %s/%s: %v", product, arch, err)
				continue
			}

			endpoint := client.GetEndpoint()
			fmt.Printf("%s/%s: %s\n", product, arch, endpoint.URL)
		}
	}

	// Output:
	// ocp/amd64: https://amd64.ocp.releases.ci.openshift.org
	// origin/amd64: https://amd64.origin.releases.ci.openshift.org
}

// Example_endpointValidation demonstrates endpoint validation
func Example_endpointValidation() {
	// Valid endpoint
	endpoint1, err := rc.NewEndpoint(rc.ArchitectureAMD64, rc.ProductOrigin)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Valid: %s\n", endpoint1.URL)
	}

	// Invalid endpoint - origin only supports amd64
	endpoint2, err := rc.NewEndpoint(rc.ArchitectureARM64, rc.ProductOrigin)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Valid: %s\n", endpoint2.URL)
	}

	// Output:
	// Valid: https://amd64.origin.releases.ci.openshift.org
	// Error: product origin only supports architecture amd64
}

// Example_allArchitecturesAndProducts demonstrates listing all supported configurations
func Example_allArchitecturesAndProducts() {
	fmt.Println("Supported Architectures:")
	for _, arch := range rc.AllArchitectures() {
		fmt.Printf("  - %s\n", arch)
	}

	fmt.Println("\nSupported Products:")
	for _, product := range rc.AllProducts() {
		fmt.Printf("  - %s\n", product)
	}

	// Output:
	// Supported Architectures:
	//   - amd64
	//   - arm64
	//   - s390x
	//   - ppc64le
	//   - multi
	//
	// Supported Products:
	//   - ocp
	//   - origin
}

// Example_contextUsage demonstrates using context for request control
func Example_contextUsage() {
	client, err := rc.NewClientForEndpoint(
		rc.ArchitectureAMD64,
		rc.ProductOCP,
		rc.DefaultClientConfig(),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The context is passed to all client methods
	// This allows for cancellation and timeout control
	_, err = client.Get(ctx, "/api/v1/releasestreams")
	if err != nil {
		// Handle error (in this example, we expect it to fail since we're not
		// actually connecting to a real server in the test environment)
		fmt.Println("Request completed (or failed as expected in test)")
	}

	// Output:
	// Request completed (or failed as expected in test)
}

// Example_iterateAllClients demonstrates iterating over all configured clients
func Example_iterateAllClients() {
	mc, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		log.Fatalf("Failed to create multi-client: %v", err)
	}
	defer func() { _ = mc.Close() }()

	allClients := mc.GetAllClients()
	fmt.Printf("Total clients: %d\n", len(allClients))

	// Expected clients:
	// OCP: amd64, arm64, s390x, ppc64le, multi (5)
	// Origin: amd64 (1)
	// Total: 6

	// Output:
	// Total clients: 6
}
