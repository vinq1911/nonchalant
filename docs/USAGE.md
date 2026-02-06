<!--
If you are AI: This file provides step-by-step usage instructions for nonchalant.
It covers all features including RTMP ingest, HTTP-FLV, WebSocket-FLV, relay, and API.
-->

# Usage Guide

This guide provides step-by-step instructions for using all features of nonchalant.

## Prerequisites

- nonchalant server built and running
- FFmpeg installed (for publishing/playing streams)
- Test video file in `assets/nonchalant-test.mp4` (for testing)

## Quick Start

1. **Start the server:**
   ```bash
   make run
   ```

2. **Verify server is running:**
   ```bash
   curl http://localhost:8081/healthz
   ```

## Feature 1: RTMP Ingest (Publishing Streams)

Publish a live stream to nonchalant via RTMP.

### Step 1: Start the server
```bash
make run
```

### Step 2: Publish a stream using FFmpeg
```bash
# Publish test video as live stream
ffmpeg -re -i assets/nonchalant-test.mp4 \
  -c copy -f flv \
  rtmp://localhost:1935/live/mystream
```

**What this does:**
- `-re`: Read input at native frame rate (simulates live)
- `-i assets/nonchalant-test.mp4`: Input file
- `-c copy`: Copy codecs (no transcoding)
- `-f flv`: Output format (FLV for RTMP)
- `rtmp://localhost:1935/live/mystream`: RTMP URL (app: `live`, stream: `mystream`)

### Step 3: Verify stream is published
```bash
# Check API for active streams
curl http://localhost:8081/api/streams
```

Expected response:
```json
{
  "streams": [
    {
      "app": "live",
      "name": "mystream",
      "has_publisher": true,
      "subscriber_count": 0
    }
  ]
}
```

## Feature 2: HTTP-FLV Output (Playing Streams)

Play a stream via HTTP-FLV using any FLV-compatible player.

### Step 1: Publish a stream (see Feature 1)

### Step 2: Play the stream via HTTP-FLV
```bash
# Using FFplay
ffplay http://localhost:8081/live/mystream.flv

# Or using VLC
vlc http://localhost:8081/live/mystream.flv

# Or using curl to save to file
curl http://localhost:8081/live/mystream.flv -o output.flv
```

**URL format:** `http://host:port/{app}/{name}.flv`

### Step 3: Verify subscriber count
```bash
curl http://localhost:8081/api/streams
```

The `subscriber_count` should increase when clients connect.

## Feature 3: WebSocket-FLV Output (Browser Playback)

Play a stream in a web browser using WebSocket-FLV.

### Step 1: Publish a stream (see Feature 1)

### Step 2: Create HTML test page
Create `test_wsflv.html`:
```html
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket-FLV Test</title>
</head>
<body>
    <h1>WebSocket-FLV Playback</h1>
    <video id="video" controls autoplay></video>
    <script>
        const ws = new WebSocket('ws://localhost:8081/ws/live/mystream');
        const video = document.getElementById('video');
        let flvPlayer = null;

        ws.binaryType = 'arraybuffer';
        
        ws.onopen = () => {
            console.log('WebSocket connected');
        };

        ws.onmessage = (event) => {
            const data = new Uint8Array(event.data);
            
            // First message is FLV header
            if (!flvPlayer) {
                // Initialize FLV.js player
                if (typeof flvjs !== 'undefined') {
                    flvPlayer = flvjs.createPlayer({
                        type: 'flv',
                        isLive: true,
                        url: URL.createObjectURL(new Blob([data]))
                    });
                    flvPlayer.attachMediaElement(video);
                    flvPlayer.load();
                } else {
                    console.error('FLV.js not loaded');
                }
            } else {
                // Subsequent messages are FLV tags
                // FLV.js handles this automatically via MediaSource
            }
        };

        ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };

        ws.onclose = () => {
            console.log('WebSocket closed');
            if (flvPlayer) {
                flvPlayer.destroy();
            }
        };
    </script>
    <!-- Include FLV.js for browser playback -->
    <script src="https://cdn.jsdelivr.net/npm/flv.js@latest/dist/flv.min.js"></script>
</body>
</html>
```

### Step 3: Open in browser
Open `test_wsflv.html` in a web browser.

**WebSocket URL format:** `ws://host:port/ws/{app}/{name}`

## Feature 4: RTMP Relay (Pull Mode)

Pull a remote RTMP stream and republish it locally.

### Step 1: Configure relay in `configs/nonchalant.example.yaml`
```yaml
server:
  health_port: 8080
  http_port: 8081
  rtmp_port: 1935

relays:
  - app: live
    name: pulled_stream
    mode: pull
    remote_url: rtmp://remote-server:1935/live/source_stream
    reconnect: true
    max_retries: 5
    retry_delay_seconds: 5
```

### Step 2: Start the server
```bash
make run
```

### Step 3: Verify relay is running
```bash
curl http://localhost:8081/api/relay
```

### Step 4: Play the relayed stream
```bash
ffplay http://localhost:8081/live/pulled_stream.flv
```

