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
	chunkStreamID  uint32
	messageType    byte
	messageLength  uint32
	timestamp      uint32
	timestampDelta uint32
	hasExtendedTS  bool   // True if last fmt0/1/2 used extended timestamp (0xFFFFFF marker)
	streamID       uint32 // Stream ID from message header (format 0 only)
	buffer         []byte
	bytesRead      uint32
}

// ChunkParser parses RTMP chunks and reassembles messages.
// Uses pooled buffers to minimize allocations.
type ChunkParser struct {
	chunkStreams map[uint32]*ChunkStream
	chunkSize    uint32 // Incoming (read) chunk size — updated only when client sends SetChunkSize
	mu           sync.RWMutex
	bufferPool   sync.Pool
}

// NewChunkParser creates a new chunk parser with default 128-byte incoming chunk size.
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

// SetChunkSize sets the incoming (read) chunk size.
// Call this ONLY when a SetChunkSize message is received from the remote peer.
func (p *ChunkParser) SetChunkSize(size uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chunkSize = size
}

// GetChunkSize returns the current incoming chunk size.
func (p *ChunkParser) GetChunkSize() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.chunkSize
}

// ReadChunk reads and parses a chunk from the reader.
// Returns the chunk stream ID and any error.
// NOTE: Uses the global parser chunkSize (not per-stream) because chunk size is per-direction.
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
		cs = &ChunkStream{chunkStreamID: csID}
		p.chunkStreams[csID] = cs
	}
	currentChunkSize := p.chunkSize
	p.mu.Unlock()

	// Read message header based on format
	if err := p.readMessageHeader(r, cs, fmt); err != nil {
		return csID, err
	}

	// Calculate payload to read for this chunk — use global parser chunk size
	payloadSize := currentChunkSize
	if cs.bytesRead+payloadSize > cs.messageLength {
		payloadSize = cs.messageLength - cs.bytesRead
	}

	// Read payload
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return csID, err
	}

	// Extend buffer if needed
	if len(cs.buffer) == 0 {
		cs.buffer = make([]byte, 0, cs.messageLength)
	}

	cs.buffer = append(cs.buffer, payload...)
	cs.bytesRead += payloadSize

	return csID, nil
}

// GetBytesReadForChunk returns the number of bytes read for a specific chunk stream.
// This is used for ACK tracking.
func (p *ChunkParser) GetBytesReadForChunk(csID uint32) uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cs, exists := p.chunkStreams[csID]
	if !exists {
		return 0
	}
	return cs.bytesRead
}

// readMessageHeader reads the message header based on format type.
func (p *ChunkParser) readMessageHeader(r io.Reader, cs *ChunkStream, fmt byte) error {
	switch fmt {
	case ChunkFmt0:
		return p.readFmt0(r, cs)
	case ChunkFmt1:
		return p.readFmt1(r, cs)
	case ChunkFmt2:
		return p.readFmt2(r, cs)
	case ChunkFmt3:
		return p.readFmt3(r, cs)
	}
	return nil
}

// readFmt0 reads an 11-byte format-0 header: timestamp(3) + length(3) + type(1) + streamID(4).
func (p *ChunkParser) readFmt0(r io.Reader, cs *ChunkStream) error {
	var tsBytes [3]byte
	if _, err := io.ReadFull(r, tsBytes[:]); err != nil {
		return err
	}
	timestamp := uint32(tsBytes[0])<<16 | uint32(tsBytes[1])<<8 | uint32(tsBytes[2])

	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	cs.hasExtendedTS = (timestamp == 0xFFFFFF)
	if cs.hasExtendedTS {
		var extTS uint32
		if err := binary.Read(r, binary.BigEndian, &extTS); err != nil {
			return err
		}
		cs.timestamp = extTS
	} else {
		cs.timestamp = timestamp
	}
	cs.messageLength = uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	cs.messageType = header[3]
	cs.streamID = binary.LittleEndian.Uint32(header[4:8])
	cs.bytesRead = 0
	cs.buffer = cs.buffer[:0]
	return nil
}

