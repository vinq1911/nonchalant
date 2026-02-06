// If you are AI: This file contains integration tests that verify server startup, health checks, and shutdown.

package itest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServerStartupAndShutdown(t *testing.T) {
	// Build the binary first
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Find a free port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create a temporary config file
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configContent := `server:
  health_port: 8080
  http_port: ` + fmt.Sprintf("%d", port) + `
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

	// Wait for health endpoint
	if err := WaitForHealth(port, 5*time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("Health endpoint not available: %v", err)
	}

	// Send SIGINT
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send SIGINT: %v", err)
	}

	// Wait for process to exit (should happen within 2 seconds)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			// Process exited with error, check if it's expected
			if exitErr, ok := err.(*exec.ExitError); ok {
				// Exit code 0 or 1 is acceptable (clean shutdown or signal)
				if exitErr.ExitCode() != 0 && exitErr.ExitCode() != 1 {
					t.Errorf("Process exited with unexpected code: %d", exitErr.ExitCode())
				}
			}
		}
	case <-time.After(2 * time.Second):
		// Process didn't exit within 2 seconds - this is a failure
		cmd.Process.Kill()
		t.Fatal("Server did not exit within 2 seconds after SIGINT")
	}
}
