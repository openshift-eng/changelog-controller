package changelog_controller

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerBasic(t *testing.T) {
	// Create a minimal options setup
	opts := &Options{
		httpPort: 18080, // Use a non-standard port for testing
		cache:    NewChangelogCache(),
	}

	// Create a context with timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the HTTP server in a goroutine
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- opts.runHTTPServer(ctx)
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Test the root endpoint
	resp, err := http.Get("http://localhost:18080/")
	if err != nil {
		t.Fatalf("Failed to GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)
	if len(bodyStr) == 0 {
		t.Error("Expected non-empty response body")
	}

	// Verify the response contains expected text
	expectedTexts := []string{
		"Release Payload Changelog Controller",
		"Changelog HTTP Server",
		"/changelog",
		"/status",
	}

	for _, expected := range expectedTexts {
		if !containsString(bodyStr, expected) {
			t.Errorf("Expected response to contain %q, got: %s", expected, bodyStr)
		}
	}

	// Cancel context to trigger server shutdown
	cancel()

	// Wait for server to shutdown
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Errorf("Server shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not shutdown within timeout")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
