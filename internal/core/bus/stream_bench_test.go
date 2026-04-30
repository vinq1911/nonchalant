// If you are AI: This file contains benchmarks for publish/fanout performance.
// Benchmarks must prove stable allocations and predictable throughput.

package bus

import (
	"fmt"
	"runtime"
	"sync"
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
		sub.Read()
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
			sub.Read()
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

// BenchmarkPublishHighFanout measures how the lock-free fanout scales with
// subscriber count. We don't drain the buffers — this isolates the publish
// hot path. With the atomic.Pointer snapshot, ns/op should grow roughly
// linearly with fan-out; the test reports allocs/op to flag any regressions
// that introduce per-publish allocation.
func BenchmarkPublishHighFanout(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			stream := NewStream(NewStreamKey("live", "bench"))
			stream.AttachPublisher(1)
			for i := 0; i < n; i++ {
				stream.AttachSubscriber(1024, BackpressureDropOldest)
			}
			msg := AcquireMessage()
			msg.Type = MessageTypeVideo
			msg.SetPayload(make([]byte, 1024))
			defer ReleaseMessage(msg)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				stream.Publish(msg)
			}
		})
	}
}

// BenchmarkPublisherFrame simulates an end-to-end RTMP publisher path:
// acquire a payload buffer (arena), copy in raw payload bytes, build a
// MediaMessage, and Publish to the stream. This is the realistic per-frame
// cost the RTMP ingest goroutine pays at runtime. With the per-stream
// arena, allocs/op should drop to zero (modulo the message struct).
func BenchmarkPublisherFrame(b *testing.B) {
	for _, payloadSize := range []int{1024, 4096, 16384} {
		b.Run(fmt.Sprintf("payload=%d", payloadSize), func(b *testing.B) {
			stream := NewStream(NewStreamKey("live", "bench"))
			stream.AttachPublisher(1)
			// One subscriber so the log doesn't optimise itself out.
			stream.AttachSubscriber(1024, BackpressureDropOldest)
			rawIn := make([]byte, payloadSize)

			b.SetBytes(int64(payloadSize))
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				msg := stream.AcquireMessage()
				msg.Type = MessageTypeVideo
				msg.Timestamp = uint32(i)
				buf := stream.AcquirePayload(len(rawIn))
				msg.Payload = append(buf, rawIn...)
				stream.Publish(msg)
			}
		})
	}
}

