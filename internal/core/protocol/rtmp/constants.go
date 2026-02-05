// If you are AI: This file defines RTMP protocol constants and message types.

package rtmp

// RTMP version constant
const RTMPVersion = 3

// Handshake sizes
const (
	HandshakeC0C1Size = 1537 // C0 (1 byte) + C1 (1536 bytes)
	HandshakeS0S1Size = 1537 // S0 (1 byte) + S1 (1536 bytes)
	HandshakeS2Size   = 1536 // S2 (1536 bytes)
	HandshakeC2Size   = 1536 // C2 (1536 bytes)
)

// Default chunk size
const DefaultChunkSize = 128

// Maximum chunk size
const MaxChunkSize = 16777215 // 2^24 - 1

// Message type IDs
const (
	MessageTypeSetChunkSize     = 1
	MessageTypeAbortMessage     = 2
	MessageTypeAck              = 3
	MessageTypeUserCtrl         = 4
	MessageTypeWinAckSize       = 5
	MessageTypeSetPeerBandwidth = 6
	MessageTypeAudio            = 8
	MessageTypeVideo            = 9
	MessageTypeDataAMF0         = 18
	MessageTypeSharedObjectAMF0 = 19
	MessageTypeCommandAMF0      = 20
)

// Chunk basic header format types
const (
	ChunkFmt0 = 0 // 11-byte header
	ChunkFmt1 = 1 // 7-byte header
	ChunkFmt2 = 2 // 3-byte header
	ChunkFmt3 = 3 // 0-byte header
)

// Control message types
const (
	ControlStreamBegin      = 0
	ControlStreamEOF        = 1
	ControlStreamDry        = 2
	ControlSetBufferLength  = 3
	ControlStreamIsRecorded = 4
	ControlPingRequest      = 6
	ControlPingResponse     = 7
)
