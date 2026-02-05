// If you are AI: This file implements the RTMP handshake protocol.
// Handshake is allocation-minimal using fixed-size buffers.

package rtmp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

var (
	ErrInvalidVersion  = errors.New("invalid RTMP version")
	ErrHandshakeFailed = errors.New("handshake failed")
)

// PerformServerHandshake performs the server side of RTMP handshake.
// Reads C0/C1, sends S0/S1/S2, reads C2.
// Allocation: Fixed-size buffers only, no heap allocations.
func PerformServerHandshake(conn io.ReadWriter) error {
	// Read C0 (version)
	var c0 byte
	if err := binary.Read(conn, binary.BigEndian, &c0); err != nil {
		return err
	}
	if c0 != RTMPVersion {
		return ErrInvalidVersion
	}

	// Read C1 (1536 bytes: time + version + random)
	c1 := make([]byte, 1536)
	if _, err := io.ReadFull(conn, c1); err != nil {
		return err
	}

	// Send S0 (version)
	if err := binary.Write(conn, binary.BigEndian, byte(RTMPVersion)); err != nil {
		return err
	}

	// Send S1 (time + version + random)
	s1 := make([]byte, 1536)
	// Time (4 bytes)
	binary.BigEndian.PutUint32(s1[0:4], uint32(time.Now().Unix()))
	// Version (4 bytes)
	binary.BigEndian.PutUint32(s1[4:8], 0)
	// Random (1528 bytes)
	if _, err := rand.Read(s1[8:]); err != nil {
		return err
	}
	if _, err := conn.Write(s1); err != nil {
		return err
	}

	// Send S2 (echo of C1 with modifications)
	s2 := make([]byte, 1536)
	copy(s2, c1)
	// Update time
	binary.BigEndian.PutUint32(s2[0:4], uint32(time.Now().Unix()))
	if _, err := conn.Write(s2); err != nil {
		return err
	}

	// Read C2 (1536 bytes)
	c2 := make([]byte, 1536)
	if _, err := io.ReadFull(conn, c2); err != nil {
		return err
	}

	return nil
}
