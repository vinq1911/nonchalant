<!--
If you are AI: This is the main README for the nonchalant repository.
It provides an overview and quick start instructions.
-->

<div align="center">
  <img src="assets/nonchalant-logo.png" alt="nonchalant logo" width="400">
</div>

# nonchalant

A high-performance, modular media server written in Go.

## Status

**Phase 7 Complete** - RTMP Ingest, HTTP-FLV, WebSocket-FLV, RTMP Relay & HTTP API

This server currently provides:
- Clean startup and graceful shutdown
- Health endpoint (`/healthz`)
- YAML configuration with validation
- **RTMP ingest** - Accept RTMP publisher connections
- **HTTP-FLV output** - Stream live media via HTTP-FLV (`GET /{app}/{name}.flv`)
- **WebSocket-FLV output** - Stream live media via WebSocket-FLV (`ws://host/ws/{app}/{name}`)
- **RTMP relay** - Pull remote streams or push local streams to remote servers
- **HTTP API** - Introspection and control endpoints (`/api/server`, `/api/streams`, `/api/relay`)
- Core stream bus with efficient fanout
- Integration tests
- Documentation generation
- Code quality enforcement

## Quick Start

### Build and Run

```bash
make build
make run
```

### Run Tests

```bash
make test
make itest
```

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
  health_port: 8080  # Port for health endpoint
  http_port: 8081    # Port for HTTP-FLV and WebSocket-FLV
  rtmp_port: 1935    # Port for RTMP ingest

relays:  # Optional: RTMP relay tasks
  - app: live
    name: mystream
    mode: pull  # or "push"
    remote_url: rtmp://remote-server:1935/live/mystream
    reconnect: true  # Optional: enable reconnect on failure
```

## Usage

### Publishing a Stream

Publish a stream via RTMP:

```bash
ffmpeg -re -i input.mp4 -c copy -f flv rtmp://localhost:1935/live/mystream
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
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design and structure
- [Configuration](docs/CONFIG.md) - Configuration schema
- [Testing](docs/TESTING.md) - How to run tests

## Development

This project follows strict discipline:
- Files must not exceed 300 lines
- All files must have AI headers
- All functions must have comments
- `make check` must pass at all times

See `.cursor/rules.md` for complete engineering contract.

## License

MIT License with Attribution Requirement

This project is licensed under the MIT License with an additional attribution
requirement. See [LICENSE](LICENSE) for full details.

**Important:** Any use, modification, or derivative work must prominently
acknowledge the original nonchalant project. This acknowledgment must appear
in all documentation, source code headers, and published materials.