## Feature 5: RTMP Relay (Push Mode)

Push a local stream to a remote RTMP server.

### Step 1: Configure relay in `configs/nonchalant.example.yaml`
```yaml
relays:
  - app: live
    name: mystream
    mode: push
    remote_url: rtmp://remote-server:1935/live/destination
    reconnect: true
```

### Step 2: Start the server
```bash
make run
```

### Step 3: Publish a local stream (see Feature 1)

### Step 4: Verify relay is pushing
```bash
curl http://localhost:8081/api/relay
```

The relay will automatically push the local stream to the remote server.

## Feature 6: HTTP API

Query server state and control relay tasks.

### Get Server Information
```bash
curl http://localhost:8081/api/server
```

Response:
```json
{
  "version": "1.0.0",
  "uptime": 12345,
  "go_version": "go1.25",
  "enabled_services": [
    "rtmp_ingest",
    "http_flv",
    "ws_flv",
    "relay"
  ]
}
```

### Get Active Streams
```bash
curl http://localhost:8081/api/streams
```

Response:
```json
{
  "streams": [
    {
      "app": "live",
      "name": "mystream",
      "has_publisher": true,
      "subscriber_count": 2
    }
  ]
}
```

### Get Relay Tasks
```bash
curl http://localhost:8081/api/relay
```

Response:
```json
{
  "tasks": [
    {
      "app": "live",
      "name": "pulled_stream",
      "mode": "pull",
      "remote_url": "rtmp://remote-server:1935/live/source",
      "running": true
    }
  ]
}
```

### Restart a Relay Task
```bash
curl -X POST http://localhost:8081/api/relay/restart \
  -H "Content-Type: application/json" \
  -d '{"app":"live","name":"pulled_stream"}'
```

## Feature 7: FFmpeg Integration (Optional)

Transcode streams using FFmpeg (requires build with `-tags ffmpeg`).

### Step 1: Build with FFmpeg support
```bash
go build -tags ffmpeg -o bin/nonchalant ./cmd/nonchalant
```

### Step 2: Configure transcoding
```yaml
transcode:
  enabled: true
  profiles:
    - name: hls_profile
      app: live
      stream: mystream
      format: hls
      output_url: /tmp/hls/output.m3u8
```

### Step 3: Start the server
```bash
./bin/nonchalant --config configs/nonchalant.example.yaml
```

### Step 4: Publish a stream
The transcoding task will automatically process the stream.

## Testing with Make Targets

nonchalant provides make targets to test send/receive functionality using the test video:

### Complete Round-Trip Test
```bash
make test-video
```
Tests the complete flow: publish test video → verify via API → receive via HTTP-FLV.

### Send Only (RTMP Publish)
```bash
make test-send
```
Tests publishing the test video via RTMP and verifies it's registered.

### Receive Only (HTTP-FLV Playback)
```bash
make test-receive
```
Tests receiving a stream via HTTP-FLV (automatically publishes in background).

### Complete Round-Trip with Verification
```bash
make test-roundtrip
```
Full test: SEND → API verification → RECEIVE → FLV validation.

### WebSocket-FLV Test
```bash
make test-send-receive-ws
```
Tests send/receive via WebSocket-FLV protocol.

### Manual Testing Workflow

1. **Start server:**
   ```bash
   make run
   ```

2. **Publish test video:**
   ```bash
   ffmpeg -re -i assets/nonchalant-test.mp4 \
     -c copy -f flv \
     rtmp://localhost:1935/live/teststream
   ```

3. **Play via HTTP-FLV (in another terminal):**
   ```bash
   ffplay http://localhost:8081/live/teststream.flv
   ```

4. **Check API:**
   ```bash
   curl http://localhost:8081/api/streams
   ```

5. **Stop publishing (Ctrl+C on FFmpeg)**

6. **Verify stream is removed:**
   ```bash
   curl http://localhost:8081/api/streams
   ```

## Troubleshooting

### Server won't start
- Check if ports are already in use: `lsof -i :8081 -i :1935`
- Verify config file is valid: `./bin/nonchalant --config configs/nonchalant.example.yaml`

### Stream not appearing
- Verify RTMP publish succeeded (check FFmpeg output)
- Check API: `curl http://localhost:8081/api/streams`
- Verify stream key matches: `{app}/{name}`

### HTTP-FLV playback fails
- Verify stream has a publisher: `curl http://localhost:8081/api/streams`
- Check player supports FLV format
- Verify URL format: `http://host:port/{app}/{name}.flv`

### WebSocket-FLV not working
- Check browser console for errors
- Verify WebSocket URL format: `ws://host:port/ws/{app}/{name}`
- Ensure FLV.js library is loaded

### Relay not working
- Check relay configuration in YAML
- Verify remote RTMP server is accessible
- Check API: `curl http://localhost:8081/api/relay`
- Review server logs for relay errors

## Next Steps

- See [ARCHITECTURE.md](ARCHITECTURE.md) for system design
- See [CONFIG.md](CONFIG.md) for configuration options
- See [TESTING.md](TESTING.md) for testing instructions
