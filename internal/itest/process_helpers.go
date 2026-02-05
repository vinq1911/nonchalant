// If you are AI: This file provides helper functions for starting and managing server processes in tests.

package itest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// StartServer starts the nonchalant server as a subprocess on a free port.
// Returns the process, the port it's listening on, and any error.
func StartServer(ctx context.Context, configPath string) (*exec.Cmd, int, error) {
	// Find a free port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, 0, fmt.Errorf("find free port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Get the path to the binary
	binPath, err := findBinary()
	if err != nil {
		return nil, 0, fmt.Errorf("find binary: %w", err)
	}

	// Create a temporary config with the free port
	tempConfig, err := createTempConfig(configPath, port)
	if err != nil {
		return nil, 0, fmt.Errorf("create temp config: %w", err)
	}

	// Start the process
	cmd := exec.CommandContext(ctx, binPath, "--config", tempConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, 0, fmt.Errorf("start server: %w", err)
	}

	return cmd, port, nil
}

// WaitForHealth waits for the health endpoint to become available.
// Returns an error if the endpoint is not available within the timeout.
func WaitForHealth(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://localhost:%d/healthz", port)

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("health endpoint not available after %v", timeout)
}

// findBinary locates the nonchalant binary in the project directory.
func findBinary() (string, error) {
	// Try common build locations
	candidates := []string{
		"bin/nonchalant",
		"nonchalant",
		filepath.Join(os.Getenv("GOPATH"), "bin", "nonchalant"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("nonchalant binary not found")
}

// createTempConfig creates a temporary config file with the specified port.
func createTempConfig(baseConfigPath string, port int) (string, error) {
	// Read base config
	data, err := os.ReadFile(baseConfigPath)
	if err != nil {
		return "", fmt.Errorf("read base config: %w", err)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "nonchalant-test-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Write config with port override
	// NOTE: This is a simple approach; in production we'd use proper YAML manipulation
	configContent := string(data)
	configContent = fmt.Sprintf("%s\nserver:\n  health_port: %d\n", configContent, port)

	if _, err := tmpFile.WriteString(configContent); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write temp config: %w", err)
	}

	return tmpFile.Name(), nil
}
