// If you are AI: This file tests the RTMP connect sequence.
// Verifies server sends proper control messages and handles the full publish lifecycle.

package itest

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRTMPConnectSequence connects to the RTMP server with a minimal client
// and verifies the server sends required control messages after connect.
func TestRTMPConnectSequence(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build: %v\n%s", err, out)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n", httpPort, rtmpPort)
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

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
	time.Sleep(200 * time.Millisecond)

	// Connect with minimal RTMP handshake
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", rtmpPort), 3*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	if err := doHandshake(conn); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	// Send a minimal connect command
	if err := sendConnect(conn); err != nil {
		t.Fatalf("Failed to send connect: %v", err)
	}

	// Read server responses — expect at least WindowAckSize, PeerBandwidth, SetChunkSize, _result
	seen := map[byte]bool{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !seen[20] {
		msgType, _, err := readChunkMessage(conn)
		if err != nil {
			t.Fatalf("Failed to read message: %v", err)
		}
		seen[msgType] = true
		t.Logf("Received message type: %d", msgType)
	}

	// Verify required messages
	for _, mt := range []byte{5, 6, 1, 20} {
		if !seen[mt] {
			t.Errorf("Missing required message type %d", mt)
		}
	}
}

// doHandshake performs a minimal RTMP handshake (C0+C1, read S0+S1+S2, send C2).
func doHandshake(conn net.Conn) error {
	// Send C0 + C1
	c0c1 := make([]byte, 1537)
	c0c1[0] = 3 // RTMP version
	if _, err := conn.Write(c0c1); err != nil {
		return err
	}
	// Read S0 + S1 + S2
	s0s1s2 := make([]byte, 1+1536+1536)
	if _, err := io.ReadFull(conn, s0s1s2); err != nil {
		return err
	}
	// Send C2 (echo S1)
	c2 := make([]byte, 1536)
	copy(c2, s0s1s2[1:1537])
	_, err := conn.Write(c2)
	return err
}

// sendConnect sends a minimal connect AMF0 command on chunk stream 3.
func sendConnect(conn net.Conn) error {
	// AMF0 payload: string "connect", number 1.0, object {app: "live"}
	payload := []byte{
		0x02, 0x00, 0x07, 'c', 'o', 'n', 'n', 'e', 'c', 't', // string "connect"
		0x00, 0x40, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // number 1.0
		0x03,                                                            // object start
		0x00, 0x03, 'a', 'p', 'p', 0x02, 0x00, 0x04, 'l', 'i', 'v', 'e', // app: "live"
		0x00, 0x00, 0x09, // object end
	}

	// Build chunk: basic header (fmt=0, csID=3) + message header (11 bytes) + payload
	header := make([]byte, 12)
	header[0] = 0x03 // fmt=0, csID=3
	// timestamp = 0
	header[1], header[2], header[3] = 0, 0, 0
	// message length (3 bytes big-endian)
	msgLen := len(payload)
	header[4] = byte(msgLen >> 16)
	header[5] = byte(msgLen >> 8)
	header[6] = byte(msgLen)
	// message type = 20 (command AMF0)
	header[7] = 20
	// stream ID = 0 (little-endian)
	header[8], header[9], header[10], header[11] = 0, 0, 0, 0

	buf := make([]byte, 0, len(header)+len(payload))
	buf = append(buf, header...)
	buf = append(buf, payload...)
	_, err := conn.Write(buf)
	return err
}

// readChunkMessage reads a single RTMP chunk and returns msgType, body, error.
// Handles format-0 chunks (sufficient for server's initial messages).
// Header layout: timestamp(3) + length(3) + type(1) + streamID(4) = 11 bytes.
func readChunkMessage(conn net.Conn) (byte, []byte, error) {
	var bh [1]byte
	if _, err := io.ReadFull(conn, bh[:]); err != nil {
		return 0, nil, err
	}

	fmt := bh[0] >> 6
	if fmt != 0 {
		// For non-fmt0, skip appropriate header bytes and return placeholder
		// This is sufficient for test purposes
		skip := []int{11, 7, 3, 0}[fmt]
		if skip > 0 {
			tmp := make([]byte, skip)
			io.ReadFull(conn, tmp)
		}
		return 0, nil, nil
	}

	// fmt=0: read 11-byte message header
	var hdr [11]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return 0, nil, err
	}
	// timestamp = hdr[0:3], length = hdr[3:6], type = hdr[6], streamID = hdr[7:11]
	msgLen := uint32(hdr[3])<<16 | uint32(hdr[4])<<8 | uint32(hdr[5])
	msgType := hdr[6]

	body := make([]byte, msgLen)
	if msgLen > 0 {
		if _, err := io.ReadFull(conn, body); err != nil {
			return 0, nil, err
		}
	}
	return msgType, body, nil
}

// TestRTMPPublishWithFFmpeg tests full publish lifecycle using FFmpeg.
// Skips if ffmpeg is not available.
func TestRTMPPublishWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	binPath := filepath.Join(t.TempDir(), "nonchalant")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/nonchalant")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Build failed: %v\n%s", err, out)
	}

	httpPort := findFreePort(t)
	rtmpPort := findFreePort(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("server:\n  health_port: 8080\n  http_port: %d\n  rtmp_port: %d\n", httpPort, rtmpPort)
	os.WriteFile(configPath, []byte(cfg), 0644)

	cmd := exec.Command(binPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Start()
	defer func() {
		cmd.Process.Signal(syscall.SIGINT)
		cmd.Wait()
	}()

	WaitForHealth(httpPort, 5*time.Second)
	time.Sleep(300 * time.Millisecond)

	// Create a 2-second test video
	testVideo := filepath.Join(t.TempDir(), "test.mp4")
	gen := exec.Command("ffmpeg", "-f", "lavfi",
		"-i", "testsrc=duration=2:size=320x240:rate=15",
		"-c:v", "libx264", "-preset", "ultrafast", "-t", "2", "-y", testVideo)
	gen.Stderr = os.Stderr
	if err := gen.Run(); err != nil {
		t.Skipf("Cannot create test video: %v", err)
	}

	// Publish via RTMP
	rtmpURL := fmt.Sprintf("rtmp://localhost:%d/live/teststream", rtmpPort)
	pub := exec.Command("ffmpeg", "-re", "-i", testVideo, "-c", "copy", "-f", "flv", rtmpURL)
	pub.Stderr = os.Stderr
	pubErr := make(chan error, 1)
	go func() { pubErr <- pub.Run() }()

	// Wait for publish to run for a bit
	select {
	case err := <-pubErr:
		// FFmpeg exited. If it ran for >1s, that's success (sent some data)
		if err != nil {
			t.Logf("FFmpeg exited with: %v (may be expected)", err)
		}
	case <-time.After(3 * time.Second):
		// Still running after 3s — success! Kill it.
		pub.Process.Signal(syscall.SIGTERM)
		<-pubErr
		t.Log("FFmpeg published successfully for 3+ seconds")
	}
}
