// If you are AI: This file implements a lock-free ring buffer for subscriber message delivery.
// The ring buffer provides bounded buffering with configurable backpressure behavior.

package bus

import (
	"sync/atomic"
)

// BackpressureStrategy defines how the ring buffer handles overflow.
type BackpressureStrategy uint8

const (
	// BackpressureDropOldest drops the oldest message when buffer is full.
	BackpressureDropOldest BackpressureStrategy = iota
	// BackpressureDropNewest drops the newest message when buffer is full.
	BackpressureDropNewest
)

// RingBuffer is a bounded circular buffer for MediaMessage delivery.
// It is lock-free for single producer, single consumer scenarios.
// NOTE: This implementation uses atomic operations for thread-safety.
// Allocation: Pre-allocated buffer, no per-message allocations.
type RingBuffer struct {
	buffer   []*MediaMessage // Pre-allocated message slots
	size     uint32          // Buffer size (power of 2 for efficient modulo)
	writePos uint32          // Write position (atomic)
	readPos  uint32          // Read position (atomic)
	strategy BackpressureStrategy
	dropped  uint64 // Counter for dropped messages (atomic)
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
// Capacity must be a power of 2 for efficient modulo operations.
func NewRingBuffer(capacity uint32, strategy BackpressureStrategy) *RingBuffer {
	// Round up to next power of 2
	actualSize := uint32(1)
	for actualSize < capacity {
		actualSize <<= 1
	}

	return &RingBuffer{
		buffer:   make([]*MediaMessage, actualSize),
		size:     actualSize,
		strategy: strategy,
	}
}

// Write attempts to write a message to the buffer.
// Returns true if written, false if buffer was full and message was dropped.
// Lock expectations: Single writer (publisher goroutine).
// NOTE: We reserve one slot to distinguish full from empty.
func (rb *RingBuffer) Write(msg *MediaMessage) bool {
	if msg == nil {
		return false
	}

	writePos := atomic.LoadUint32(&rb.writePos)
	readPos := atomic.LoadUint32(&rb.readPos)

	// Calculate next write position
	mask := rb.size - 1
	nextWritePos := (writePos + 1) & mask

	// Check if buffer is full (next write position equals read position)
	if nextWritePos == (readPos & mask) {
		// Buffer full - apply backpressure strategy
		atomic.AddUint64(&rb.dropped, 1)
		if rb.strategy == BackpressureDropOldest {
			// Advance read position to drop oldest
			atomic.AddUint32(&rb.readPos, 1)
		} else {
			// Drop newest (current message)
			return false
		}
	}

	// Write message at current write position
	rb.buffer[writePos&mask] = msg
	atomic.StoreUint32(&rb.writePos, nextWritePos)
	return true
}

// Read attempts to read a message from the buffer.
// Returns the message and true if available, nil and false if empty.
// Lock expectations: Single reader (subscriber goroutine).
func (rb *RingBuffer) Read() (*MediaMessage, bool) {
	readPos := atomic.LoadUint32(&rb.readPos)
	writePos := atomic.LoadUint32(&rb.writePos)

	if readPos == writePos {
		// Buffer empty
		return nil, false
	}

	msg := rb.buffer[readPos&(rb.size-1)]
	atomic.AddUint32(&rb.readPos, 1)
	return msg, true
}

// Dropped returns the number of messages dropped due to backpressure.
func (rb *RingBuffer) Dropped() uint64 {
	return atomic.LoadUint64(&rb.dropped)
}

// Available returns the number of available slots in the buffer.
// NOTE: One slot is reserved to distinguish full from empty.
func (rb *RingBuffer) Available() uint32 {
	writePos := atomic.LoadUint32(&rb.writePos)
	readPos := atomic.LoadUint32(&rb.readPos)
	mask := rb.size - 1
	nextWritePos := (writePos + 1) & mask
	nextReadPos := readPos & mask

	if nextWritePos == nextReadPos {
		return 0 // Full
	}

	// Calculate used slots
	if nextWritePos > nextReadPos {
		used := nextWritePos - nextReadPos
		return rb.size - 1 - used // -1 for reserved slot
	} else {
		used := (rb.size - nextReadPos) + nextWritePos
		return rb.size - 1 - used // -1 for reserved slot
	}
}
