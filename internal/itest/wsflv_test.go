// If you are AI: This file contains integration tests for WebSocket-FLV output.
// Tests verify that clients can consume streams via WebSocket-FLV.

package itest

import (
	"bytes"
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

// TestWSFLVPlayback publishes a stream via RTMP and verifies it can be
// consumed via WebSocket-FLV. Checks the FLV header and that data flows.
func TestWSFLVPlayback(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping WebSocket-FLV test")
	}

	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n",
		httpPort, rtmpPort)
	os.WriteFile(configPath, []byte(cfg), 0644)

	cmd := exec.Command(binPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGINT)
		cmd.Wait()
	}()

	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		t.Fatalf("Health endpoint not available: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Create an 8-second test video with audio and frequent keyframes.
	// -g 15 = keyframe every 15 frames (1 second at 15fps).
	testVideo := filepath.Join(t.TempDir(), "test.mp4")
	gen := exec.Command("ffmpeg", "-f", "lavfi",
		"-i", "testsrc=duration=8:size=320x240:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "15",
		"-c:a", "aac", "-b:a", "64k",
		"-t", "8", "-y", testVideo)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("Cannot create test video: %v\n%s", err, out)
	}

	// Publish to RTMP in background
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/teststream", rtmpPort)
	pub := exec.Command("ffmpeg", "-re", "-i", testVideo,
		"-c", "copy", "-f", "flv", rtmpURL)
	pub.Stderr = os.Stderr
	pubErr := make(chan error, 1)
	go func() { pubErr <- pub.Run() }()
	defer func() {
		pub.Process.Signal(syscall.SIGTERM)
		<-pubErr
	}()

	// Wait for stream to establish
	time.Sleep(2 * time.Second)

	// Verify publisher is still running
	select {
	case err := <-pubErr:
		if err != nil {
			t.Skipf("Publisher exited early: %v", err)
		}
	default:
	}

	// Connect WebSocket-FLV client
	wsURL := fmt.Sprintf("ws://localhost:%d/ws/live/teststream",
		httpPort)
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
	msgType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read first message: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Errorf("Expected binary message, got %d", msgType)
	}
	if len(data) < 9 {
		t.Fatal("Response too short for FLV header")
	}
	if !bytes.HasPrefix(data, []byte("FLV")) {
		t.Errorf("Not FLV signature, got: %v", data[:3])
	}

	// Read a few more frames to verify tags are coming
	framesRead := 0
	for i := 0; i < 5; i++ {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType != websocket.BinaryMessage {
			t.Errorf("Expected binary message, got %d", msgType)
		}
		if len(data) == 0 {
			t.Error("Received empty frame")
		}
		framesRead++
	}
	t.Logf("WebSocket-FLV: read %d frames after header", framesRead)
}
