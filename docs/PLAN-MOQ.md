# Plan — Media over QUIC (MoQ) for nonchalant

This is a session-bootable plan. Read it cold; everything you need is here.
There is intentionally no surrounding conversation.

## Read this first: reality check

**MoQ is bleeding-edge.** The IETF `moq` working group's transport draft has
gone through 10+ revisions and is still moving. There is **no widely deployed
client** today. The reference implementation is [`moq-rs`](https://github.com/kixelated/moq-rs)
(Rust, Twitch). Real browser support depends on WebTransport (Chromium yes,
Firefox partial, Safari no as of the most recent stable).

Building MoQ in nonchalant today is **infrastructure for a future** — it lets
you say "we speak MoQ" once the ecosystem catches up, and lets you experiment
with sub-second relay topologies that WebRTC can't easily do. But you will
not have a population of viewers to test against. Plan accordingly.

If you want sub-second live with users in 2026, build [WebRTC](PLAN-WEBRTC.md)
instead. Build MoQ as an additional output, not a replacement.

## What MoQ actually is

A media transport over QUIC, with first-class support for:

- **Named tracks** — e.g. `nonchalant/live/foo/video` is a track. Publishers
  push to it, subscribers subscribe to it.
- **Groups** — a track is a sequence of groups. A group is roughly one
  GOP-or-keyframe-interval (~1 s of media). Group ID monotonically increases.
- **Objects** — a group is a sequence of objects. An object is one
  encoded frame plus metadata. Object ID monotonically increases within a
  group.
- **Relays** — built into the protocol. A relay can subscribe upstream and
  fan out to its own subscribers without re-encoding. Cache-aware.
- **Object-per-stream**: each MoQ object can ride a separate QUIC stream. Lost
  / late objects don't block subsequent ones (unlike a single TCP connection).

Reading order if you've never touched MoQ:

1. The architecture overview in `moq-rs` README
2. `draft-ietf-moq-transport` (latest version — pin in the implementation)
3. One of the media-binding drafts (`draft-ietf-moq-warp` or
   `draft-ietf-moq-streaming-format-cmaf-v01`); pick one and stick with it.

## Target spec version

Pin to a specific transport draft version at session start. **Do not chase HEAD.**

- `draft-ietf-moq-transport` — pick the most recent version supported by
  `moq-rs` at the time the session begins (check the moq-rs `Cargo.toml`).
  Mismatch with the reference client = nothing works.
- Document the pinned version in the WireConfig comment (see below).
- Plan to bump it once a session per six months — this is not a stable RFC.

## Library choices

### QUIC

[`github.com/quic-go/quic-go`](https://github.com/quic-go/quic-go) — mature,
production-quality, supports server-initiated streams, all the v1 features.
Used by Cloudflare, Caddy. Pure Go. **No competitor; pick it.**

### MoQ

This is the hard part. Options, in decreasing order of preference:

1. **[`github.com/mengelbart/moqtransport`](https://github.com/mengelbart/moqtransport)**
   — pure-Go MoQ-Transport implementation. Most complete Go option. Track its
   commit history before depending — pin a specific commit, not a tag.
2. **Build a minimal MoQ-Transport ourselves** on top of quic-go. ~1500 lines.
   Worth it only if (1) is broken or unmaintained at the time. Most of the
   complexity is in control messages and parameter negotiation.
3. **Wrap moq-rs via cgo** — don't. Building cross-language tooling is its
   own engineering project.

Pick (1). If it doesn't work or the API is too unstable, fall back to (2)
with a strict budget.

## Architecture

```
RTMP publisher ─▶ rtmp ingest ─▶ bus.Stream ─▶ MoQ publisher goroutine
                                                     │
                                    ┌────────────────┼────────────────┐
                                    │                │                │
                              video track       audio track       catalog track
                              (groups,objs)     (groups,objs)     (announce metadata)
                                    │                │                │
                                    ▼                ▼                ▼
                                  quic-go MoQ session (one per subscriber)
                                          │
                                          ▼
                                      WebTransport (browser) or
                                      raw QUIC (moq-rs CLI, future SDKs)
```

One MoQ session per subscriber, like WebRTC. Subscribers attach to the bus
exactly like other output services. The MoQ layer cuts the bus's `MediaMessage`
stream into groups (one group per video keyframe) and emits objects.

## Files to create

```
internal/svc/moq/
├── service.go         # constructor + RegisterRoutes
├── handler.go         # WebTransport upgrade + raw QUIC ALPN selector
├── publisher.go       # bus subscriber → groups + objects
├── encoder.go         # MediaMessage → moq.Object framing
├── catalog.go         # MoQ catalog track (track list, codec descriptors)
└── listener.go        # standalone QUIC listener for non-WebTransport clients
```

All under 300 lines each. Likely you'll need to split `publisher.go` once
group/object accounting expands.

## Wiring

In `internal/server/server.go`:

```go
moqSvc, err := moq.NewService(registry, cfg.MoQ, playKeys)
if err != nil { log.Printf("MoQ disabled: %v", err) } else {
    moqSvc.RegisterRoutes(mux)               // mounts WebTransport at /moq
    go moqSvc.ListenQUIC(ctx)                // standalone QUIC on cfg.MoQ.Port
}
```

WebTransport rides on the existing HTTP/3 server (which we'll need to add as
a precondition — see "Prerequisites"). Native QUIC clients hit a separate UDP
port. Both feed the same MoQ session machinery.

## Prerequisites

These must exist before the MoQ session starts:

1. **HTTP/3 server.** `quic-go`'s `http3.Server` listening on a UDP port,
   sharing the existing `http.Handler` mux. This is option A from the earlier
   QUIC discussion — start there if it isn't already merged. ~1 day.
2. **TLS certificates.** WebTransport requires HTTPS. Self-signed for dev,
   document Let's Encrypt for prod. Add `tls_cert` / `tls_key` to config.
3. **WebTransport-Go.** `github.com/quic-go/webtransport-go`. Wraps quic-go.

Without (1) and (2) the user has nothing to connect with from a browser. Do
not start MoQ until those are landed.

## Config

Add to `internal/config/config.go`:

```go
type MoQConfig struct {
    // UDP port for native (non-WebTransport) MoQ-over-QUIC clients.
    // 0 disables the standalone listener; WebTransport still works.
    Port uint16 `yaml:"port,omitempty"`

    // Path to TLS cert and key. Required for WebTransport and native QUIC.
    // QUIC has no plaintext mode.
    TLSCert string `yaml:"tls_cert,omitempty"`
    TLSKey  string `yaml:"tls_key,omitempty"`

    // Track namespace prefix. Tracks are published as
    // "{namespace}/{app}/{name}/{video|audio|catalog}".
    Namespace string `yaml:"namespace,omitempty"`
}
```

Validate `tls_cert` and `tls_key` exist if MoQ is enabled. Default
`Namespace = "nonchalant"`.

## Track layout

Per RTMP stream, publish three MoQ tracks:

```
{namespace}/{app}/{name}/catalog   # JSON describing the other tracks
{namespace}/{app}/{name}/video     # H.264 NALUs
{namespace}/{app}/{name}/audio     # AAC frames (or Opus if we transcode)
```

Catalog track is a single object that announces the codec parameters
(SPS/PPS for H.264, AudioSpecificConfig for AAC). Subscribers fetch this
first. See `draft-ietf-moq-catalog`.

Pick one media binding draft and stick with it. Recommended: the
**simple-CMAF-style** binding — each video object is one CMAF chunk
(fMP4 fragment), each audio object is one frame. This is what `moq-rs`
demos. Avoid WARP for v1 — it's optimized for screen sharing.

## Group / object accounting

```
group   = video keyframe interval (~1 second of media)
object  = one frame within that group
group_id = monotonic, starts at 0
object_id = monotonic, starts at 0 within each group
```

Subscriber goroutine pseudo-code:

```go
var groupID, objectID uint64
for msg := range busSub.Buffer() {
    if msg.Type == bus.MessageTypeVideo && flv.IsVideoKeyframe(msg.Payload) {
        groupID++
        objectID = 0
        // Optional: signal end-of-previous-group to the MoQ track
    }
    obj := moq.Object{
        TrackID:   videoTrackID,
        GroupID:   groupID,
        ObjectID:  objectID,
        Priority:  0,
        Payload:   convertAVCCToCMAF(msg.Payload),
    }
    track.WriteObject(obj)
    objectID++
}
```

Same pattern for audio. Audio groups can be aligned with video groups (same
group IDs) for easier sync, or independent — pick aligned for simplicity.

## Subscription handling

When a subscriber sends `SUBSCRIBE` for a track:

- Reply with the most recent group's start, in subscribe-mode terms:
  `LATEST_GROUP` semantics — start delivery at the first keyframe boundary
  that hasn't been finalised yet.
- Replay the last cached IDR's group from the start (we already cache the
  AVC sequence header on the bus; cache the last keyframe too — same
  extension we plan for WebRTC join-on-keyframe).

`moqtransport` exposes a `Track.WriteObject` API and handles the SUBSCRIBE
state machine. Worth verifying this on a small toy program before writing
nonchalant integration.

## TLS / certificates

Add a small certificate-loading helper. Default behaviour when no cert is
configured: refuse to start the MoQ service and log a clear error. Do not
silently fall back to plaintext (impossible) or to no-MoQ (silently breaks
the deployment).

For local development: ship a `make moq-dev-cert` target that emits a
self-signed cert into `./certs/`. Document that browsers will need
`chrome://flags/#allow-insecure-localhost` or equivalent to test.

## Tests

### Unit tests

- `encoder_test.go` — AVCC frame → CMAF chunk on canned input.
- `catalog_test.go` — JSON catalog round-trips through the MoQ catalog draft.
- `publisher_test.go` — group ID increments on keyframe; object ID increments
  per frame; resets correctly.

### Integration tests

There is no battle-tested Go MoQ client. Two options for integration:

1. **Use `moqtransport` as the in-test client** — same library as the server,
   talking to itself through a real QUIC connection. This proves wire format
   compatibility with itself, not with the world. Cheap and worth doing.
2. **Spawn `moq-rs` as a subprocess** — install Rust toolchain in CI, build
   `moq-pub` / `moq-sub` binaries, drive them as fixtures. Slow (minutes per
   build) and flaky (Rust toolchain on CI). Worth it for a single end-to-end
   compatibility check.

Recommended: do (1) for every test, (2) once per CI run as a separate job
that can be skipped on PRs.

### E2E (browser)

WebTransport from Chromium via Playwright:

```js
const wt = new WebTransport('https://localhost:8443/moq');
await wt.ready;
const stream = await wt.createBidirectionalStream();
// ... write SUBSCRIBE control messages, read objects
```

Plus a tiny pure-JS MoQ client that decodes catalog + video objects, feeds
them into a `MediaSource` `<video>`. **You will probably need to write this
client.** It's the single biggest hidden cost of choosing MoQ today — the
ecosystem doesn't give you a player.

Budget at least 3 days for the JS client. Reference: `moq-rs` web demo.

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Spec changes mid-session | High | Pin a specific draft. Do not bump. |
| `moqtransport` library unstable | High | Pin a commit. Read its issues before depending. |
| Browser WebTransport gaps (Safari, Firefox) | Certain | Document support matrix. Not your bug to fix. |
| No JS client to test against | Certain | Budget time to write a minimal player. |
| QUIC port blocked by firewall | High | Document the requirement clearly. UDP is often filtered. |
| TLS certificate provisioning UX | Medium | Provide `make moq-dev-cert` and good docs. |
| Performance on large fan-out | Unknown | Benchmark early. quic-go is good but per-session cost is higher than TCP. |

## Effort estimate

This is genuinely 3–4 weeks of work. Don't pretend otherwise.

| Task | Days |
|---|---|
| HTTP/3 server prerequisite | 1 |
| Cert plumbing + dev-cert tooling | 1 |
| Library evaluation: try `moqtransport`, decide go/no-go | 1 |
| MoQ session machinery (connect, setup, announce, subscribe) | 3 |
| Track abstraction wired to bus subscriber | 2 |
| Encoder: AVCC → CMAF chunks; AAC framing | 3 |
| Catalog track | 1 |
| Group / object accounting + keyframe-on-join | 2 |
| Unit + integration tests (Go-to-Go) | 2 |
| Minimal browser client (JS) | 3 |
| Playwright e2e | 2 |
| `moq-rs` interop test (CI subprocess) | 2 |
| Metrics, logging, docs | 2 |

**Total: ~22 working days** if everything goes well. Add 30% for spec churn,
library issues, and the time you spend reading drafts.

## Order of work

1. **Spike, do not commit**: `go get` `quic-go` and `moqtransport`. Run the
   library's own examples end-to-end on localhost. If they don't work, stop
   and tell the user — moq-go ecosystem is not ready.
2. Add `internal/config.MoQConfig` + cert plumbing + HTTP/3 server. Don't
   wire MoQ yet; just confirm HTTPS works on a UDP port.
3. Stub `internal/svc/moq/` package with a `Service` that returns 501. Wire
   into `server.go`. Confirm `make build` and `make check` still pass.
4. Implement MoQ session setup (CONNECT, SETUP, role/version negotiation).
   Test with `moqtransport`'s own client in a unit test.
5. Implement ANNOUNCE for the three tracks. Confirm a Go client can list them.
6. Implement video track publication. Use a hand-crafted publisher loop with
   canned NALUs first, no bus integration. Verify a Go client receives objects
   with correct group/object IDs.
7. Connect the bus: a `Subscriber` that consumes `bus.MediaMessage` and
   pushes to MoQ tracks. Group on keyframes. Test end-to-end with bus +
   in-process MoQ client.
8. Add audio track and catalog track.
9. Unit + integration tests, golangci-lint, line caps. Bring `make check` /
   `make lint` / `make test-race` back to green.
10. Write the JS client. Verify in Chrome.
11. Playwright e2e.
12. `moq-rs` interop CI job (optional but recommended).
13. Update the doc generator with the new endpoints, config, and tests.
14. README.

## Out of scope

- Multi-publisher relay topology (we publish from RTMP only; we don't pull
  from other MoQ relays).
- Caching layer for MoQ objects across viewers (in-memory dedup is implicit;
  we don't persist).
- Any media binding other than the simple CMAF profile we picked.
- WARP / screen-sharing-optimised profile.
- TURN equivalents — QUIC doesn't have them; viewers behind blocked-UDP
  networks just won't connect.
- DRM.
- WHIP/WHEP-over-QUIC variants.
- Protocol negotiation between MoQ versions on the same connection.

## Open questions to surface early

- **Specific `draft-ietf-moq-transport` version**: confirm against `moq-rs`
  at session start. Mismatch = no interop.
- **Media binding**: pin one of the published drafts — likely
  `draft-ietf-moq-streaming-format-cmaf-v01` or its successor. Do not invent
  your own.
- **Audio codec**: AAC pass-through saves CPU (re-uses RTMP audio directly),
  Opus is more browser-friendly. Pick AAC for v1 since browser MoQ client
  support matters less than spec correctness.
- **Cert source for dev**: shipped `make moq-dev-cert`, or expect user to
  bring their own? The first is more useful; the second is more honest.

Surface these in the first message of the implementation session.

## Reference reading

- IETF moq working group: <https://datatracker.ietf.org/wg/moq/about/>
- Transport draft (track index): <https://datatracker.ietf.org/doc/draft-ietf-moq-transport/>
- Catalog draft: <https://datatracker.ietf.org/doc/draft-ietf-moq-catalog/>
- `moq-rs` reference impl: <https://github.com/kixelated/moq-rs>
- `moqtransport` Go: <https://github.com/mengelbart/moqtransport>
- quic-go: <https://github.com/quic-go/quic-go>
- WebTransport (browser): <https://developer.mozilla.org/en-US/docs/Web/API/WebTransport>
- Existing nonchalant FLV/RTMP code: `internal/core/protocol/{flv,rtmp}/`
- Existing pkger as a model for "stream → format conversion service":
  `internal/svc/pkger/`

## When this session is done

- `make check && make lint && make test-race && make itest && make e2e` are
  all green.
- The minimum viable demo: publish via RTMP, open a Chromium tab pointed at
  `https://localhost:8443/moq/...`, see live video play within ~500 ms of
  publish.
- A `docs/OPERATIONS.md` section explains how to operate MoQ in production
  (port, cert, namespace, `moq-rs` interop status).
- Remind the user: this is for early adopters. If their viewers want a
  reliable experience today, they should use HLS, DASH, or — when shipped —
  WebRTC.
