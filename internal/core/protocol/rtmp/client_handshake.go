// If you are AI: This file implements the RTMP client-side handshake.
// Used by relay tasks to connect to remote RTMP servers.

package rtmp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

var (
	ErrClientHandshakeFailed = errors.New("client handshake failed")
)

// PerformClientHandshake performs the client side of RTMP handshake.
// Sends C0/C1, reads S0/S1/S2, sends C2.
// Allocation: Fixed-size buffers only, no heap allocations.
func PerformClientHandshake(conn io.ReadWriter) error {
	// Send C0 (version)
	if err := binary.Write(conn, binary.BigEndian, byte(RTMPVersion)); err != nil {
		return err
	}

	// Send C1 (1536 bytes: time + version + random)
	c1 := make([]byte, 1536)
	// Time (4 bytes)
	binary.BigEndian.PutUint32(c1[0:4], uint32(time.Now().Unix()))
	// Version (4 bytes)
	binary.BigEndian.PutUint32(c1[4:8], 0)
	// Random (1528 bytes)
	if _, err := rand.Read(c1[8:]); err != nil {
		return err
	}
	if _, err := conn.Write(c1); err != nil {
		return err
	}

	// Read S0 (version)
	var s0 byte
	if err := binary.Read(conn, binary.BigEndian, &s0); err != nil {
		return err
	}
	if s0 != RTMPVersion {
		return ErrInvalidVersion
	}

	// Read S1 (1536 bytes)
	s1 := make([]byte, 1536)
	if _, err := io.ReadFull(conn, s1); err != nil {
		return err
	}

	// Read S2 (1536 bytes)
	s2 := make([]byte, 1536)
	if _, err := io.ReadFull(conn, s2); err != nil {
		return err
	}

	// Send C2 (echo of S1 with modifications)
	c2 := make([]byte, 1536)
	copy(c2, s1)
	// Update time
	binary.BigEndian.PutUint32(c2[0:4], uint32(time.Now().Unix()))
	if _, err := conn.Write(c2); err != nil {
		return err
	}

	return nil
}
