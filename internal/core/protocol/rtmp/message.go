// If you are AI: This file handles RTMP message parsing and creation.
// Messages are parsed from chunk data and converted to appropriate types.

package rtmp

import (
	"encoding/binary"
	"io"
)

// Message represents a parsed RTMP message.
type Message struct {
	Type      byte
	Length    uint32
	Timestamp uint32
	StreamID  uint32
	Body      []byte
}

// ParseSetChunkSize parses a Set Chunk Size message.
func ParseSetChunkSize(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	size := binary.BigEndian.Uint32(body[0:4])
	if size > MaxChunkSize {
		return 0, ErrChunkTooLarge
	}
	return size, nil
}

// CreateSetChunkSize creates a Set Chunk Size message body.
func CreateSetChunkSize(size uint32) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, size)
	return body
}

// CreateWindowAckSize creates a Window Acknowledgement Size message body.
func CreateWindowAckSize(size uint32) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, size)
	return body
}

// CreateSetPeerBandwidth creates a Set Peer Bandwidth message body.
func CreateSetPeerBandwidth(size uint32, limitType byte) []byte {
	body := make([]byte, 5)
	binary.BigEndian.PutUint32(body[0:4], size)
	body[4] = limitType
	return body
}

// CreateStreamBegin creates a Stream Begin control message.
func CreateStreamBegin(streamID uint32) []byte {
	body := make([]byte, 6)
	body[0] = ControlStreamBegin
	body[1] = 0
	binary.BigEndian.PutUint32(body[2:6], streamID)
	return body
}

// WriteChunk writes a message as RTMP chunks.
// Allocation: Uses pre-allocated buffers, minimal allocations.
// NOTE: If w implements Flusher, call Flush() after writing to ensure immediate transmission.
func WriteChunk(w io.Writer, csID uint32, msgType byte, timestamp uint32, streamID uint32, body []byte, chunkSize uint32) error {
	bodyLen := uint32(len(body))
	offset := uint32(0)

	for offset < bodyLen {
		// Determine chunk format
		var fmt byte
		if offset == 0 {
			fmt = ChunkFmt0 // First chunk
		} else {
			fmt = ChunkFmt3 // Continuation chunks
		}

		// Write basic header
		basicHeader := (fmt << 6)
		if csID < 64 {
			basicHeader |= byte(csID)
			if err := binary.Write(w, binary.BigEndian, basicHeader); err != nil {
				return err
			}
		} else if csID < 320 {
			basicHeader |= 0
			if err := binary.Write(w, binary.BigEndian, basicHeader); err != nil {
				return err
			}
			if err := binary.Write(w, binary.BigEndian, byte(csID-64)); err != nil {
				return err
			}
		} else {
			basicHeader |= 1
			if err := binary.Write(w, binary.BigEndian, basicHeader); err != nil {
				return err
			}
			if err := binary.Write(w, binary.BigEndian, uint16(csID-64)); err != nil {
				return err
			}
		}

		// Write message header (fmt 0)
		if fmt == ChunkFmt0 {
			// Timestamp (3 bytes, use extended if needed)
			ts := timestamp
			if ts >= 0xFFFFFF {
				ts = 0xFFFFFF
			}
			header := make([]byte, 11)
			header[0] = byte(ts >> 16)
			header[1] = byte(ts >> 8)
			header[2] = byte(ts)
			header[3] = byte(bodyLen >> 16)
			header[4] = byte(bodyLen >> 8)
			header[5] = byte(bodyLen)
			header[6] = msgType
			// Stream ID is little-endian in RTMP (per go2rtc reference)
			binary.LittleEndian.PutUint32(header[7:11], streamID)
			if _, err := w.Write(header); err != nil {
				return err
			}
			// Extended timestamp if needed
			if timestamp >= 0xFFFFFF {
				if err := binary.Write(w, binary.BigEndian, timestamp); err != nil {
					return err
				}
			}
		}

		// Write chunk payload
		chunkLen := chunkSize
		if offset+chunkLen > bodyLen {
			chunkLen = bodyLen - offset
		}
		if _, err := w.Write(body[offset : offset+chunkLen]); err != nil {
			return err
		}
		offset += chunkLen
	}

	// Flush if the writer supports it (e.g., net.Conn, bufio.Writer)
	if flusher, ok := w.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}

	return nil
}
