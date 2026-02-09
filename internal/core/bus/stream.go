// If you are AI: This file implements the Stream type that manages publisher and subscribers.
// A stream allows exactly one publisher and multiple subscribers with efficient fanout.

package bus

import (
	"sync"
)

// Stream represents a live media stream instance.
// It manages one publisher and multiple subscribers with efficient message fanout.
// Init messages (AVC/AAC sequence headers) are cached and replayed to late-joining subscribers.
// Lock expectations: Uses mutex for publisher/subscriber management and init cache.
// Allocation: Pre-allocated subscriber map, no per-message allocations in fanout.
type Stream struct {
	key         StreamKey
	mu          sync.RWMutex
	publisher   *Publisher
	subscribers map[uint64]*Subscriber
	nextSubID   uint64
	initVideo   *MediaMessage // Cached AVC sequence header (cloned, long-lived)
	initAudio   *MediaMessage // Cached AAC sequence header (cloned, long-lived)
	initMeta    *MediaMessage // Cached metadata/onMetaData (cloned, long-lived)
}

// Publisher represents a stream publisher.
// Only one publisher can be attached to a stream at a time.
type Publisher struct {
	id uint64 // Unique publisher ID
}

// NewStream creates a new stream with the given key.
func NewStream(key StreamKey) *Stream {
	return &Stream{
		key:         key,
		subscribers: make(map[uint64]*Subscriber),
		nextSubID:   1,
	}
}

// Key returns the stream's key.
func (s *Stream) Key() StreamKey {
	return s.key
}

// AttachPublisher attaches a publisher to the stream.
// Returns true if attached, false if a publisher is already attached.
func (s *Stream) AttachPublisher(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.publisher != nil {
		return false
	}

	s.publisher = &Publisher{id: id}
	return true
}

// DetachPublisher detaches the current publisher from the stream.
// Also clears cached init messages since they belong to the publisher's session.
func (s *Stream) DetachPublisher() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publisher = nil
	s.initVideo = nil
	s.initAudio = nil
	s.initMeta = nil
}

// HasPublisher returns true if a publisher is currently attached.
func (s *Stream) HasPublisher() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publisher != nil
}

// AttachSubscriber attaches a new subscriber to the stream.
// Cached init messages (sequence headers) are replayed into the subscriber's buffer
// so late-joining clients receive codec configuration before live frames.
// Returns the subscriber and a unique subscriber ID.
func (s *Stream) AttachSubscriber(capacity uint32, strategy BackpressureStrategy) (*Subscriber, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextSubID
	s.nextSubID++

	sub := NewSubscriber(id, capacity, strategy)

	// Replay cached init messages so subscriber gets codec configuration first
	if s.initMeta != nil {
		sub.Buffer().Write(s.initMeta)
	}
	if s.initVideo != nil {
		sub.Buffer().Write(s.initVideo)
	}
	if s.initAudio != nil {
		sub.Buffer().Write(s.initAudio)
	}

	s.subscribers[id] = sub
	return sub, id
}

// DetachSubscriber detaches a subscriber from the stream.
func (s *Stream) DetachSubscriber(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, id)
}

// Publish delivers a message to all subscribers.
// Init messages (IsInit=true) are also cached so late-joining subscribers receive them.
// This is the hot path - must be allocation-free in steady state.
// Lock expectations: Read lock for fanout, write lock only for init message caching.
// Allocation: No allocations in steady state. Clone only for init messages (rare).
func (s *Stream) Publish(msg *MediaMessage) {
	if msg == nil {
		return
	}

	// Cache init messages (sequence headers) for late-joining subscribers.
	// NOTE: This allocates a clone, but only happens once per stream setup (not per frame).
	if msg.IsInit {
		s.cacheInitMessage(msg)
	}

	s.mu.RLock()
	subs := make([]*Subscriber, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.mu.RUnlock()

	// Fanout to all subscribers
	// NOTE: Each subscriber gets a reference to the same message.
	// Subscribers must not modify the message. Ownership remains with publisher
	// until all subscribers have processed it.
	for _, sub := range subs {
		// Write to subscriber's buffer (non-blocking)
		sub.Buffer().Write(msg)
	}
}

// cacheInitMessage stores a clone of an init message for late-joining subscribers.
// Only called for messages with IsInit=true (codec sequence headers).
func (s *Stream) cacheInitMessage(msg *MediaMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case MessageTypeVideo:
		s.initVideo = msg.Clone()
	case MessageTypeAudio:
		s.initAudio = msg.Clone()
	case MessageTypeMetadata:
		s.initMeta = msg.Clone()
	}
}

// SubscriberCount returns the number of active subscribers.
func (s *Stream) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// IsEmpty returns true if the stream has no publisher and no subscribers.
func (s *Stream) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publisher == nil && len(s.subscribers) == 0
}
