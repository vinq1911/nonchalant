// If you are AI: This file contains benchmarks for publish/fanout performance.
// Benchmarks must prove stable allocations and predictable throughput.

package bus

import (
	"testing"
)

// BenchmarkPublishSingleSubscriber benchmarks publish to a single subscriber.
// This measures the hot path for single consumer scenarios.
func BenchmarkPublishSingleSubscriber(b *testing.B) {
	key := NewStreamKey("live", "bench")
	stream := NewStream(key)
	stream.AttachPublisher(1)

	sub, _ := stream.AttachSubscriber(1000, BackpressureDropOldest)

	// Pre-allocate message
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	msg.Timestamp = 1000
	msg.SetPayload(make([]byte, 1024))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg.Timestamp = uint32(i * 1000)
		stream.Publish(msg)
		// Read to keep buffer from filling
		sub.Buffer().Read()
	}

	ReleaseMessage(msg)
}

// BenchmarkPublishMultipleSubscribers benchmarks publish to multiple subscribers.
// This measures fanout performance with concurrent consumers.
func BenchmarkPublishMultipleSubscribers(b *testing.B) {
	key := NewStreamKey("live", "bench")
	stream := NewStream(key)
	stream.AttachPublisher(1)

	// Create 10 subscribers
	subs := make([]*Subscriber, 10)
	for i := 0; i < 10; i++ {
		sub, _ := stream.AttachSubscriber(1000, BackpressureDropOldest)
		subs[i] = sub
	}

	// Pre-allocate message
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	msg.Timestamp = 1000
	msg.SetPayload(make([]byte, 1024))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg.Timestamp = uint32(i * 1000)
		stream.Publish(msg)
		// Read from all subscribers to keep buffers from filling
		for _, sub := range subs {
			sub.Buffer().Read()
		}
	}

	ReleaseMessage(msg)
}

// BenchmarkPublishFanoutOnly benchmarks the fanout operation without reading.
// This isolates the publish/fanout overhead.
func BenchmarkPublishFanoutOnly(b *testing.B) {
	key := NewStreamKey("live", "bench")
	stream := NewStream(key)
	stream.AttachPublisher(1)

	// Create 10 subscribers with large buffers
	for i := 0; i < 10; i++ {
		stream.AttachSubscriber(10000, BackpressureDropOldest)
	}

	// Pre-allocate message
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	msg.Timestamp = 1000
	msg.SetPayload(make([]byte, 1024))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg.Timestamp = uint32(i * 1000)
		stream.Publish(msg)
	}

	ReleaseMessage(msg)
}

// BenchmarkMessagePool benchmarks message acquisition and release.
// This verifies the pool eliminates allocations in steady state.
func BenchmarkMessagePool(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg := AcquireMessage()
		msg.Type = MessageTypeVideo
		msg.Timestamp = uint32(i)
		ReleaseMessage(msg)
	}
}

// BenchmarkPayloadPool benchmarks payload buffer acquisition and release.
// This verifies the payload pool eliminates allocations.
func BenchmarkPayloadPool(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := AcquirePayload()
		buf = append(buf, make([]byte, 1024)...)
		ReleasePayload(buf)
	}
}
