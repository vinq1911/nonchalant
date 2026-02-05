// If you are AI: This file defines the Subscriber interface and implementation.
// Subscribers receive messages from streams via a ring buffer.

package bus

// Subscriber represents a consumer of media messages from a stream.
// Each subscriber has its own ring buffer to avoid blocking the publisher.
type Subscriber struct {
	id        uint64              // Unique subscriber ID
	buffer    *RingBuffer         // Bounded buffer for message delivery
	onMessage func(*MediaMessage) // Callback for message delivery (optional)
}

// NewSubscriber creates a new subscriber with the specified buffer capacity and strategy.
func NewSubscriber(id uint64, capacity uint32, strategy BackpressureStrategy) *Subscriber {
	return &Subscriber{
		id:     id,
		buffer: NewRingBuffer(capacity, strategy),
	}
}

// ID returns the unique subscriber identifier.
func (s *Subscriber) ID() uint64 {
	return s.id
}

// Buffer returns the subscriber's ring buffer.
// This is used by the stream to deliver messages.
func (s *Subscriber) Buffer() *RingBuffer {
	return s.buffer
}

// SetMessageHandler sets a callback function to be called when messages are available.
// If set, the subscriber will call this function for each message read from the buffer.
func (s *Subscriber) SetMessageHandler(handler func(*MediaMessage)) {
	s.onMessage = handler
}

// Process reads and processes messages from the buffer.
// This should be called periodically by the subscriber's goroutine.
// Returns the number of messages processed.
func (s *Subscriber) Process(maxMessages int) int {
	processed := 0
	for i := 0; i < maxMessages; i++ {
		msg, ok := s.buffer.Read()
		if !ok {
			break
		}

		if s.onMessage != nil {
			s.onMessage(msg)
		}

		// NOTE: Message ownership transfers to handler.
		// Handler is responsible for releasing the message if needed.
		processed++
	}
	return processed
}

// Dropped returns the number of messages dropped due to backpressure.
func (s *Subscriber) Dropped() uint64 {
	return s.buffer.Dropped()
}