// readFmt1 reads a 7-byte format-1 header: delta(3) + length(3) + type(1).
func (p *ChunkParser) readFmt1(r io.Reader, cs *ChunkStream) error {
	var deltaBytes [3]byte
	if _, err := io.ReadFull(r, deltaBytes[:]); err != nil {
		return err
	}
	delta := uint32(deltaBytes[0])<<16 | uint32(deltaBytes[1])<<8 | uint32(deltaBytes[2])

	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	cs.hasExtendedTS = (delta == 0xFFFFFF)
	if cs.hasExtendedTS {
		var extTS uint32
		if err := binary.Read(r, binary.BigEndian, &extTS); err != nil {
			return err
		}
		cs.timestampDelta = extTS
	} else {
		cs.timestampDelta = delta
	}
	cs.timestamp += cs.timestampDelta
	cs.messageLength = uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	cs.messageType = header[3]
	cs.bytesRead = 0
	cs.buffer = cs.buffer[:0]
	return nil
}

// readFmt2 reads a 3-byte format-2 header: delta(3).
func (p *ChunkParser) readFmt2(r io.Reader, cs *ChunkStream) error {
	var deltaBytes [3]byte
	if _, err := io.ReadFull(r, deltaBytes[:]); err != nil {
		return err
	}
	delta := uint32(deltaBytes[0])<<16 | uint32(deltaBytes[1])<<8 | uint32(deltaBytes[2])

	cs.hasExtendedTS = (delta == 0xFFFFFF)
	if cs.hasExtendedTS {
		var extTS uint32
		if err := binary.Read(r, binary.BigEndian, &extTS); err != nil {
			return err
		}
		cs.timestampDelta = extTS
	} else {
		cs.timestampDelta = delta
	}
	cs.timestamp += cs.timestampDelta
	return nil
}

// readFmt3 reads a format-3 header (no message header, reuse previous values).
// CRITICAL: fmt3 is used for two cases:
//   - Continuation chunks (bytesRead > 0): same message split across chunks — do NOT add delta.
//   - New message (bytesRead == 0): same params as previous message — add delta once.
//
// Per RTMP spec, if the preceding chunk used extended timestamp (0xFFFFFF marker),
// fmt3 chunks also carry the 4-byte extended timestamp field that must be consumed.
func (p *ChunkParser) readFmt3(r io.Reader, cs *ChunkStream) error {
	// If preceding chunk used extended timestamp, consume the 4-byte field
	if cs.hasExtendedTS {
		var extTS uint32
		if err := binary.Read(r, binary.BigEndian, &extTS); err != nil {
			return err
		}
		// Only apply for new messages, not continuations
		if cs.bytesRead == 0 {
			cs.timestamp = extTS
		}
		return nil
	}

	// Normal timestamps: apply delta only for new messages (first chunk)
	if cs.bytesRead == 0 {
		cs.timestamp += cs.timestampDelta
	}
	return nil
}

// GetCompleteMessage returns the complete message if reassembly is done.
// Returns: body, messageType, timestamp, streamID, complete.
func (p *ChunkParser) GetCompleteMessage(csID uint32) ([]byte, byte, uint32, uint32, bool) {
	p.mu.RLock()
	cs, exists := p.chunkStreams[csID]
	p.mu.RUnlock()

	if !exists || cs.bytesRead < cs.messageLength {
		return nil, 0, 0, 0, false
	}

	msg := make([]byte, len(cs.buffer))
	copy(msg, cs.buffer)
	msgType := cs.messageType
	timestamp := cs.timestamp
	streamID := cs.streamID

	cs.buffer = cs.buffer[:0]
	cs.bytesRead = 0

	return msg, msgType, timestamp, streamID, true
}
