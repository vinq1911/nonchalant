// If you are AI: This file implements FLV file header generation.
// FLV header is written once at the start of the stream.

package flv

// Header represents an FLV file header.
type Header struct {
	HasAudio bool
	HasVideo bool
}

// Bytes returns the FLV header as a byte slice.
// Allocation: Pre-allocated 9-byte slice, no heap allocations.
func (h *Header) Bytes() []byte {
	header := make([]byte, FLVHeaderSize)

	// Signature "FLV" (3 bytes)
	copy(header[0:3], FLVSignature)

	// Version (1 byte)
	header[3] = FLVVersion

	// Flags (1 byte): audio and video flags
	flags := byte(0)
	if h.HasAudio {
		flags |= 0x04
	}
	if h.HasVideo {
		flags |= 0x01
	}
	header[4] = flags

	// Data offset (4 bytes, big-endian)
	// Points to start of first tag (after header + previous tag size)
	offset := uint32(FLVHeaderSize + 4) // header + previous tag size
	header[5] = byte(offset >> 24)
	header[6] = byte(offset >> 16)
	header[7] = byte(offset >> 8)
	header[8] = byte(offset)

	return header
}

// NewHeader creates a new FLV header with specified audio/video flags.
func NewHeader(hasAudio, hasVideo bool) *Header {
	return &Header{
		HasAudio: hasAudio,
		HasVideo: hasVideo,
	}
}
