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
// Format: tag type (1) + data size (3) + timestamp lower (3) + timestamp upper (1) + stream ID (3) + data (N) + previous tag size (4)
// Allocation: Creates new slice for complete tag, reuses data slice.
func (t *Tag) Bytes() []byte {
	dataSize := uint32(len(t.Data))

	// Total: 11-byte header + data + 4-byte previous tag size
	totalSize := 11 + len(t.Data) + 4
	result := make([]byte, totalSize)

	// Tag type (1 byte)
	result[0] = t.Type

	// Data size (3 bytes, big-endian)
	result[1] = byte(dataSize >> 16)
	result[2] = byte(dataSize >> 8)
	result[3] = byte(dataSize)

	// Timestamp: lower 24 bits in bytes 4-6, upper 8 bits in byte 7 (per FLV spec)
	result[4] = byte(t.Timestamp >> 16)
	result[5] = byte(t.Timestamp >> 8)
	result[6] = byte(t.Timestamp)
	result[7] = byte(t.Timestamp >> 24) // TimestampExtended

	// Stream ID (3 bytes, always 0)
	result[8] = 0
	result[9] = 0
	result[10] = 0

	// Data
	copy(result[11:], t.Data)

	// Previous tag size (4 bytes, big-endian) = 11 + data size
	prevSize := uint32(11 + len(t.Data))
	binary.BigEndian.PutUint32(result[11+len(t.Data):], prevSize)

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
