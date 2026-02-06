//go:build ffmpeg
// +build ffmpeg

// If you are AI: This file contains tests for FFmpeg initialization.
// Tests verify Init/Cleanup and availability checks.

package ffx

import "testing"

func TestInitCleanup(t *testing.T) {
	// Test that Init and Cleanup work
	if err := Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	if !IsAvailable() {
		t.Error("IsAvailable() returned false after Init()")
	}

	Cleanup()

	// After cleanup, availability may be false
	// (depends on implementation)
}

func TestIsAvailable(t *testing.T) {
	// Before Init, should be false
	Cleanup() // Ensure clean state
	if IsAvailable() {
		t.Error("IsAvailable() returned true before Init()")
	}

	// After Init, should be true
	if err := Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	if !IsAvailable() {
		t.Error("IsAvailable() returned false after Init()")
	}

	Cleanup()
}
