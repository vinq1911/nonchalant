// If you are AI: This file handles RTMP publish lifecycle and integration with the bus.
// Manages publisher attachment, media message publishing, and sequence header detection.

package rtmp

import (
	"log"
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
// Detects AAC sequence headers and marks them as init data for late-joining subscribers.
func (p *Publisher) PublishAudio(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeAudio
	msg.Timestamp = timestamp
	msg.IsInit = isAACSequenceHeader(payload)

	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	if msg.IsInit {
		log.Printf("Cached AAC sequence header (%d bytes)", len(payload))
	}

	p.stream.Publish(msg)
}

// PublishVideo publishes a video message to the stream.
// Detects AVC sequence headers and marks them as init data for late-joining subscribers.
func (p *Publisher) PublishVideo(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeVideo
	msg.Timestamp = timestamp
	msg.IsInit = isAVCSequenceHeader(payload)

	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	if msg.IsInit {
		log.Printf("Cached AVC sequence header (%d bytes)", len(payload))
	}

	p.stream.Publish(msg)
}

// PublishMetadata publishes a metadata message to the stream.
// Metadata (@setDataFrame / onMetaData) is always treated as init data.
func (p *Publisher) PublishMetadata(timestamp uint32, payload []byte) {
	msg := bus.AcquireMessage()
	msg.Type = bus.MessageTypeMetadata
	msg.Timestamp = timestamp
	msg.IsInit = true // Metadata is always init data

	buf := bus.AcquirePayload()
	msg.Payload = append(buf, payload...)

	p.stream.Publish(msg)
}

// isAVCSequenceHeader detects an AVC (H.264) decoder configuration record.
// In RTMP/FLV video format: byte[0] lower nibble = codec ID (7=AVC), byte[1] = packet type (0=seq header).
func isAVCSequenceHeader(payload []byte) bool {
	return len(payload) >= 2 && (payload[0]&0x0F) == 7 && payload[1] == 0
}

// isAACSequenceHeader detects an AAC AudioSpecificConfig.
// In RTMP/FLV audio format: byte[0] upper nibble = sound format (10=AAC), byte[1] = packet type (0=seq header).
func isAACSequenceHeader(payload []byte) bool {
	return len(payload) >= 2 && (payload[0]>>4) == 10 && payload[1] == 0
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
