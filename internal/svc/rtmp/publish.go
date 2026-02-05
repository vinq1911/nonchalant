// If you are AI: This file handles RTMP publish lifecycle and integration with the bus.
// Manages publisher attachment and media message publishing.

package rtmp

import (
	"nonchalant/internal/core/bus"
	rtmpprotocol "nonchalant/internal/core/protocol/rtmp"
)

// Publisher manages publishing media messages to a stream.
// Integrates RTMP session with the core bus.
type Publisher struct {
	session     *rtmpprotocol.Session
	stream      *bus.Stream
	streamKey   bus.StreamKey
	publisherID uint64
}

// NewPublisher creates a new publisher for a stream.
func NewPublisher(session *rtmpprotocol.Session, stream *bus.Stream, publisherID uint64) *Publisher {
	return &Publisher{
		session:     session,
		stream:      stream,
		streamKey:   stream.Key(),
		publisherID: publisherID,
	}
}

// PublishAudio publishes an audio message to the stream.
// Uses pooled message and payload from the bus.
func (p *Publisher) PublishAudio(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeAudio
	msg.Timestamp = timestamp

	// Acquire payload buffer from pool
	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	p.stream.Publish(msg)

	// NOTE: Message ownership transfers to stream/subscribers
	// Publisher should not release the message here
}

// PublishVideo publishes a video message to the stream.
// Uses pooled message and payload from the bus.
func (p *Publisher) PublishVideo(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeVideo
	msg.Timestamp = timestamp

	// Acquire payload buffer from pool
	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	p.stream.Publish(msg)
}

// PublishMetadata publishes a metadata message to the stream.
func (p *Publisher) PublishMetadata(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeMetadata
	msg.Timestamp = timestamp

	// Acquire payload buffer from pool
	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	p.stream.Publish(msg)
}

// Detach detaches the publisher from the stream.
func (p *Publisher) Detach() {
	if p.stream != nil {
		p.stream.DetachPublisher()
	}
}

// StreamKey returns the stream key for this publisher.
func (p *Publisher) StreamKey() bus.StreamKey {
	return p.streamKey
}
