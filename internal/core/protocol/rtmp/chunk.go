// If you are AI: This file implements RTMP chunk parsing and reassembly.
// Chunk reassembly uses pooled buffers to avoid allocations in hot loops.

package rtmp

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
)

var (
	ErrInvalidChunkHeader = errors.New("invalid chunk header")
	ErrChunkTooLarge      = errors.New("chunk size too large")
)

// ChunkStream represents a chunk stream for message reassembly.
// Each chunk stream ID has its own reassembly buffer.
type ChunkStream struct {
	chunkStreamID     uint32
	messageType       byte
	messageLength     uint32
	timestamp         uint32
	timestampDelta    uint32
	extendedTimestamp uint32
	buffer            []byte
	bytesRead         uint32
	chunkSize         uint32
}

// ChunkParser parses RTMP chunks and reassembles messages.
// Uses pooled buffers to minimize allocations.
type ChunkParser struct {
	chunkStreams map[uint32]*ChunkStream
	chunkSize    uint32
	mu           sync.RWMutex
	bufferPool   sync.Pool
}

// NewChunkParser creates a new chunk parser.
func NewChunkParser() *ChunkParser {
	return &ChunkParser{
		chunkStreams: make(map[uint32]*ChunkStream),
		chunkSize:    DefaultChunkSize,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 64*1024)
			},
		},
	}
}

// SetChunkSize sets the chunk size for outgoing chunks.
func (p *ChunkParser) SetChunkSize(size uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chunkSize = size
}

// ReadChunk reads and parses a chunk from the reader.
// Returns the chunk stream ID and any error.
// Allocation: Uses pooled buffers for message reassembly.
func (p *ChunkParser) ReadChunk(r io.Reader) (uint32, error) {
	// Read basic header (first byte)
	var basicHeader byte
	if err := binary.Read(r, binary.BigEndian, &basicHeader); err != nil {
		return 0, err
	}

	// Extract format and chunk stream ID
	fmt := (basicHeader >> 6) & 0x03
	csID := uint32(basicHeader & 0x3F)

	// Extended chunk stream ID (if csID == 0)
	if csID == 0 {
		var extID byte
		if err := binary.Read(r, binary.BigEndian, &extID); err != nil {
			return 0, err
		}
		csID = uint32(extID) + 64
	} else if csID == 1 {
		// 2-byte extended ID
		var extID uint16
		if err := binary.Read(r, binary.BigEndian, &extID); err != nil {
			return 0, err
		}
		csID = uint32(extID) + 64
	}

	// Get or create chunk stream
	p.mu.Lock()
	cs, exists := p.chunkStreams[csID]
	if !exists {
		cs = &ChunkStream{
			chunkStreamID: csID,
			chunkSize:     p.chunkSize,
		}
		p.chunkStreams[csID] = cs
	}
	p.mu.Unlock()

	// Read message header based on format
	if err := p.readMessageHeader(r, cs, fmt); err != nil {
		return csID, err
	}

	// Read chunk payload
	payloadSize := cs.chunkSize
	if cs.bytesRead+payloadSize > cs.messageLength {
		payloadSize = cs.messageLength - cs.bytesRead
	}

	// Get buffer from pool
	buf := p.bufferPool.Get().([]byte)
	if cap(buf) < int(cs.messageLength) {
		buf = make([]byte, 0, cs.messageLength)
	}
	buf = buf[:0]

	// Extend buffer if needed
	if len(cs.buffer) == 0 {
		cs.buffer = make([]byte, 0, cs.messageLength)
	}

	// Read payload
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return csID, err
	}

	cs.buffer = append(cs.buffer, payload...)
	cs.bytesRead += payloadSize

	return csID, nil
}

// readMessageHeader reads the message header based on format type.
func (p *ChunkParser) readMessageHeader(r io.Reader, cs *ChunkStream, fmt byte) error {
	switch fmt {
	case ChunkFmt0:
		// 11 bytes: timestamp (3) + length (3) + type (1) + stream ID (4)
		// Read timestamp as 3 bytes
		var tsBytes [3]byte
		if _, err := io.ReadFull(r, tsBytes[:]); err != nil {
			return err
		}
		timestamp := uint32(tsBytes[0])<<16 | uint32(tsBytes[1])<<8 | uint32(tsBytes[2])

		// Read remaining header (8 bytes)
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return err
		}

		if timestamp == 0xFFFFFF {
			// Extended timestamp
			if err := binary.Read(r, binary.BigEndian, &cs.extendedTimestamp); err != nil {
				return err
			}
			cs.timestamp = cs.extendedTimestamp
		} else {
			cs.timestamp = timestamp
		}
		cs.messageLength = uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
		cs.messageType = header[3]
		// Stream ID is in header[4:8], but we don't use it for now
		cs.bytesRead = 0
		cs.buffer = cs.buffer[:0]

	case ChunkFmt1:
		// 7 bytes: timestamp delta (3) + length (3) + type (1)
		var deltaBytes [3]byte
		if _, err := io.ReadFull(r, deltaBytes[:]); err != nil {
			return err
		}
		delta := uint32(deltaBytes[0])<<16 | uint32(deltaBytes[1])<<8 | uint32(deltaBytes[2])

		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return err
		}

		if delta == 0xFFFFFF {
			if err := binary.Read(r, binary.BigEndian, &cs.extendedTimestamp); err != nil {
				return err
			}
			cs.timestampDelta = cs.extendedTimestamp
		} else {
			cs.timestampDelta = delta
		}
		cs.timestamp += cs.timestampDelta
		cs.messageLength = uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
		cs.messageType = header[3]
		cs.bytesRead = 0
		cs.buffer = cs.buffer[:0]

	case ChunkFmt2:
		// 3 bytes: timestamp delta (3)
		var deltaBytes [3]byte
		if _, err := io.ReadFull(r, deltaBytes[:]); err != nil {
			return err
		}
		delta := uint32(deltaBytes[0])<<16 | uint32(deltaBytes[1])<<8 | uint32(deltaBytes[2])

		if delta == 0xFFFFFF {
			if err := binary.Read(r, binary.BigEndian, &cs.extendedTimestamp); err != nil {
				return err
			}
			cs.timestampDelta = cs.extendedTimestamp
		} else {
			cs.timestampDelta = delta
		}
		cs.timestamp += cs.timestampDelta

	case ChunkFmt3:
		// No header, use previous values
		// Update timestamp if extended
		if cs.timestamp == 0xFFFFFF || cs.timestampDelta == 0xFFFFFF {
			if err := binary.Read(r, binary.BigEndian, &cs.extendedTimestamp); err != nil {
				return err
			}
			cs.timestamp = cs.extendedTimestamp
		} else {
			cs.timestamp += cs.timestampDelta
		}
	}

	return nil
}

// GetCompleteMessage returns the complete message if reassembly is complete.
// Returns nil if message is not yet complete.
func (p *ChunkParser) GetCompleteMessage(csID uint32) ([]byte, byte, uint32, bool) {
	p.mu.RLock()
	cs, exists := p.chunkStreams[csID]
	p.mu.RUnlock()

	if !exists || cs.bytesRead < cs.messageLength {
		return nil, 0, 0, false
	}

	// Message is complete
	msg := make([]byte, len(cs.buffer))
	copy(msg, cs.buffer)
	msgType := cs.messageType
	timestamp := cs.timestamp

	// Reset for next message
	cs.buffer = cs.buffer[:0]
	cs.bytesRead = 0

	return msg, msgType, timestamp, true
}
