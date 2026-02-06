// If you are AI: This file contains integration tests for HTTP API endpoints.
// Tests verify API responses without requiring active media streams.

package itest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAPIServer(t *testing.T) {
	// Build the binary first
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Find free ports
	httpPort := findFreePort(t)

	// Create a temporary config file
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configContent := `server:
  health_port: 8080
  http_port: ` + portToString(httpPort) + `
  rtmp_port: 1935
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start the server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGINT)
		cmd.Wait()
	}()

	// Wait for health endpoint
	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		t.Fatalf("Health endpoint not available: %v", err)
	}

	// Wait a bit for server to be ready
	time.Sleep(500 * time.Millisecond)

	// Test GET /api/server
	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/api/server", portToString(httpPort)))
	if err != nil {
		t.Fatalf("Failed to query /api/server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var serverResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&serverResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if serverResp["version"] == nil {
		t.Error("Response missing version")
	}
	if serverResp["uptime"] == nil {
		t.Error("Response missing uptime")
	}

	// Test GET /api/streams (empty)
	resp2, err := http.Get(fmt.Sprintf("http://localhost:%s/api/streams", portToString(httpPort)))
	if err != nil {
		t.Fatalf("Failed to query /api/streams: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp2.StatusCode)
	}

	var streamsResp map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&streamsResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if streamsResp["streams"] == nil {
		t.Error("Response missing streams")
	}

	// Test GET /api/relay
	resp3, err := http.Get(fmt.Sprintf("http://localhost:%s/api/relay", portToString(httpPort)))
	if err != nil {
		t.Fatalf("Failed to query /api/relay: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp3.StatusCode)
	}

	var relayResp map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&relayResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if relayResp["tasks"] == nil {
		t.Error("Response missing tasks")
	}
}
