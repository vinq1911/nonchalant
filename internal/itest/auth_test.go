// If you are AI: This file integration-tests publish-key authentication on RTMP ingest.
// Verifies that a configured server rejects bad keys and accepts good ones via real ffmpeg.

package itest

import (
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

// TestPublishAuthRejectsMissingKey: when publish_keys is configured, an unauthenticated
// publish must NOT register a stream in the bus.
func TestPublishAuthRejectsMissingKey(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	httpPort, rtmpPort, cleanup := startAuthServer(t, "secret123")
	defer cleanup()

	// Publish WITHOUT a key — should be rejected.
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/anon", rtmpPort)
	pub := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc=duration=2:size=160x120:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10",
		"-f", "flv", "-y", rtmpURL)
	pub.Stderr = os.Stderr
	_ = pub.Run() // We expect a non-zero exit; either way the server must not list the stream.

	time.Sleep(500 * time.Millisecond)
	if streamRegistered(t, httpPort, "live", "anon") {
		t.Fatal("server registered an unauthenticated stream — auth check failed")
	}
}

// TestPlayAuthRejectsAndAccepts: HTTP-FLV / HLS / DASH consumers must
// match the configured play_keys when present. /api/* must NOT be gated.
func TestPlayAuthRejectsAndAccepts(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	httpPort, _, cleanup := startPlayAuthServer(t, "watcher")
	defer cleanup()

	// /api/streams is anonymous (auth gates only the playback paths).
	if resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/streams", httpPort)); err != nil {
		t.Fatalf("api: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("api/streams expected 200, got %d", resp.StatusCode)
		}
	}

	// Without ?key= the FLV endpoint must return 401, even for a non-existent stream.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/live/x.flv", httpPort))
	if err != nil {
		t.Fatalf("flv: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous flv expected 401, got %d", resp.StatusCode)
	}

	// With wrong key still 401.
	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/live/x.flv?key=wrong", httpPort))
	if err != nil {
		t.Fatalf("flv: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("wrong-key flv expected 401, got %d", resp.StatusCode)
	}

	// Right key should pass the gate. The stream doesn't exist so we expect 404,
	// not 401.
	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/live/x.flv?key=watcher", httpPort))
	if err != nil {
		t.Fatalf("flv: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("authorised flv expected 404 (no publisher), got %d", resp.StatusCode)
	}
}

// startPlayAuthServer is the play-side equivalent of startAuthServer: builds and
// boots a server with auth.play_keys configured.
func startPlayAuthServer(t *testing.T, key string) (int, int, func()) {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf(
		"server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\nauth:\n  play_keys: [\"%s\"]\n",
		httpPort, rtmpPort, key,
	)
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

// TestPublishAuthAcceptsValidKey: a publisher with a matching ?key=<secret>
// must succeed and the stream must show up in /api/streams.
func TestPublishAuthAcceptsValidKey(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	httpPort, rtmpPort, cleanup := startAuthServer(t, "secret123")
	defer cleanup()

	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/authok?key=secret123", rtmpPort)
	pub := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-re", "-f", "lavfi",
		"-i", "testsrc=duration=6:size=160x120:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10",
		"-f", "flv", rtmpURL)
	pub.Stderr = os.Stderr
	if err := pub.Start(); err != nil {
		t.Fatalf("start publisher: %v", err)
	}
	defer func() {
		_ = pub.Process.Signal(syscall.SIGTERM)
		_ = pub.Wait()
	}()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if streamRegistered(t, httpPort, "live", "authok") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("authenticated stream did not register within 10s")
}

// startAuthServer builds, configures, and starts the server with a single
// publish key. Returns the http port, rtmp port, and a cleanup function.
func startAuthServer(t *testing.T, key string) (int, int, func()) {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf(
		"server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\nauth:\n  publish_keys: [\"%s\"]\n",
		httpPort, rtmpPort, key,
	)
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

// streamRegistered returns true if /api/streams currently lists app/name with a publisher.
func streamRegistered(t *testing.T, httpPort int, app, name string) bool {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/streams", httpPort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var body struct {
		Streams []struct {
			App          string `json:"app"`
			Name         string `json:"name"`
			HasPublisher bool   `json:"has_publisher"`
		} `json:"streams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, s := range body.Streams {
		if s.App == app && s.Name == name && s.HasPublisher {
			return true
		}
	}
	return false
}
