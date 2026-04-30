// If you are AI: This file integration-tests adaptive-bitrate (ABR) HLS / DASH
// packaging. The server is configured with a 2-rung ladder; we publish a real
// stream and assert the master playlist + per-rendition resources resolve.

package itest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHLSABRMasterPlaylist publishes a stream to a server configured with two
// HLS ladder rungs and asserts:
//   - /hls/{app}/{name}.m3u8 returns a master playlist that references both
//     rungs by name.
//   - /hls/{app}/{name}/{rung}/index.m3u8 returns a media playlist with .ts
//     segments for each rung.
//   - At least one .ts segment per rung is fetchable and starts with the
//     MPEG-TS sync byte.
func TestHLSABRMasterPlaylist(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	httpPort, rtmpPort, kill := startABRServer(t)
	defer kill()

	pubKill := startLoopingPublisher(t, rtmpPort, "live", "abr")
	defer pubKill()
	waitForLiveStream(t, httpPort, "live", "abr", 15*time.Second)

	master := waitForGet(t,
		fmt.Sprintf("http://localhost:%d/hls/live/abr.m3u8", httpPort),
		"#EXT-X-STREAM-INF", 45*time.Second)

	// Master must list both rungs.
	for _, rung := range []string{"med", "low"} {
		if !strings.Contains(master, rung+"/index.m3u8") {
			t.Fatalf("master playlist missing rung %q:\n%s", rung, master)
		}
	}

	// Fetch each per-rendition playlist and grab one .ts segment.
	for _, rung := range []string{"med", "low"} {
		playlistURL := fmt.Sprintf("http://localhost:%d/hls/live/abr/%s/index.m3u8",
			httpPort, rung)
		body := waitForGet(t, playlistURL, "#EXTM3U", 30*time.Second)
		seg := firstTSSegment(body)
		if seg == "" {
			t.Fatalf("rung %s playlist has no .ts segment:\n%s", rung, body)
		}
		segURL := fmt.Sprintf("http://localhost:%d/hls/live/abr/%s/%s",
			httpPort, rung, seg)
		bytes := mustGet(t, segURL)
		if len(bytes) < 188 || bytes[0] != 0x47 {
			t.Fatalf("rung %s segment %s missing TS sync byte (len=%d, byte0=0x%02x)",
				rung, seg, len(bytes), bytes[0])
		}
	}
}

// TestDASHABRManifest does the same for DASH: the MPD must declare two video
// Representations (one per video rung).
func TestDASHABRManifest(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	httpPort, rtmpPort, kill := startABRServer(t)
	defer kill()

	pubKill := startLoopingPublisher(t, rtmpPort, "live", "abrdash")
	defer pubKill()
	waitForLiveStream(t, httpPort, "live", "abrdash", 15*time.Second)

	mpd := waitForGet(t,
		fmt.Sprintf("http://localhost:%d/dash/live/abrdash.mpd", httpPort),
		"<MPD", 45*time.Second)

	if !strings.Contains(mpd, "</MPD>") {
		t.Fatalf("MPD missing closing tag:\n%s", mpd)
	}
	// We expect at least 2 Representation entries — ABR is the whole point.
	if got := strings.Count(mpd, "<Representation"); got < 2 {
		t.Fatalf("MPD should have ≥2 Representations for an ABR ladder, got %d:\n%s",
			got, mpd)
	}
}

// startABRServer builds and starts the server with a small ladder configured.
// We use deliberately tiny resolutions and bitrates to keep the test cheap.
func startABRServer(t *testing.T) (int, int, func()) {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf(`server:
  health_port: 8080
  http_port: %d
  rtmp_port: %d
hls:
  ladder:
    - {name: med, width: 320, height: 240, video_bitrate: 400}
    - {name: low, width: 160, height: 120, video_bitrate: 150}
`, httpPort, rtmpPort)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
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

// firstTSSegment returns the first .ts filename referenced in an HLS media
// playlist (or "" if none found).
func firstTSSegment(playlist string) string {
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && strings.HasSuffix(line, ".ts") {
			return line
		}
	}
	return ""
}
