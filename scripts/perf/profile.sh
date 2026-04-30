#!/usr/bin/env bash
# If you are AI: Capture a 30-second CPU profile of nonchalant under the
# 1024-subscriber load. Output: reports/data/cpu-<label>.prof and a top-30
# textual summary at reports/data/cpu-<label>.txt.

set -euo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd)
cd "$REPO"

LABEL="${1:-prof}"
HTTP_PORT=18101
RTMP_PORT=11937
HEALTH_PORT=18100
DATA_DIR="reports/data"
mkdir -p "$DATA_DIR"

go build -o bin/nonchalant ./cmd/nonchalant
go build -o bin/loadtest   ./cmd/loadtest

CFG=$(mktemp); trap 'rm -f "$CFG"' EXIT
cat > "$CFG" <<EOF
server:
  health_port: $HEALTH_PORT
  http_port:   $HTTP_PORT
  rtmp_port:   $RTMP_PORT
EOF

./bin/nonchalant --config "$CFG" > "$DATA_DIR/profile-server-${LABEL}.log" 2>&1 &
SERVER_PID=$!
trap 'kill $SERVER_PID 2>/dev/null || true; rm -f "$CFG"' EXIT
for _ in $(seq 1 50); do
    curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" >/dev/null 2>&1 && break
    sleep 0.1
done

# Start publisher
ffmpeg -hide_banner -loglevel error -re -stream_loop -1 \
    -i "./assets/nonchalant-test.mp4" \
    -c copy -f flv "rtmp://127.0.0.1:${RTMP_PORT}/live/prof" \
    > /dev/null 2>&1 &
PUB_PID=$!
trap 'kill $PUB_PID $SERVER_PID 2>/dev/null || true; rm -f "$CFG"' EXIT

for _ in $(seq 1 100); do
    curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/streams" 2>/dev/null \
        | grep -q '"name":"prof"' && break
    sleep 0.1
done

# Spawn 1024 subscribers, give them time to connect, then capture profile.
URL="http://127.0.0.1:${HTTP_PORT}/live/prof.flv"
API="http://127.0.0.1:${HTTP_PORT}/api/streams"
LOADTEST_LABEL="profile-${LABEL}" ./bin/loadtest \
    -url "$URL" -api "$API" -n 1024 -d 35s \
    -out "$DATA_DIR/profile-loadtest-${LABEL}.csv" \
    > "$DATA_DIR/profile-loadtest-${LABEL}.txt" 2>&1 &
LP=$!

# Let the connect storm settle and clients reach steady state.
sleep 5

PROF="$DATA_DIR/cpu-${LABEL}.prof"
echo "==> capturing 30s CPU profile -> $PROF"
curl -fsS "http://127.0.0.1:${HTTP_PORT}/debug/pprof/profile?seconds=30" \
    -o "$PROF"

go tool pprof -top -cum -nodecount=30 "$PROF" \
    > "$DATA_DIR/cpu-${LABEL}.txt" 2>&1
echo "==> top-30 functions in $DATA_DIR/cpu-${LABEL}.txt"
head -45 "$DATA_DIR/cpu-${LABEL}.txt"

# Wait for loadtest to finish.
wait $LP 2>/dev/null || true

kill $PUB_PID $SERVER_PID 2>/dev/null || true
wait 2>/dev/null || true
echo "==> done"
