// If you are AI: This file implements HTTP-FLV subscriber that reads from bus and writes FLV.
// Subscriber manages the connection lifecycle and message processing.

package httpflv

import (
	"bufio"
	"io"
	"runtime"

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
	gotKeyframe   bool   // True after first video keyframe received
	tsOffset      uint32 // First non-init timestamp, subtracted from all subsequent
	tsBaseSet     bool   // True after tsOffset is captured
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
// ALL non-init frames are dropped until the first video keyframe arrives, so that
// audio and video start simultaneously and the decoder can initialize properly.
// Timestamps are rebased so the subscriber's stream starts at ts=0, preventing
// players from buffering a multi-second gap between init (ts=0) and live data.
// Allocation: Tag creation allocates header, but payloads are reused from bus.
// NOTE: This blocks until client disconnects. HTTP connection close detection
// happens at the write/flush level.
func (s *Subscriber) ProcessMessages() error {
	if s.busSubscriber == nil {
		return nil
	}

	for {
		msg, ok := s.busSubscriber.Buffer().Read()
		if !ok {
			// Buffer empty — yield to avoid busy-wait CPU burn
			runtime.Gosched()
			continue
		}

		// Keyframe gating: drop ALL non-init frames until first video keyframe.
		// This prevents audio from piling up before video, which causes player
		// buffer deadlocks. Init messages (codec config) always pass through.
		if !s.gotKeyframe && !msg.IsInit {
			if msg.Type == bus.MessageTypeVideo && flv.IsVideoKeyframe(msg.Payload) {
				s.gotKeyframe = true
			} else {
				continue // Drop all non-init frames before first keyframe
			}
		}

		// Convert to FLV tag
		tag := flv.MuxMessage(msg)
		if tag == nil {
			continue
		}

		// Rebase timestamp so stream starts at ts=0 for this subscriber
		tag.Timestamp = s.rebaseTimestamp(msg)

		// Write tag and flush to detect disconnects early
		if _, err := s.writer.Write(tag.Bytes()); err != nil {
			return err
		}
		if err := s.writer.Flush(); err != nil {
			return err
		}
	}
}

// rebaseTimestamp adjusts a message timestamp so the subscriber's stream starts at ts=0.
// Init messages always return ts=0. The first non-init timestamp becomes the offset
// that is subtracted from all subsequent timestamps.
func (s *Subscriber) rebaseTimestamp(msg *bus.MediaMessage) uint32 {
	if msg.IsInit {
		return 0
	}
	if !s.tsBaseSet {
		s.tsOffset = msg.Timestamp
		s.tsBaseSet = true
	}
	if msg.Timestamp < s.tsOffset {
		return 0 // Guard against underflow
	}
	return msg.Timestamp - s.tsOffset
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
