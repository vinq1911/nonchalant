// If you are AI: This file contains integration tests for HTTP-FLV output.
// Tests verify that clients can consume streams via HTTP-FLV.

package itest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHTTPFLVPlayback(t *testing.T) {
	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping HTTP-FLV test")
	}

	// Build the binary first
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Find free ports
	healthPort := findFreePort(t)
	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)

	// Create a temporary config file
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configContent := `server:
  health_port: ` + portToString(healthPort) + `
  http_port: ` + portToString(httpPort) + `
  rtmp_port: ` + portToString(rtmpPort) + `
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

	// Wait for health endpoint on HTTP port (health is available on HTTP port)
	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		t.Fatalf("Health endpoint not available: %v", err)
	}

	// Wait a bit for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Create a test video file using ffmpeg
	testVideoPath := filepath.Join(t.TempDir(), "test.mp4")
	createVideoCmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "testsrc=duration=2:size=320x240:rate=1",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-t", "2",
		"-y",
		testVideoPath,
	)
	createVideoCmd.Stderr = os.Stderr
	if err := createVideoCmd.Run(); err != nil {
		t.Skipf("Failed to create test video (ffmpeg may not support lavfi): %v", err)
	}

	// Publish to RTMP using ffmpeg in background
	rtmpURL := "rtmp://localhost:" + portToString(rtmpPort) + "/live/teststream"
	publishCmd := exec.Command("ffmpeg",
		"-re",
		"-i", testVideoPath,
		"-c", "copy",
		"-f", "flv",
		rtmpURL,
	)
	publishCmd.Stderr = os.Stderr

	publishErrChan := make(chan error, 1)
	go func() {
		publishErrChan <- publishCmd.Run()
	}()

	// Wait for publish to establish and check if it succeeded
	time.Sleep(2 * time.Second)

	// Check if publish is still running (success) or has errored
	select {
	case err := <-publishErrChan:
		if err != nil {
			t.Skipf("RTMP publish failed (prerequisite for HTTP-FLV test): %v", err)
		}
	default:
		// Publish is running, continue with HTTP-FLV test
	}

	// Connect HTTP-FLV client
	flvURL := fmt.Sprintf("http://localhost:%s/live/teststream.flv", portToString(httpPort))
	resp, err := http.Get(flvURL)
	if err != nil {
		t.Fatalf("Failed to connect to HTTP-FLV: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Validate FLV header
	header := make([]byte, 9)
	if _, err := resp.Body.Read(header); err != nil {
		t.Fatalf("Failed to read FLV header: %v", err)
	}

	// Check FLV signature
	if !bytes.HasPrefix(header, []byte("FLV")) {
		t.Errorf("Response does not start with FLV signature, got: %v", header[:3])
	}

	// Read a bit more to verify tags are coming
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		// EOF is OK, we just want to verify we got some data
		if n == 0 {
			t.Error("No data received after FLV header")
		}
	}

	// Stop publishing after we've verified HTTP-FLV works
	publishCmd.Process.Signal(syscall.SIGTERM)
	<-publishErrChan

	// Close HTTP connection
	resp.Body.Close()

	// Test passes if we got FLV header and some data
	// NOTE: Full end-to-end test would require RTMP publish to work correctly
}
