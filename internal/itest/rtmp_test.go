// If you are AI: This file contains the legacy RTMP publish integration test.
// The primary RTMP tests are in rtmp_connect_test.go.

package itest

import (
	"fmt"
	"net"
	"testing"
)

// findFreePort finds a free TCP port.
func findFreePort(t *testing.T) int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// portToString converts a port number to string.
func portToString(port int) string {
	return fmt.Sprintf("%d", port)
}
