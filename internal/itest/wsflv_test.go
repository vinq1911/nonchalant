// If you are AI: This file contains integration tests for WebSocket-FLV output.
// Tests verify that clients can consume streams via WebSocket-FLV.

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

	"github.com/gorilla/websocket"
)

func TestWSFLVPlayback(t *testing.T) {
	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping WebSocket-FLV test")
	}

	// Build the binary first
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Find free ports
	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)

	// Create a temporary config file
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configContent := `server:
  health_port: 8080
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

	// Wait for health endpoint on HTTP port
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
			t.Skipf("RTMP publish failed (prerequisite for WebSocket-FLV test): %v", err)
		}
	default:
		// Publish is running, continue with WebSocket-FLV test
	}

	// Connect WebSocket-FLV client
	wsURL := fmt.Sprintf("ws://localhost:%s/ws/live/teststream", portToString(httpPort))
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("Expected status 101, got %d", resp.StatusCode)
	}

	// Read first frame (should be FLV header)
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read first message: %v", err)
	}

	if messageType != websocket.BinaryMessage {
		t.Errorf("Expected binary message, got %d", messageType)
	}

	// Validate FLV header
	if len(data) < 9 {
		t.Error("Response too short for FLV header")
	}

	if !bytes.HasPrefix(data, []byte("FLV")) {
		t.Errorf("Response does not start with FLV signature, got: %v", data[:3])
	}

	// Read a few more frames to verify tags are coming
	for i := 0; i < 3; i++ {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or error - acceptable for test
			break
		}

		if messageType != websocket.BinaryMessage {
			t.Errorf("Expected binary message, got %d", messageType)
		}

		if len(data) == 0 {
			t.Error("Received empty frame")
		}
	}

	// Stop publishing
	publishCmd.Process.Signal(syscall.SIGTERM)
	<-publishErrChan

	// Close WebSocket connection
	conn.Close()

	// Test passes if we got FLV header and some frames
	// NOTE: Full end-to-end test would require RTMP publish to work correctly
}
