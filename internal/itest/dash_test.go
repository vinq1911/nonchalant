// If you are AI: This file tests the RTMP → HTTP-FLV → DASH round-trip.
// Verifies that nonchalant's FLV output can be packaged into valid DASH by ffmpeg.

package itest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestDASHRoundTrip publishes a stream via RTMP, captures it via HTTP-FLV,
// and packages the captured FLV into DASH using ffmpeg. Verifies the .mpd
// manifest and segment files are produced.
// Uses a two-stage approach: capture FLV to file, then convert to DASH.
func TestDASHRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping DASH test")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available, skipping DASH test")
	}

	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Build failed: %v\n%s", err, out)
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
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGINT)
		cmd.Wait()
	}()

	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		t.Fatalf("Server not ready: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Create an 8-second test video with audio and frequent keyframes (every 1s).
	// -g 15 = keyframe every 15 frames (at 15fps = every 1 second).
	// Late-joining subscribers wait for a keyframe before streaming data.
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

	// Publish via RTMP in background (runs for 8 real-time seconds)
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/dashtest", rtmpPort)
	pub := exec.Command("ffmpeg", "-re", "-i", testVideo,
		"-c", "copy", "-f", "flv", rtmpURL)
	pub.Stderr = os.Stderr
	if err := pub.Start(); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}
	defer func() {
		pub.Process.Signal(syscall.SIGTERM)
		pub.Wait()
	}()

	// Wait for stream to establish and some data to flow
	time.Sleep(2 * time.Second)

	// Stage 1: Capture HTTP-FLV stream to file using curl with timeout.
	// 5s is enough to capture at least one keyframe + some data.
	flvFile := filepath.Join(t.TempDir(), "capture.flv")
	flvURL := fmt.Sprintf("http://localhost:%d/live/dashtest.flv", httpPort)
	curlCmd := exec.Command("curl", "-s", "--max-time", "5",
		"-o", flvFile, flvURL)
	curlCmd.Run() // Ignore exit code; curl exits non-zero on timeout

	// Verify we captured meaningful data (>1KB means we got frames)
	info, err := os.Stat(flvFile)
	if err != nil || info.Size() < 1024 {
		t.Fatalf("Failed to capture sufficient FLV data: size=%d err=%v",
			safeSize(info), err)
	}
	t.Logf("Captured %d bytes of FLV data", info.Size())

	// Stage 2: Convert captured FLV to DASH
	dashDir := filepath.Join(t.TempDir(), "dash")
	os.MkdirAll(dashDir, 0755)
	dashManifest := filepath.Join(dashDir, "stream.mpd")

	dashCmd := exec.Command("ffmpeg",
		"-i", flvFile,
		"-c", "copy",
		"-f", "dash",
		"-seg_duration", "1",
		"-y",
		dashManifest)
	if out, err := dashCmd.CombinedOutput(); err != nil {
		t.Fatalf("DASH packaging failed: %v\n%s", err, out)
	}

	// Verify .mpd manifest exists and has content
	mpdInfo, err := os.Stat(dashManifest)
	if err != nil {
		t.Fatalf("DASH manifest not found: %v", err)
	}
	if mpdInfo.Size() == 0 {
		t.Fatal("DASH manifest is empty")
	}

	// Verify segment files were produced (m4s or mp4)
	m4sSegments, _ := filepath.Glob(filepath.Join(dashDir, "*.m4s"))
	mp4Segments, _ := filepath.Glob(filepath.Join(dashDir, "*.mp4"))
	totalSegments := len(m4sSegments) + len(mp4Segments)
	if totalSegments == 0 {
		t.Fatal("No DASH segment files produced")
	}

	t.Logf("DASH round-trip OK: manifest=%s, segments=%d",
		dashManifest, totalSegments)
}
