// If you are AI: This file contains unit tests for the ring buffer.

package bus

import (
	"testing"
)

func TestRingBufferWriteRead(t *testing.T) {
	rb := NewRingBuffer(8, BackpressureDropOldest)

	msg := AcquireMessage()
	msg.Type = MessageTypeVideo

	// Write message
	if !rb.Write(msg) {
		t.Error("Write should succeed on empty buffer")
	}

	// Read message
	read, ok := rb.Read()
	if !ok {
		t.Error("Read should succeed after write")
	}
	if read != msg {
		t.Error("Read should return same message")
	}

	// Buffer should be empty
	_, ok = rb.Read()
	if ok {
		t.Error("Read should fail on empty buffer")
	}
}

func TestRingBufferFull(t *testing.T) {
	rb := NewRingBuffer(4, BackpressureDropOldest)

	// Fill buffer
	for i := 0; i < 4; i++ {
		msg := AcquireMessage()
		msg.Type = MessageTypeVideo
		if !rb.Write(msg) {
			t.Errorf("Write %d should succeed", i)
		}
	}

	// Buffer should be full
	if rb.Available() != 0 {
		t.Errorf("Expected 0 available, got %d", rb.Available())
	}

	// Next write should drop oldest
	droppedBefore := rb.Dropped()
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	if !rb.Write(msg) {
		t.Error("Write should succeed (dropping oldest)")
	}

	if rb.Dropped() != droppedBefore+1 {
		t.Error("Dropped count should increase")
	}
}

func TestRingBufferDropNewest(t *testing.T) {
	rb := NewRingBuffer(4, BackpressureDropNewest)

	// Fill buffer
	for i := 0; i < 4; i++ {
		msg := AcquireMessage()
		msg.Type = MessageTypeVideo
		rb.Write(msg)
	}

	// Next write should drop newest (message is dropped, write returns false)
	droppedBefore := rb.Dropped()
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	// With drop newest, the write returns false (message is dropped)
	if rb.Write(msg) {
		t.Error("Write should return false with drop newest when buffer is full")
	}

	if rb.Dropped() != droppedBefore+1 {
		t.Error("Dropped count should increase")
	}
}

func TestRingBufferMultipleReads(t *testing.T) {
	rb := NewRingBuffer(8, BackpressureDropOldest)

	// Write multiple messages
	for i := 0; i < 5; i++ {
		msg := AcquireMessage()
		msg.Timestamp = uint32(i * 1000)
		rb.Write(msg)
	}

	// Read all messages
	for i := 0; i < 5; i++ {
		msg, ok := rb.Read()
		if !ok {
			t.Errorf("Read %d should succeed", i)
		}
		if msg.Timestamp != uint32(i*1000) {
			t.Errorf("Expected timestamp %d, got %d", i*1000, msg.Timestamp)
		}
	}

	// Buffer should be empty
	_, ok := rb.Read()
	if ok {
		t.Error("Read should fail on empty buffer")
	}
}
