// If you are AI: This file implements HTTP-FLV subscriber that reads from bus and writes FLV.
// Subscriber manages the connection lifecycle and message processing.

package httpflv

import (
	"bufio"
	"io"
	"nonchalant/internal/core/bus"
	"nonchalant/internal/core/protocol/flv"
)

// Subscriber represents an HTTP-FLV client subscriber.
// Reads messages from bus and writes FLV tags to HTTP response.
type Subscriber struct {
	writer        *bufio.Writer
	busSubscriber *bus.Subscriber
	stream        *bus.Stream
	subscriberID  uint64
	headerWritten bool
}

// NewSubscriber creates a new HTTP-FLV subscriber.
func NewSubscriber(w io.Writer, stream *bus.Stream) *Subscriber {
	return &Subscriber{
		writer: bufio.NewWriter(w),
		stream: stream,
	}
}

// WriteHeader writes the FLV file header.
// Must be called before writing any tags.
func (s *Subscriber) WriteHeader(hasAudio, hasVideo bool) error {
	if s.headerWritten {
		return nil
	}

	header := flv.NewHeader(hasAudio, hasVideo)
	if _, err := s.writer.Write(header.Bytes()); err != nil {
		return err
	}

	// Write previous tag size (0 for first tag)
	prevSize := make([]byte, 4)
	if _, err := s.writer.Write(prevSize); err != nil {
		return err
	}

	if err := s.writer.Flush(); err != nil {
		return err
	}

	s.headerWritten = true
	return nil
}

// ProcessMessages processes messages from the subscriber buffer and writes FLV tags.
// This runs in a loop until the connection is closed or an error occurs.
// Allocation: Tag creation allocates header, but payloads are reused from bus.
// NOTE: This blocks until client disconnects. HTTP connection close detection
// happens at the write/flush level.
func (s *Subscriber) ProcessMessages() error {
	if s.busSubscriber == nil {
		return nil
	}

	for {
		// Read message from subscriber buffer
		msg, ok := s.busSubscriber.Buffer().Read()
		if !ok {
			// Buffer empty, continue waiting
			// NOTE: In a production system, we might want to add a timeout
			// or context cancellation here
			continue
		}

		// Convert to FLV tag
		tag := flv.MuxMessage(msg)
		if tag == nil {
			// Unsupported message type, skip
			continue
		}

		// Write tag
		tagBytes := tag.Bytes()
		if _, err := s.writer.Write(tagBytes); err != nil {
			// Client disconnected or write error
			return err
		}

		// Flush to detect disconnects early
		if err := s.writer.Flush(); err != nil {
			// Client disconnected
			return err
		}
	}
}

// Attach attaches the subscriber to the stream.
// Returns the subscriber ID for later detach.
// Backpressure strategy: DropOldest - slow clients drop oldest frames to prevent blocking publisher.
func (s *Subscriber) Attach() uint64 {
	// Attach with bounded buffer and drop oldest strategy
	// This ensures publisher never blocks on slow HTTP clients
	busSub, id := s.stream.AttachSubscriber(1000, bus.BackpressureDropOldest)
	s.busSubscriber = busSub
	s.subscriberID = id
	return id
}

// Detach detaches the subscriber from the stream.
func (s *Subscriber) Detach() {
	if s.stream != nil && s.subscriberID != 0 {
		s.stream.DetachSubscriber(s.subscriberID)
		s.subscriberID = 0
		s.busSubscriber = nil
	}
}

// Buffer returns the subscriber's buffer for direct access.
func (s *Subscriber) Buffer() *bus.RingBuffer {
	if s.busSubscriber == nil {
		return nil
	}
	return s.busSubscriber.Buffer()
}