// BenchmarkPublisherFrameLegacy measures the previous publisher path that
// goes through the global bus.AcquirePayload sync.Pool — kept for
// comparison so regressions are obvious. The pool is empty in steady state
// (nothing Releases) so every Acquire allocates a 64 KB buffer.
func BenchmarkPublisherFrameLegacy(b *testing.B) {
	stream := NewStream(NewStreamKey("live", "bench"))
	stream.AttachPublisher(1)
	stream.AttachSubscriber(1024, BackpressureDropOldest)
	rawIn := make([]byte, 4096)

	b.SetBytes(int64(len(rawIn)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg := AcquireMessage()
		msg.Type = MessageTypeVideo
		msg.Timestamp = uint32(i)
		buf := AcquirePayload() // pool path — no Release ⇒ allocates per frame
		msg.Payload = append(buf, rawIn...)
		stream.Publish(msg)
	}
}

// BenchmarkAttachDetachChurn measures the cost of subscribers joining and
// leaving a stream. This is the path the snapshot rebuild fires on; if it
// regresses, every viewer connect / disconnect pays for it.
func BenchmarkAttachDetachChurn(b *testing.B) {
	stream := NewStream(NewStreamKey("live", "bench"))
	stream.AttachPublisher(1)
	// Hold a steady population so the snapshot rebuild has work to copy.
	const steady = 50
	steadyIDs := make([]uint64, steady)
	for i := 0; i < steady; i++ {
		_, id := stream.AttachSubscriber(64, BackpressureDropOldest)
		steadyIDs[i] = id
	}
	defer func() {
		for _, id := range steadyIDs {
			stream.DetachSubscriber(id)
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, id := stream.AttachSubscriber(64, BackpressureDropOldest)
		stream.DetachSubscriber(id)
	}
}

// BenchmarkRingBufferSaturated forces the ring buffer to overflow on every
// write so the backpressure path is taken every iteration. Measures the
// dropped-message accounting cost — the slow-client steady state.
func BenchmarkRingBufferSaturated(b *testing.B) {
	rb := NewRingBuffer(8, BackpressureDropOldest)
	msg := AcquireMessage()
	msg.Type = MessageTypeVideo
	msg.SetPayload(make([]byte, 64))
	defer ReleaseMessage(msg)

	// Fill the buffer once so subsequent writes always trigger overflow.
	for i := 0; i < 8; i++ {
		rb.Write(msg)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rb.Write(msg) // every call overflows + drops oldest
	}
}

// BenchmarkRingBufferReadEmpty measures the empty-buffer Read path — the
// hot loop that subscribers run when their publisher stalls.
func BenchmarkRingBufferReadEmpty(b *testing.B) {
	rb := NewRingBuffer(8, BackpressureDropOldest)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rb.Read()
	}
}

// BenchmarkScaleSubscribers sweeps subscriber count (1 publisher) up the
// power-of-2 ladder to find the fanout ceiling. Uses a small payload arena
// per stream so memory stays bounded.
func BenchmarkScaleSubscribers(b *testing.B) {
	const payload = 256
	for n := 0; n <= 18; n++ {
		subs := 1 << n
		b.Run(fmt.Sprintf("subs=%d", subs), func(b *testing.B) {
			runScaleSubs(b, subs, payload)
		})
	}
}

// runScaleSubs is one cell of the BenchmarkScaleSubscribers grid.
// We do NOT spawn drain goroutines — with the shared-log architecture, subscribers
// don't affect publish cost (publishing is O(1) regardless of subscriber count;
// they fall behind harmlessly when nobody drains them). Skipping the drain
// loop avoids polluting the measurement with 10K goroutines worth of scheduler
// overhead, isolating the publish hot path.
func runScaleSubs(b *testing.B, subs, payload int) {
	stream := NewStreamWithCapacity(NewStreamKey("live", "scale"), 1024, 256, 4096)
	stream.AttachPublisher(1)
	for i := 0; i < subs; i++ {
		stream.AttachSubscriber(0, BackpressureDropOldest)
	}

	rawIn := make([]byte, payload)
	b.SetBytes(int64(payload))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg := stream.AcquireMessage()
		msg.Type = MessageTypeVideo
		msg.Timestamp = uint32(i)
		buf := stream.AcquirePayload(len(rawIn))
		msg.Payload = append(buf, rawIn...)
		stream.Publish(msg)
	}
}

// BenchmarkScalePublishers sweeps publisher count up the power-of-2 ladder,
// each publisher feeding 16 subscribers on its own stream. Each pub runs on
// its own goroutine so the scheduler is part of what's measured. Pushed up
// to 2^19 = 524288 publishers to find the throughput / scheduler ceiling.
func BenchmarkScalePublishers(b *testing.B) {
	const subsPerPub = 16
	const payload = 256
	for n := 0; n <= 19; n++ {
		pubs := 1 << n
		b.Run(fmt.Sprintf("pubs=%d", pubs), func(b *testing.B) {
			runScalePubs(b, pubs, subsPerPub, payload)
		})
	}
}

// runScalePubs is one cell of the BenchmarkScalePublishers grid.
// Each publisher is a separate stream + goroutine; subscribers are attached
// (registered) but not drained. With the shared-log architecture this is
// representative — publish throughput doesn't depend on whether subscribers
// keep up, so leaving them stalled is fine for the measurement.
//
// Streams use tiny arena slabs (16 slots × 1 KB = 16 KB each) so memory
// stays bounded as we push publisher count toward 16 K. Each pub still
// runs in its own goroutine, so the scheduler IS part of what's measured.
func runScalePubs(b *testing.B, publishers, subsPerPub, payload int) {
	streams := make([]*Stream, publishers)
	for i := 0; i < publishers; i++ {
		streams[i] = NewStreamWithCapacity(
			NewStreamKey("live", fmt.Sprintf("p%d", i)), 64, 16, payload)
		streams[i].AttachPublisher(uint64(i + 1))
		for j := 0; j < subsPerPub; j++ {
			streams[i].AttachSubscriber(0, BackpressureDropOldest)
		}
	}

	per := (b.N + publishers - 1) / publishers
	b.SetBytes(int64(payload))
	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(stream *Stream) {
			defer wg.Done()
			rawIn := make([]byte, payload)
			for k := 0; k < per; k++ {
				msg := stream.AcquireMessage()
				msg.Type = MessageTypeVideo
				msg.Timestamp = uint32(k)
				buf := stream.AcquirePayload(payload)
				msg.Payload = append(buf, rawIn...)
				stream.Publish(msg)
			}
		}(streams[i])
	}
	wg.Wait()
}

