// If you are AI: This file tests the RTMP → HTTP-FLV → HLS round-trip.
// Verifies that nonchalant's FLV output can be packaged into valid HLS by ffmpeg.

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

// TestHLSRoundTrip publishes a stream via RTMP, captures it via HTTP-FLV,
// and packages the captured FLV into HLS using ffmpeg. Verifies the m3u8
// playlist and .ts segment files are produced.
// Uses a two-stage approach: capture FLV to file, then convert to HLS.
func TestHLSRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available, skipping HLS test")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available, skipping HLS test")
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
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/hlstest", rtmpPort)
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
	flvURL := fmt.Sprintf("http://localhost:%d/live/hlstest.flv", httpPort)
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

	// Stage 2: Convert captured FLV to HLS
	hlsDir := filepath.Join(t.TempDir(), "hls")
	os.MkdirAll(hlsDir, 0755)
	hlsPlaylist := filepath.Join(hlsDir, "stream.m3u8")

	hlsCmd := exec.Command("ffmpeg",
		"-i", flvFile,
		"-c", "copy",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "0",
		"-y",
		hlsPlaylist)
	if out, err := hlsCmd.CombinedOutput(); err != nil {
		t.Fatalf("HLS packaging failed: %v\n%s", err, out)
	}

	// Verify m3u8 playlist exists and has content
	plInfo, err := os.Stat(hlsPlaylist)
	if err != nil {
		t.Fatalf("HLS playlist not found: %v", err)
	}
	if plInfo.Size() == 0 {
		t.Fatal("HLS playlist is empty")
	}

	// Verify at least one .ts segment was produced
	segments, _ := filepath.Glob(filepath.Join(hlsDir, "*.ts"))
	if len(segments) == 0 {
		t.Fatal("No HLS .ts segments produced")
	}

	t.Logf("HLS round-trip OK: playlist=%s, segments=%d",
		hlsPlaylist, len(segments))
}

// safeSize returns the file size or 0 if info is nil.
func safeSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}
