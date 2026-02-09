// If you are AI: This file contains integration tests for HTTP-FLV output.
// Tests verify that clients can consume streams via HTTP-FLV.

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
)

// TestHTTPFLVPlayback publishes a stream via RTMP and verifies it can be
// consumed via HTTP-FLV. Checks the FLV header and that data flows.
func TestHTTPFLVPlayback(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping HTTP-FLV test")
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

	// Connect HTTP-FLV client
	flvURL := fmt.Sprintf("http://localhost:%d/live/teststream.flv", httpPort)
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
	if !bytes.HasPrefix(header, []byte("FLV")) {
		t.Errorf("Not FLV signature, got: %v", header[:3])
	}

	// Read more data to verify tags are flowing (wait for keyframe gating)
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && n == 0 {
		t.Error("No data received after FLV header")
	}
	t.Logf("HTTP-FLV: received %d bytes after header", n)
}
