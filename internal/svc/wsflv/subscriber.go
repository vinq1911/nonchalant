// If you are AI: This file implements WebSocket-FLV subscriber that reads from bus and writes FLV.
// Subscriber manages the WebSocket connection lifecycle and message processing.

package wsflv

import (
	"nonchalant/internal/core/bus"
	"nonchalant/internal/core/protocol/flv"
)

// Subscriber represents a WebSocket-FLV client subscriber.
// Reads messages from bus and writes FLV tags to WebSocket connection.
type Subscriber struct {
	conn          WebSocketConn
	busSubscriber *bus.Subscriber
	stream        *bus.Stream
	subscriberID  uint64
	headerWritten bool
}

// WebSocketConn defines the interface for WebSocket operations.
// This allows for easier testing and abstraction.
type WebSocketConn interface {
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// NewSubscriber creates a new WebSocket-FLV subscriber.
func NewSubscriber(conn WebSocketConn, stream *bus.Stream) *Subscriber {
	return &Subscriber{
		conn:   conn,
		stream: stream,
	}
}

// WriteHeader writes the FLV file header as the first WebSocket frame.
// Must be called before writing any tags.
func (s *Subscriber) WriteHeader(hasAudio, hasVideo bool) error {
	if s.headerWritten {
		return nil
	}

	header := flv.NewHeader(hasAudio, hasVideo)
	headerBytes := header.Bytes()

	// Write previous tag size (0 for first tag)
	prevSize := make([]byte, 4)

	// Combine header and previous tag size into single frame
	frame := make([]byte, len(headerBytes)+len(prevSize))
	copy(frame, headerBytes)
	copy(frame[len(headerBytes):], prevSize)

	// Write as binary WebSocket frame
	if err := s.conn.WriteMessage(2, frame); err != nil {
		return err
	}

	s.headerWritten = true
	return nil
}

// ProcessMessages processes messages from the subscriber buffer and writes FLV tags.
// This runs in a loop until the connection is closed or an error occurs.
// Allocation: Tag creation allocates header, but payloads are reused from bus.
// NOTE: This blocks until client disconnects. WebSocket connection close detection
// happens at the write level.
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

		// Write tag as binary WebSocket frame
		// NOTE: Each complete FLV tag is sent as a single frame
		tagBytes := tag.Bytes()
		if err := s.conn.WriteMessage(2, tagBytes); err != nil {
			// Client disconnected or write error
			return err
		}
	}
}

// Attach attaches the subscriber to the stream.
// Returns the subscriber ID for later detach.
// Backpressure strategy: DropOldest - same as HTTP-FLV to ensure consistency.
// Slow WebSocket clients drop oldest frames to prevent blocking publisher.
func (s *Subscriber) Attach() uint64 {
	// Attach with bounded buffer and drop oldest strategy
	// This ensures publisher never blocks on slow WebSocket clients
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
