//go:build !ffmpeg
// +build !ffmpeg

// If you are AI: This file provides stub implementations when FFmpeg is not available.
// All functions return errors indicating FFmpeg is not compiled in.

package ffx

import "errors"

var ErrFFmpegNotAvailable = errors.New("FFmpeg support not compiled in (build with -tags ffmpeg)")

// Init initializes FFmpeg libraries.
// Stub: returns error indicating FFmpeg is not available.
func Init() error {
	return ErrFFmpegNotAvailable
}

// Cleanup cleans up FFmpeg resources.
// Stub: no-op.
func Cleanup() {
	// No-op
}

// IsAvailable returns whether FFmpeg is available.
// Stub: always returns false.
func IsAvailable() bool {
	return false
}
