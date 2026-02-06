//go:build !ffmpeg
// +build !ffmpeg

// If you are AI: This file provides stub implementations for FFmpeg output operations.

package ffx

// Output represents an FFmpeg output context.
// Stub implementation.
type Output struct{}

// NewOutput creates a new output context.
// Stub: returns error.
func NewOutput(url string, format string) (*Output, error) {
	return nil, ErrFFmpegNotAvailable
}

// Close closes the output context.
// Stub: no-op.
func (out *Output) Close() error {
	return nil
}

// WritePacket writes a packet to the output.
// Stub: returns error.
func (out *Output) WritePacket(data []byte) error {
	return ErrFFmpegNotAvailable
}
