<div align="center">
  <img src="assets/nonchalant-logo.png" alt="nonchalant logo" width="400">
</div>

# nonchalant

A high-performance, modular media server written in Go.

## Status

**Phases 1–8 complete + ops hardening + perf hot path** — RTMP ingest with
publish auth, HTTP-FLV, WebSocket-FLV, RTMP relay, HTTP API, native HLS / DASH
endpoints, Prometheus metrics, lint, CI, and a documented performance harness.

This server currently provides:

- Clean startup and graceful shutdown (signal-aware)
- Health endpoint (`/healthz`), Prometheus `/metrics`, `/debug/pprof/`
- YAML configuration with strict validation
- **RTMP ingest** with optional pre-shared-key publish authentication
- **HTTP-FLV output** — `GET /{app}/{name}.flv` (HTTP/1.1 hijack, one syscall per tag)
- **WebSocket-FLV output** — `ws://host/ws/{app}/{name}`
- **HLS** — `GET /hls/{app}/{name}/index.m3u8` (native, ffmpeg-backed, ABR ladder)
- **DASH** — `GET /dash/{app}/{name}.mpd` (native)
- **RTMP relay** — pull remote streams or push local streams (ffmpeg supervised)
- **HTTP API** — `/api/server`, `/api/streams` (with drop counts), `/api/relay`
- **FFmpeg integration** — optional cgo transcoding (build with `-tags ffmpeg`)
- Lock-free single-producer / multi-cursor shared-log bus
- Per-stream wrap-around arena allocator — zero allocations per RTMP frame
- Wake-on-publish — idle server sits near 0% CPU instead of polling
- Three test layers: unit, integration (real ffmpeg), browser E2E (Playwright)
- `golangci-lint v2` config, GitHub Actions CI

## Performance

The fan-out hot path has been profiled and rewritten three times. Headline
numbers from `scripts/perf/` on an Apple M5 (see [reports/PERF-WOP.pdf](reports/PERF-WOP.pdf)
for the full report including pprof tables and quality markers):

| Scenario              | Pre-WOP   | Post-WOP + Hijack |
| --------------------- | --------- | ----------------- |
| Idle CPU (no clients) | ~41 %     | ~4.7 %            |
| 1024 subs throughput  | 184 MB/s  | 357 MB/s          |
| Per-frame allocations | 3 / 64 KB | 0 / 0 B           |

What did the heavy lifting:

- **Shared-log bus** (`internal/core/bus/sharedlog.go`) — single-producer
  multi-cursor ring; `Publish` is one atomic add + one atomic store regardless
  of subscriber count. 1 pub × 512 subs went from 3.93 µs to 74 ns.
- **Per-stream Arena** (`internal/core/bus/arena.go`) — wrap-around bump
  allocator for payload buffers. Eliminated the 64 KB sync.Pool allocation
  per frame that pprof showed dominating the publisher path.
- **Wake-on-publish** — subscribers park on an `atomic.Pointer[chan struct{}]`
  closed by the publisher instead of busy-polling.
- **HTTP/1.1 hijack** (`internal/svc/httpflv/handler.go:91`) — bypasses Go's
  chunked-transfer encoding and the `bufio` + `ResponseWriter.Flush` double
  flush. Each FLV tag is now exactly one `syscall.write` to the TCP socket.

Reproduce locally:

```bash
scripts/perf/idle.sh         # idle CPU sample
scripts/perf/run.sh          # subscriber sweep
scripts/perf/profile.sh prof # 30 s pprof at 1024 subs
python scripts/perf/build_report.py  # rebuild reports/PERF-WOP.pdf
```

## Quick Start

### Build and Run

**Default build (without FFmpeg):**
```bash
make build
make run
```

**Build with FFmpeg support:**
```bash
go build -tags ffmpeg -o bin/nonchalant ./cmd/nonchalant
```
Requires FFmpeg development libraries installed.

### Run Tests

```bash
make test          # unit tests (Go)
make test-race     # unit tests with the race detector
make itest         # integration tests (real binary + ffmpeg CLI)
make e2e-install   # one-time: install Playwright + Chromium for browser E2E
make e2e           # browser E2E tests (real flv.js playback in Chromium)
```

See [docs/TESTING.md](docs/TESTING.md) and [e2e/README.md](e2e/README.md) for
details on each layer.

### Run Checks

