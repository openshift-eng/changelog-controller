package releasecontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// ClientConfig contains configuration options for the release-controller client
type ClientConfig struct {
	// Timeout is the HTTP client timeout (default: 30 seconds)
	Timeout time.Duration

	// UserAgent is the User-Agent header to send with requests
	UserAgent string

	// Transport allows customizing the HTTP transport
	Transport http.RoundTripper
}

// DefaultClientConfig returns a ClientConfig with sensible defaults
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:   30 * time.Second,
		UserAgent: "changelog-controller/1.0",
	}
}

// client implements the Client interface
type client struct {
	endpoint   *Endpoint
	httpClient *http.Client
	config     *ClientConfig
}

// NewClient creates a new release-controller client for the specified endpoint
func NewClient(endpoint *Endpoint, config *ClientConfig) (Client, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("endpoint cannot be nil")
	}

	if config == nil {
		config = DefaultClientConfig()
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	if config.Transport != nil {
		httpClient.Transport = config.Transport
	}

	return &client{
		endpoint:   endpoint,
		httpClient: httpClient,
		config:     config,
	}, nil
}

// NewClientForEndpoint is a convenience function that creates both an endpoint and a client
func NewClientForEndpoint(arch Architecture, product Product, config *ClientConfig) (Client, error) {
	endpoint, err := NewEndpoint(arch, product)
	if err != nil {
		return nil, fmt.Errorf("failed to create endpoint: %w", err)
	}

	return NewClient(endpoint, config)
}

// Get performs a GET request to the specified path on the endpoint
func (c *client) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	fullURL, err := url.JoinPath(c.endpoint.URL, path)
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	klog.V(4).Infof("GET %s", fullURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.UserAgent != "" {
		req.Header.Set("User-Agent", c.config.UserAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// GetJSON performs a GET request and unmarshals the JSON response
func (c *client) GetJSON(ctx context.Context, path string, v any) error {
	body, err := c.Get(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return nil
}

// GetEndpoint returns the endpoint this client is configured for
func (c *client) GetEndpoint() *Endpoint {
	return c.endpoint
}

// Close closes the client and releases any resources
func (c *client) Close() error {
	// Close idle connections
	if c.httpClient != nil && c.httpClient.Transport != nil {
		if transport, ok := c.httpClient.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	return nil
}

// MultiClient manages multiple release-controller clients across different endpoints
type MultiClient struct {
	clients map[string]Client
}

// NewMultiClient creates a new multi-client that can access multiple endpoints
func NewMultiClient(config *ClientConfig) (*MultiClient, error) {
	mc := &MultiClient{
		clients: make(map[string]Client),
	}

	// Create clients for all valid endpoint combinations
	for _, product := range AllProducts() {
		for _, arch := range AllArchitectures() {
			// Skip invalid combinations (origin only supports amd64)
			if product == ProductOrigin && arch != ArchitectureAMD64 {
				continue
			}

			endpoint, err := NewEndpoint(arch, product)
			if err != nil {
				return nil, fmt.Errorf("failed to create endpoint for %s/%s: %w", product, arch, err)
			}

			client, err := NewClient(endpoint, config)
			if err != nil {
				return nil, fmt.Errorf("failed to create client for %s/%s: %w", product, arch, err)
			}

			key := fmt.Sprintf("%s/%s", product, arch)
			mc.clients[key] = client
		}
	}

	return mc, nil
}

// GetClient returns the client for the specified product and architecture
func (mc *MultiClient) GetClient(product Product, arch Architecture) (Client, error) {
	key := fmt.Sprintf("%s/%s", product, arch)
	client, ok := mc.clients[key]
	if !ok {
		return nil, fmt.Errorf("no client found for %s/%s", product, arch)
	}
	return client, nil
}

// GetAllClients returns all configured clients
func (mc *MultiClient) GetAllClients() map[string]Client {
	return mc.clients
}

// Close closes all clients
func (mc *MultiClient) Close() error {
	var errs []error
	for key, client := range mc.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close client %s: %w", key, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing clients: %v", errs)
	}
	return nil
}

// AddClientForTesting adds a client to the multi-client for testing purposes
// This method should only be used in tests
func (mc *MultiClient) AddClientForTesting(product Product, arch Architecture, client Client) {
	if mc.clients == nil {
		mc.clients = make(map[string]Client)
	}
	key := fmt.Sprintf("%s/%s", product, arch)
	mc.clients[key] = client
}

// FindReleaseAcrossAll searches for a specific release across all endpoints
// Returns all locations where the release was found (across all architectures and products)
// Returns an empty slice if the release is not found anywhere
func (mc *MultiClient) FindReleaseAcrossAll(ctx context.Context, releaseName string) ([]ReleaseLocation, error) {
	var allLocations []ReleaseLocation

	for _, client := range mc.clients {
		locations, err := client.FindRelease(ctx, releaseName)
		if err != nil {
			// Log error but continue searching other endpoints
			continue
		}

		// Locations already have endpoint information
		allLocations = append(allLocations, locations...)
	}

	return allLocations, nil
}

// GetPullSpecAcrossAll searches for a specific release across all endpoints
// and returns its pull specification from the first location found
// Returns an error if the release is not found in any endpoint
func (mc *MultiClient) GetPullSpecAcrossAll(ctx context.Context, releaseName string) (string, *ReleaseLocation, error) {
	locations, err := mc.FindReleaseAcrossAll(ctx, releaseName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to find release %s: %w", releaseName, err)
	}

	if len(locations) == 0 {
		return "", nil, fmt.Errorf("release %s not found in any endpoint", releaseName)
	}

	// Use the first location (typically all pull specs should be the same for a given release name
	// but different architectures will have different suffixes like -x86_64, -aarch64, etc.)
	location := locations[0]

	// Get the client for this location's endpoint
	client, err := mc.GetClient(location.Endpoint.Product, location.Endpoint.Architecture)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get client for %s/%s: %w",
			location.Endpoint.Product, location.Endpoint.Architecture, err)
	}

	pullSpec, err := client.GetPullSpecForLocation(ctx, location, releaseName)
	if err != nil {
		return "", nil, err
	}

	return pullSpec, &location, nil
}
