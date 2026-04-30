// If you are AI: Integration test that verifies the push relay actually
// moves a live stream between two nonchalant instances via ffmpeg.

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

// TestPushRelayMovesStream: server A pushes "live/relayed" to server B via
// a configured push relay. We then assert that B's /api/streams shows the
// stream as live.
func TestPushRelayMovesStream(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	binPath := filepath.Join(t.TempDir(), "nonchalant")
	if out, err := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Server B (destination): plain config, no relays.
	bHTTP := findFreePort(t)
	bRTMP := findFreePort(t)
	bCfg := filepath.Join(t.TempDir(), "b.yaml")
	mustWrite(t, bCfg, fmt.Sprintf(
		"server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n",
		bHTTP, bRTMP))
	srvB := startBin(t, binPath, bCfg, bHTTP)
	defer srvB()

	// Server A (source): publishes to itself, push-relays to B.
	aHTTP := findFreePort(t)
	aRTMP := findFreePort(t)
	aCfg := filepath.Join(t.TempDir(), "a.yaml")
	mustWrite(t, aCfg, fmt.Sprintf(
		"server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n"+
			"relays:\n  - {app: live, name: relayed, mode: push, "+
			"remote_url: \"rtmp://127.0.0.1:%d/live/relayed\", reconnect: true}\n",
		aHTTP, aRTMP, bRTMP))
	srvA := startBin(t, binPath, aCfg, aHTTP)
	defer srvA()

	// Publish into A. The push relay will then forward to B.
	pub := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-re", "-f", "lavfi",
		"-i", "testsrc=duration=60:size=160x120:rate=15",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "15",
		"-f", "flv", fmt.Sprintf("rtmp://127.0.0.1:%d/live/relayed", aRTMP))
	pub.Stderr = os.Stderr
	if err := pub.Start(); err != nil {
		t.Fatalf("publisher: %v", err)
	}
	defer func() { _ = pub.Process.Signal(syscall.SIGTERM); _ = pub.Wait() }()

	// Wait for the relay to land the stream on B.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/streams", bHTTP))
		if err == nil {
			body, _ := readAllClose(resp)
			if strings.Contains(body, `"name":"relayed"`) && strings.Contains(body, `"has_publisher":true`) {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("push relay never delivered the stream to server B within 40s")
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func startBin(t *testing.T, binPath, cfgPath string, httpPort int) func() {
	t.Helper()
	cmd := exec.Command(binPath, "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := WaitForHealth(httpPort, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("server %d not ready: %v", httpPort, err)
	}
	return func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		_ = cmd.Wait()
	}
}

func readAllClose(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	return string(buf), err
}
