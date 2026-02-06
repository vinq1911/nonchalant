// If you are AI: This file implements FLV tag creation and encoding.
// Tags are created from MediaMessage instances with minimal allocations.

package flv

import (
	"encoding/binary"
)

// Tag represents an FLV tag (audio, video, or script).
type Tag struct {
	Type      byte
	Timestamp uint32
	Data      []byte
}

// Bytes encodes the tag as FLV tag bytes.
// Format: tag type (1) + data size (3) + timestamp (3) + timestamp extended (1) + stream ID (3) + data (N) + previous tag size (4)
// Allocation: Creates new slice for tag header, reuses data slice.
func (t *Tag) Bytes() []byte {
	dataSize := uint32(len(t.Data))

	// Tag header: 11 bytes
	header := make([]byte, 11)
	header[0] = t.Type

	// Data size (3 bytes, big-endian)
	header[1] = byte(dataSize >> 16)
	header[2] = byte(dataSize >> 8)
	header[3] = byte(dataSize)

	// Timestamp (3 bytes) + extended (1 byte)
	timestamp := t.Timestamp
	if timestamp >= 0xFFFFFF {
		header[4] = 0xFF
		header[5] = 0xFF
		header[6] = 0xFF
		header[7] = 1 // Extended timestamp flag
		// Extended timestamp will be written after header
	} else {
		header[4] = byte(timestamp >> 16)
		header[5] = byte(timestamp >> 8)
		header[6] = byte(timestamp)
		header[7] = 0 // No extended timestamp
	}

	// Stream ID (3 bytes, always 0)
	header[8] = 0
	header[9] = 0
	header[10] = 0

	// Calculate total tag size (header + data + previous tag size)
	tagSize := 11 + len(t.Data)
	if timestamp >= 0xFFFFFF {
		tagSize += 4 // Extended timestamp
	}
	tagSize += 4 // Previous tag size

	// Build complete tag
	result := make([]byte, tagSize)
	copy(result, header)
	offset := 11

	// Extended timestamp if needed
	if timestamp >= 0xFFFFFF {
		binary.BigEndian.PutUint32(result[offset:offset+4], timestamp)
		offset += 4
	}

	// Data
	copy(result[offset:], t.Data)
	offset += len(t.Data)

	// Previous tag size (4 bytes, big-endian)
	prevSize := uint32(11 + len(t.Data))
	if timestamp >= 0xFFFFFF {
		prevSize += 4
	}
	binary.BigEndian.PutUint32(result[offset:offset+4], prevSize)

	return result
}

// NewTag creates a new FLV tag from type, timestamp, and data.
func NewTag(tagType byte, timestamp uint32, data []byte) *Tag {
	return &Tag{
		Type:      tagType,
		Timestamp: timestamp,
		Data:      data,
	}
}
