//go:build !ffmpeg
// +build !ffmpeg

// If you are AI: This file provides stub implementations for FFmpeg input operations.

package ffx

// Input represents an FFmpeg input context.
// Stub implementation.
type Input struct{}

// NewInput creates a new input context.
// Stub: returns error.
func NewInput(url string) (*Input, error) {
	return nil, ErrFFmpegNotAvailable
}

// Close closes the input context.
// Stub: no-op.
func (in *Input) Close() error {
	return nil
}

// ReadPacket reads a packet from the input.
// Stub: returns error.
func (in *Input) ReadPacket() ([]byte, error) {
	return nil, ErrFFmpegNotAvailable
}
