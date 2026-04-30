# Plan — WebRTC playback (WHEP) for nonchalant

This is a session-bootable plan. Read it cold; everything you need to start
implementing is here. There is intentionally no surrounding conversation — if
something isn't covered, prefer the conservative choice and ask the user.

## Why

Today's lowest-latency output is WebSocket-FLV (~2–5 s end-to-end). HLS / DASH
is 2–30 s. WebRTC is 50–500 ms. That's the difference between "watch a stream"
and "interact with one" — gaming, real-time betting, auctions, conferencing,
chat-and-react.

We are building **WHEP** (WebRTC-HTTP Egress Protocol, IETF draft, widely
deployed): viewers POST an SDP offer, get back an answer, watch. HTTP signalling.
No custom WebSocket signalling, no proprietary client. WHIP (ingest) is **out of
scope** for this session — RTMP ingest is fine for now.

## What we have to plug into

- `internal/core/bus/` — fan-out registry. Add a subscriber to a `*bus.Stream`
  and you receive `*MediaMessage` (audio / video / metadata).
- AVC sequence header (SPS/PPS) is already cached on the `*bus.Stream` and
  replayed to every new subscriber via `Stream.AttachSubscriber`. Use it.
- AAC sequence header is similarly cached. We will transcode AAC → Opus, so
  this matters only if we choose to keep AAC pass-through (we won't, see below).
- Existing services live in `internal/svc/<name>/` and follow this layout:
  `service.go` (constructor + `RegisterRoutes`), `handler.go` (HTTP),
  `subscriber.go` (bus → output adapter). Stay consistent.
- Hard rules enforced by `make check`:
  - Every `.go` file ≤ 300 lines.
  - Every `.go` file has an `// If you are AI:` header comment.
  - Every exported function has a doc comment.
- `golangci-lint` v2 config exists in `.golangci.yml`. Run `make lint`.

## Library

[`github.com/pion/webrtc/v4`](https://github.com/pion/webrtc) — battle-tested,
pure Go, no cgo. Used by Twitch, Discord. Add via `go get`.

For the H.264 RTP packetiser we want
`github.com/pion/rtp/codecs.H264Packet` (already a transitive dep of webrtc/v4).
For Opus transcoding we'll either:

- **(Recommended)** Use `github.com/hraban/opus` (cgo, libopus). Bulletproof
  browser compat. ~50 ms encoder latency at 20 ms frames. **Pick this.**
- Pure-Go Opus encoders exist (`gopus`, `pure-go-opus`) but quality + maturity
  are inferior. Avoid for v1.

Add `-tags webrtc` build tag if cgo opus is undesirable in the default build,
mirroring the existing `-tags ffmpeg` pattern. Optional — discuss with user.

## Architecture

```
RTMP publisher ─▶ rtmp ingest ─▶ bus.Stream ─▶ webrtc subscriber goroutine
                                                  │
                                                  ├─ video: AVC tag → NALUs → RTP H.264 packets
                                                  └─ audio: AAC tag → PCM → Opus → RTP packets
                                                  │
                                                  ▼
                                          pion PeerConnection
                                                  │
                                                  ▼
                                       DTLS / SRTP over UDP / ICE
                                                  │
                                                  ▼
                                              browser
```

One PeerConnection per viewer. Subscribers attach to the bus exactly like
HTTP-FLV does today. Decode → repackage → `track.WriteSample()` (Pion does the
RTP packetisation for us when you write samples; we only need to deliver
correctly-framed AnnexB NALUs and Opus frames).

## Files to create

```
internal/svc/whep/
├── service.go          # constructor + RegisterRoutes (≤ 80 lines)
├── handler.go          # POST /whep/{app}/{name}, DELETE /whep/{app}/{name}/{id}
├── session.go          # one PeerConnection + subscriber goroutine per viewer
├── h264.go             # AVCC → AnnexB NALU extraction
├── opus.go             # AAC → PCM → Opus encoder (cgo, build-tagged)
└── opus_stub.go        # stub when -tags webrtc is OFF (returns 503)
```

All under 300 lines each. The 300-line cap will force you to split `session.go`
into `session.go` + `subscriber.go` once it grows.

## Wiring

In `internal/server/server.go`:

```go
whepSvc := whep.NewService(registry, cfg.WebRTC, playKeys)
whepSvc.RegisterRoutes(mux)
```

Register **before** the httpflv catch-all (matches the existing pattern for
`/api`, `/metrics`, `/ws`, `/hls`, `/dash`).

## Config

Add to `internal/config/config.go`:

```go
type WebRTCConfig struct {
    // External UDP port range Pion's ICE agent will use. If empty, Pion
    // picks an ephemeral port per session.
    PortMin uint16 `yaml:"port_min,omitempty"`
    PortMax uint16 `yaml:"port_max,omitempty"`

    // Public IP advertised in ICE host candidates (1:1 NAT / cloud VMs).
    // Empty = auto-detect interface addresses.
    PublicIP string `yaml:"public_ip,omitempty"`

    // STUN servers to gather server-reflexive candidates.
    // Defaults to ["stun:stun.l.google.com:19302"] if nil.
    STUNServers []string `yaml:"stun_servers,omitempty"`
}
```

Update `Config` to include `WebRTC WebRTCConfig`. Update the doc generator
(`scripts/gen-docs/main.go`) to mention the new fields.

`auth.play_keys` already gates everything via the existing `auth.Gate` helper —
WHEP plugs into it the same way HLS/DASH do.

## The signalling exchange

WHEP is just one HTTP exchange:

1. Client `POST /whep/{app}/{name}` with body = SDP offer
   - `Content-Type: application/sdp`
2. Server creates a Pion `PeerConnection`, applies the offer, generates an
   answer **with all ICE candidates inlined** (skip trickle ICE for v1).
3. Server replies `201 Created`:
   - `Content-Type: application/sdp`
   - `Location: /whep/{app}/{name}/{session-id}`  (used for cancel)
   - body = SDP answer
4. To stop, client `DELETE /whep/{app}/{name}/{session-id}`.

This is the entire protocol. No WebSocket, no Socket.IO, no Janus.

Use Pion's `pc.SetLocalDescription` then `<-webrtc.GatheringCompletePromise(pc)`
to wait for ICE gathering before responding (so candidates are inlined).

## Codec strategy

Browsers reliably support:
- Video: H.264 (constrained baseline / main), VP8, VP9, AV1
- Audio: Opus, G.711

RTMP gives us H.264 + AAC. So:

- **Video: pass-through H.264.** No transcode. Convert AVCC to AnnexB and
  packetise. Cheap.
- **Audio: AAC → Opus.** Decode AAC with `internal/ffx` (already cgo wrapper
  around libavcodec), resample to 48 kHz mono/stereo, encode Opus. Pion has
  no built-in transcoder.

If `-tags webrtc` is off, return 503 from `/whep/*`. Same pattern as the
HLS/DASH endpoints when `ffmpeg` is missing.

## Subscriber goroutine

Pseudo-code:

```go
func (s *session) pump(ctx context.Context) {
    for {
        msg, ok := s.bus.Buffer().Read()
        if !ok {
            select {
            case <-ctx.Done(): return
            case <-time.After(20 * time.Millisecond): continue
            }
        }
        switch msg.Type {
        case bus.MessageTypeVideo:
            s.writeVideo(msg)        // AVCC → AnnexB → track.WriteSample
        case bus.MessageTypeAudio:
            s.writeAudio(msg)        // AAC → PCM → Opus → track.WriteSample
        case bus.MessageTypeMetadata:
            // ignore for WebRTC
        }
    }
}
```

Pion's `webrtc.TrackLocalStaticSample` accepts `media.Sample{Data, Duration}`
and handles RTP packetisation, including FU-A fragmentation for H.264.
**This is the magic that lets you skip writing a real RTP stack.** Use it.

## Keyframe-on-join

A new viewer can't decode until they receive an IDR. Two options:

1. Cache the most recent IDR on the bus (extend `Stream` with `lastVideoKey`,
   replay on attach like the AVC sequence header). Adds memory cost (~1 keyframe
   per stream).
2. Drop everything until the next IDR arrives organically.

Today's HTTP-FLV uses option 2 (gate non-init frames until first keyframe).
Use the same gate. Add a TODO for option 1 if join latency is too high.

## H.264 AVCC → AnnexB

RTMP delivers video as AVCC: `[length][NALU][length][NALU]...` where length
is 4-byte big-endian. AnnexB is `[0x000001][NALU][0x000001][NALU]...`.

The first video tag for each subscriber is the **AVC sequence header**:
`[FLV header byte][AVCPacketType=0]`. The body is an AVCDecoderConfigurationRecord
that contains SPS and PPS. You must extract those and emit:

```
0x00000001 SPS 0x00000001 PPS
```

before the first IDR. Then for each subsequent video tag, scan the AVCC NALUs
and emit each as `0x00000001 NALU`.

Reference: ISO/IEC 14496-15 § 5.2.4.1, and the existing FLV code in
`internal/core/protocol/flv/`. Copy the parsing patterns.

`internal/svc/rtmp/publish.go::isAVCSequenceHeader` already detects the
sequence header. `internal/core/bus.MediaMessage.IsInit` flags it.

## ICE / STUN / TURN

- **STUN**: free public servers exist. `stun:stun.l.google.com:19302` is the
  canonical default. Pion handles candidate gathering automatically.
- **TURN**: required for symmetric-NAT viewers. **Out of scope for v1.**
  Document that NAT-Traversal users may need to deploy their own coturn.
- **Host candidates only** is enough for LAN / same-network deployments and is
  the default Pion behaviour.

If `cfg.WebRTC.PublicIP` is set, configure Pion's `SettingEngine.SetNAT1To1IPs`
so ICE candidates advertise the right address. This is the typical
cloud-VM-with-elastic-IP setup.

## Metrics

Extend `internal/svc/metrics/collector.go` with:

- `nonchalant_webrtc_sessions{app,name}` (gauge)
- `nonchalant_webrtc_packets_sent_total{app,name}` (counter)
- `nonchalant_webrtc_bytes_sent_total{app,name}` (counter)
- `nonchalant_webrtc_rtt_seconds{app,name}` (histogram, from Pion's
  `pc.GetStats()`)

Pull from Pion stats via `pc.GetStats()` on each scrape.

## Tests

### Unit tests

- `h264_test.go` — AVCC → AnnexB on canned input (build a fake AVC seq header
  and a fake IDR; check NALU count and start codes).
- `opus_test.go` (cgo only) — encode 20 ms of silence, check it round-trips
  through a decoder.

### Integration test

`internal/itest/whep_test.go` — full chain:

1. Build the binary, start with default config.
2. Publish via ffmpeg as today.
3. Use [`github.com/pion/webrtc/v4`](https://github.com/pion/webrtc) **as a
   client** (in-test): construct a `PeerConnection`, generate an offer, POST
   to `/whep/live/X`, set the answer, register `OnTrack` handlers.
4. Assert: at least one RTP packet received on the video track within N
   seconds; H.264 NALU type appears.

This is much faster + more deterministic than driving a real browser, and
covers the wire format end-to-end.

### E2E (browser)

`e2e/tests/whep.spec.ts` — Playwright drives Chromium:

1. Reuse the existing `server` + `publisher` fixtures.
2. Open a static page that runs:
   ```js
   const pc = new RTCPeerConnection({iceServers: [{urls: 'stun:stun.l.google.com:19302'}]});
   pc.addTransceiver('video', {direction: 'recvonly'});
   pc.addTransceiver('audio', {direction: 'recvonly'});
   const offer = await pc.createOffer();
   await pc.setLocalDescription(offer);
   const resp = await fetch(whepURL, {method:'POST', headers:{'Content-Type':'application/sdp'}, body: offer.sdp});
   await pc.setRemoteDescription({type:'answer', sdp: await resp.text()});
   pc.ontrack = e => video.srcObject = e.streams[0];
   ```
3. Assert: `videoWidth > 0`, `currentTime > 0` after 5 s, no console errors.
4. Reuse the `pages/` static-server pattern from `e2e/fixtures/server.ts`.

## Pitfalls (read these before debugging anything)

1. **MTU**: Pion's `TrackLocalStaticSample` does FU-A fragmentation for you.
   Don't roll your own packetiser unless you want to learn this the hard way.
2. **Keyframe on join**: see above. Without it, the viewer sees green / black
   forever. The most common WebRTC server bug.
3. **SDP answer must include all ICE candidates** for non-trickle clients
   (which is what we tell viewers to use for v1). Use
   `<-webrtc.GatheringCompletePromise(pc)` before sending the response.
4. **Pion's `MediaEngine`** has to register codecs in the order you want them
   negotiated. Use `RegisterDefaultCodecs()` and override only if needed.
5. **Sample timestamps**: `media.Sample.Duration` is wall-clock, not RTP ticks.
   Pion converts. **Pass the actual frame duration**, not 0 or arbitrary values.
6. **Memory churn**: pooling matters. The bus is allocation-free in steady
   state; don't introduce per-frame allocations in the WebRTC subscriber.
   Reuse a NALU buffer across calls.
7. **DTLS handshake takes ~150 ms**. Don't tear down sessions for slow
   connect; let Pion's defaults handle.
8. **Memory leak on viewer disconnect**: track `pc.OnConnectionStateChange`
   and call `pc.Close()` plus detach the bus subscriber.
9. **Browser quirks**: Safari prefers H.264 packetization-mode-0 (single
   NAL); Chromium handles mode-1 (FU-A) fine. Pion advertises mode-1 by
   default; Safari should still negotiate. Test on real Safari before
   declaring victory.

## Effort estimate

Self-paced, single developer:

| Task | Days |
|---|---|
| Library install, package scaffolding, `RegisterRoutes` | 0.5 |
| WHEP signalling (POST/DELETE), Pion PC plumbing | 1.5 |
| AVCC → AnnexB H.264 + sample feeding | 2.0 |
| AAC → Opus transcoder (cgo libopus) | 2.5 |
| Keyframe-on-join + bus integration | 1.0 |
| Metrics integration | 0.5 |
| Unit + integration tests | 1.5 |
| Playwright E2E across Chrome / Safari | 1.5 |
| Docs (gen-docs additions, README updates, OPERATIONS.md) | 0.5 |

**Total: ~11 working days** for a production-quality WHEP-only deployment.

## Order of work (the actual TODO)

1. `go get github.com/pion/webrtc/v4`. Verify `go build ./...` still passes.
2. Create `internal/svc/whep/{service.go,handler.go}` with stubs that return
   501. Wire into `server.go` so `POST /whep/...` reaches the handler. Confirm
   in a smoke test.
3. Implement the WHEP signalling: parse offer, build PC with Pion, set
   local/remote descriptions, wait for ICE gathering, respond with answer.
   Test with `curl -X POST --data-binary @offer.sdp -H 'Content-Type: application/sdp'`
   using a hand-crafted offer.
4. Add a video `TrackLocalStaticSample` to the PC. Bridge to bus: when a video
   message arrives, convert AVCC to AnnexB, call `WriteSample`. Start with
   raw video only (no audio negotiated in SDP).
5. Add unit test for AVCC → AnnexB.
6. Open the URL in Chromium manually (or via a tiny static page) and confirm
   video plays. **First milestone — celebrate.**
7. Add audio: AAC decoder → 48 kHz PCM → libopus encoder → Opus
   `TrackLocalStaticSample`. Add unit test for the encode path.
8. Keyframe-on-join: cache last IDR on `bus.Stream` analogous to the AVC
   sequence header. Replay on attach. Test by joining mid-stream.
9. Metrics: extend the collector. Test via `/metrics` shape assertion.
10. Integration test (`internal/itest/whep_test.go`) using Pion as the client.
11. Playwright e2e (`e2e/tests/whep.spec.ts`).
12. Update `docs/ARCHITECTURE.md` (in `scripts/gen-docs/main.go`),
    `docs/CONFIG.md`, `docs/OPERATIONS.md`, README.
13. Run `make check && make lint && make test-race && make itest && make e2e`.
    Everything green = done.

## Out of scope (do not let scope creep happen)

- WHIP (publishing). Document that publishing remains RTMP-only for now.
- Simulcast / SVC.
- AV1 / VP9 / H.265.
- DataChannels (chat alongside video).
- TURN deployment.
- ABR adaptation (we don't have a transcoding ladder yet — see other plan
  proposal).
- Recording.
- Per-viewer DRM.
- Stats sub-protocol (RTCStats over a side channel).

If the user asks for any of these mid-session, **finish WHEP-only first** and
park the request as a follow-up.

## Open questions to surface early

- **Default codec for audio**: Opus (recommended) requires cgo. Should we
  build behind `-tags webrtc`, default ON, and document the dependency? Or
  default OFF? Decide with user before writing the cgo code.
- **Public IP / STUN configuration**: who's the target deployment (LAN /
  cloud / behind NAT)? This shapes how aggressive we make the defaults.
- **Browser support target**: Chrome + Firefox is easy. Safari sometimes needs
  packetization-mode-0 nudging. Confirm whether iOS Safari is required.

Surface these in the first message of the implementation session so the user
can pick before any code is written.

## Reference reading

- WHEP draft: <https://datatracker.ietf.org/doc/draft-ietf-wish-whep/>
- WHIP: RFC 9725 (for context, not implementation)
- Pion WebRTC v4 docs: <https://github.com/pion/webrtc/blob/master/README.md>
- Pion examples: <https://github.com/pion/webrtc/tree/master/examples>
  (especially `whip-whep` and `play-from-disk`)
- AVCDecoderConfigurationRecord: ISO/IEC 14496-15 § 5.2.4.1
- Existing nonchalant FLV/RTMP code: `internal/core/protocol/{flv,rtmp}/`
