//go:build ffmpeg
// +build ffmpeg

// If you are AI: This file provides FFmpeg initialization and cleanup.
// FFmpeg code is isolated behind build tags.

package ffx

import (
	"errors"
)

var (
	ErrFFmpegInitFailed = errors.New("FFmpeg initialization failed")
	initialized         = false
)

// Init initializes FFmpeg libraries.
// Must be called before using any FFmpeg functions.
// NOTE: FFmpeg may have global state - this is documented as unavoidable.
func Init() error {
	// NOTE: Full implementation would call av_register_all() and similar
	// For now, this is a placeholder that demonstrates structure
	initialized = true
	return nil
}

// Cleanup cleans up FFmpeg resources.
// Should be called on shutdown.
func Cleanup() {
	initialized = false
	// NOTE: Full implementation would clean up FFmpeg global state
}

// IsAvailable returns whether FFmpeg is available.
func IsAvailable() bool {
	return initialized
}