// BenchmarkConcurrent simulates a real workload: P publishers each writing
// to their own stream while N consumers per stream drain the buffers in
// parallel goroutines. ns/op is the wall-clock time per publish under
// contention; bytes/op (via SetBytes) gives effective throughput.
//
// The matrix covers four shapes:
//   - 1×1, 1×64, 1×512: single popular stream, growing fan-out.
//   - 4×16, 16×16: multi-stream with moderate fan-out (busy server).
//   - 64×4: many small streams (relay / origin scenario).
//
// Allocations should remain at zero per op in steady state; the message
// payload buffer is reused per-publisher.
func BenchmarkConcurrent(b *testing.B) {
	const payload = 1024
	cases := []struct{ pub, sub int }{
		{1, 1}, {1, 64}, {1, 512},
		{4, 16}, {16, 16},
		{64, 4},
	}
	for _, c := range cases {
		name := fmt.Sprintf("pubs=%d/subs=%d", c.pub, c.sub)
		b.Run(name, func(b *testing.B) {
			runConcurrent(b, c.pub, c.sub, payload)
		})
	}
}

// runConcurrent is the body of one BenchmarkConcurrent sub-test.
// Extracted so the inner closure stays small.
func runConcurrent(b *testing.B, publishers, consumersPerStream, payload int) {
	streams, allSubs := setupConcurrent(publishers, consumersPerStream)
	stopCh := make(chan struct{})
	drainWG := startDrainers(stopCh, allSubs)

	// Spread b.N publishes evenly across publisher goroutines.
	per := (b.N + publishers - 1) / publishers

	b.SetBytes(int64(payload))
	b.ResetTimer()
	b.ReportAllocs()

	var pubWG sync.WaitGroup
	for i := 0; i < publishers; i++ {
		pubWG.Add(1)
		go func(stream *Stream) {
			defer pubWG.Done()
			msg := AcquireMessage()
			msg.Type = MessageTypeVideo
			msg.SetPayload(make([]byte, payload))
			defer ReleaseMessage(msg)
			for k := 0; k < per; k++ {
				stream.Publish(msg)
			}
		}(streams[i])
	}
	pubWG.Wait()

	b.StopTimer()
	close(stopCh)
	drainWG.Wait()
}

// setupConcurrent allocates P streams, each with C subscribers attached,
// and returns the streams plus the flattened subscriber list for draining.
func setupConcurrent(publishers, consumersPerStream int) ([]*Stream, []*Subscriber) {
	streams := make([]*Stream, publishers)
	allSubs := make([]*Subscriber, 0, publishers*consumersPerStream)
	for i := 0; i < publishers; i++ {
		streams[i] = NewStream(NewStreamKey("live", fmt.Sprintf("p%d", i)))
		streams[i].AttachPublisher(uint64(i + 1))
		for j := 0; j < consumersPerStream; j++ {
			sub, _ := streams[i].AttachSubscriber(1024, BackpressureDropOldest)
			allSubs = append(allSubs, sub)
		}
	}
	return streams, allSubs
}

// startDrainers spawns one goroutine per subscriber that reads its buffer
// in a tight loop until stopCh is closed. Yields when the buffer is empty so
// the publisher goroutines can make progress.
func startDrainers(stopCh <-chan struct{}, subs []*Subscriber) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(s *Subscriber) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
				}
				if _, ok := s.Read(); !ok {
					runtime.Gosched()
				}
			}
		}(sub)
	}
	return &wg
}
