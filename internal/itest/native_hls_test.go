// If you are AI: This file integration-tests the built-in /hls/* and /dash/* endpoints.
// Verifies that nonchalant itself (not an external pipeline) serves a manifest plus segments.

package itest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestNativeHLSEndpoint verifies GET /hls/{app}/{name}.m3u8 returns a real
// playlist and that at least one .ts segment is reachable from it.
func TestNativeHLSEndpoint(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	httpPort, rtmpPort, kill := startPlainServer(t)
	defer kill()

	pubKill := startLoopingPublisher(t, rtmpPort, "live", "hlsnative")
	defer pubKill()
	waitForLiveStream(t, httpPort, "live", "hlsnative", 10*time.Second)

	manifest := waitForGet(t,
		fmt.Sprintf("http://localhost:%d/hls/live/hlsnative.m3u8", httpPort),
		"#EXTM3U", 25*time.Second)

	// Parse out the first .ts line, fetch it, and check it has a TS sync byte.
	var seg string
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".ts") {
			seg = line
			break
		}
	}
	if seg == "" {
		t.Fatalf("no .ts segment in manifest:\n%s", manifest)
	}
	segURL := fmt.Sprintf("http://localhost:%d/hls/live/hlsnative/%s", httpPort, seg)
	body := mustGet(t, segURL)
	if len(body) < 188 {
		t.Fatalf("segment too short: %d bytes", len(body))
	}
	if body[0] != 0x47 {
		t.Fatalf("segment first byte = 0x%02x, want TS sync 0x47", body[0])
	}
}

// TestNativeDASHEndpoint verifies GET /dash/{app}/{name}.mpd returns a manifest.
func TestNativeDASHEndpoint(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	httpPort, rtmpPort, kill := startPlainServer(t)
	defer kill()

	pubKill := startLoopingPublisher(t, rtmpPort, "live", "dashnative")
	defer pubKill()
	waitForLiveStream(t, httpPort, "live", "dashnative", 10*time.Second)

	mpd := waitForGet(t,
		fmt.Sprintf("http://localhost:%d/dash/live/dashnative.mpd", httpPort),
		"<MPD", 25*time.Second)
	if !strings.Contains(mpd, "</MPD>") {
		t.Fatalf("MPD missing closing tag:\n%s", mpd)
	}
}

// startPlainServer is a small helper that builds and starts a server with no auth.
func startPlainServer(t *testing.T) (httpPort, rtmpPort int, kill func()) {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	httpPort = findFreePort(t)
	rtmpPort = findFreePort(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n", httpPort, rtmpPort)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(binPath, "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("server not ready: %v", err)
	}
	return httpPort, rtmpPort, func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		_ = cmd.Wait()
	}
}

// startLoopingPublisher publishes a 30s test pattern in a loop until killed.
// Keyframe interval is 1s so HLS segmentation has frequent IDR points.
func startLoopingPublisher(t *testing.T, rtmpPort int, app, name string) func() {
	t.Helper()
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/%s/%s", rtmpPort, app, name)
	pub := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-stream_loop", "-1", "-re", "-f", "lavfi",
		"-i", "testsrc=duration=30:size=320x240:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "15",
		"-c:a", "aac", "-b:a", "64k",
		"-f", "flv", rtmpURL)
	pub.Stderr = os.Stderr
	if err := pub.Start(); err != nil {
		t.Fatalf("publisher start: %v", err)
	}
	return func() {
		_ = pub.Process.Signal(syscall.SIGTERM)
		_ = pub.Wait()
	}
}

// waitForLiveStream polls /api/streams until app/name shows up with a publisher.
func waitForLiveStream(t *testing.T, httpPort int, app, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/streams", httpPort))
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(body), `"name":"`+name+`"`) &&
				strings.Contains(string(body), `"has_publisher":true`) {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("stream %s/%s never went live", app, name)
}

// waitForGet polls url until the response body contains needle.
// Returns the body once found.
func waitForGet(t *testing.T, url, needle string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := mustGetMaybe(url)
		if strings.Contains(body, needle) {
			return body
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s never returned a response containing %q within %v", url, needle, timeout)
	return ""
}

// mustGet does an HTTP GET and fails the test on any non-2xx or read error.
func mustGet(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode/100 != 2 {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	return body
}

// mustGetMaybe does a single GET and returns the body or "" on any error.
func mustGetMaybe(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	if resp.StatusCode/100 != 2 {
		return ""
	}
	return string(body)
}