```bash
make check
```

### Docker

```bash
make docker-build
make docker-run
```

## Configuration

Copy `configs/nonchalant.example.yaml` and modify as needed:

```yaml
server:
  health_port: 8080  # Port for /healthz endpoint
  http_port: 8081    # Port for HTTP-FLV, WS-FLV, HLS, DASH, API, /metrics
  rtmp_port: 1935    # Port for RTMP ingest

auth:                # Optional. Omit for anonymous publishing.
  publish_keys:
    - changeme       # Publishers must use rtmp://host/live/foo?key=changeme

relays:              # Optional: RTMP relay tasks
  - app: live
    name: mystream
    mode: pull       # or "push"
    remote_url: rtmp://remote-server:1935/live/mystream
    reconnect: true
```

See [docs/CONFIG.md](docs/CONFIG.md) for the full schema.

## Usage

### Publishing a Stream

Publish a stream via RTMP:

```bash
ffmpeg -re -i input.mp4 -c copy -f flv rtmp://localhost:1935/live/mystream
```

If `auth.publish_keys` is configured, append the key:

```bash
ffmpeg -re -i input.mp4 -c copy -f flv \
  'rtmp://localhost:1935/live/mystream?key=changeme'
```

### Playing a Stream

Play a stream via HTTP-FLV:

```bash
ffplay http://localhost:8081/live/mystream.flv
```

Or use any FLV-compatible player with the URL: `http://localhost:8081/{app}/{name}.flv`

Play a stream via WebSocket-FLV (browser-compatible):

```javascript
const ws = new WebSocket('ws://localhost:8081/ws/live/mystream');
ws.binaryType = 'arraybuffer';
ws.onmessage = (event) => {
  // First message contains FLV header
  // Subsequent messages contain FLV tags
  const flvData = new Uint8Array(event.data);
  // Process FLV data with your player
};
```

Or use any WebSocket-FLV compatible player with the URL: `ws://host:port/ws/{app}/{name}`

### HLS / DASH

nonchalant ships native HLS and DASH endpoints. The first request lazily spawns
an `ffmpeg` subprocess that pulls the server's own HTTP-FLV stream and serves
segments from a temp directory. Idle packagers are GC'd after 60 s.

```bash
# HLS — single rendition (stream-copy)
ffplay http://localhost:8081/hls/live/mystream.m3u8

# DASH
http://localhost:8081/dash/live/mystream.mpd
```

**ABR (multi-bitrate)** kicks in when `hls.ladder` is configured with one
entry per rendition. The `.m3u8` becomes a master playlist listing each rung,
and per-rendition files live under `/hls/{app}/{name}/{rung}/`:

```yaml
hls:
  ladder:
    - {name: 720p, width: 1280, height: 720, video_bitrate: 2500}
    - {name: 480p, width: 854,  height: 480, video_bitrate: 1100}
    - {name: 240p, width: 426,  height: 240, video_bitrate: 400}
```

`ffmpeg` must be on the server's PATH. If it's missing, the endpoints return
503. See [docs/OPERATIONS.md](docs/OPERATIONS.md) for details.

### API Endpoints

Query server state via HTTP API:

```bash
# Server information
curl http://localhost:8081/api/server

# Active streams
curl http://localhost:8081/api/streams

# Relay tasks
curl http://localhost:8081/api/relay

# Restart a relay task
curl -X POST http://localhost:8081/api/relay/restart \
  -H "Content-Type: application/json" \
  -d '{"app":"live","name":"mystream"}'

# Prometheus metrics
curl http://localhost:8081/metrics
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design and structure
- [Configuration](docs/CONFIG.md) - Configuration schema (incl. auth + relays)
- [Operations](docs/OPERATIONS.md) - Endpoints, metrics, auth, HLS/DASH ops
- [Usage Guide](docs/USAGE.md) - Step-by-step feature usage
- [Testing](docs/TESTING.md) - Unit, integration, and browser E2E layers
- [Browser E2E](e2e/README.md) - Playwright suite for WS-FLV / HTTP-FLV / API

## License

MIT License with Attribution Requirement

This project is licensed under the MIT License with an additional attribution
requirement. See [LICENSE](LICENSE) for full details.

**Important:** Any use, modification, or derivative work must prominently
acknowledge the original nonchalant project. This acknowledgment must appear
in all documentation, source code headers, and published materials.
