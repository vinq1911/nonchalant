#!/usr/bin/env bash
# If you are AI: This script orchestrates the nonchalant performance smoke test.
# It launches the server, an ffmpeg publisher looping the test asset, sweeps a
# matrix of subscriber counts via cmd/loadtest, samples server CPU/RSS once
# per second, and emits CSV files into reports/data/ that scripts/perf/build_report.py
# turns into reports/PERF-WOP.pdf.

set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
cd "$REPO"

HTTP_PORT=18081
RTMP_PORT=11935
HEALTH_PORT=18080  # unused but required by config validator
DATA_DIR="reports/data"
RUN_LABEL="${1:-wop}"  # usage: run.sh <label>   (e.g. wop / pre-wop)
mkdir -p "$DATA_DIR"

# ----- prereqs -----
command -v ffmpeg >/dev/null || { echo "ffmpeg required"; exit 1; }
[[ -f assets/nonchalant-test.mp4 ]] || { echo "missing test video"; exit 1; }

# ----- build -----
echo "==> building binaries"
go build -o bin/nonchalant ./cmd/nonchalant
go build -o bin/loadtest   ./cmd/loadtest

# ----- start server -----
CFG=$(mktemp)
trap 'rm -f "$CFG"' EXIT
cat > "$CFG" <<EOF
server:
  health_port: $HEALTH_PORT
  http_port:   $HTTP_PORT
  rtmp_port:   $RTMP_PORT
EOF

echo "==> starting nonchalant"
./bin/nonchalant --config "$CFG" > "$DATA_DIR/server-${RUN_LABEL}.log" 2>&1 &
SERVER_PID=$!
trap 'kill $SERVER_PID 2>/dev/null || true; rm -f "$CFG"' EXIT

# wait for /healthz
for _ in $(seq 1 50); do
    if curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" > /dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

# ----- background CPU/RSS sampler (1 Hz) -----
SAMPLE_CSV="$DATA_DIR/sampler-${RUN_LABEL}.csv"
echo "ts,phase,cpu_percent,rss_mb,goroutines" > "$SAMPLE_CSV"
PHASE_FILE=$(mktemp); echo "idle-no-pub" > "$PHASE_FILE"
trap 'kill $SERVER_PID 2>/dev/null || true; rm -f "$CFG" "$PHASE_FILE"' EXIT

(
  while kill -0 "$SERVER_PID" 2>/dev/null; do
    PHASE=$(cat "$PHASE_FILE" 2>/dev/null || echo "?")
    LINE=$(ps -o pcpu=,rss= -p "$SERVER_PID" 2>/dev/null || echo "0 0")
    CPU=$(echo "$LINE" | awk '{print $1}')
    RSSKB=$(echo "$LINE" | awk '{print $2}')
    RSSMB=$(awk -v r=$RSSKB 'BEGIN{printf "%.1f", r/1024}')
    GOROUTINES=$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/metrics" 2>/dev/null \
        | awk '/^go_goroutines /{print $2}')
    [ -z "$GOROUTINES" ] && GOROUTINES=0
    printf "%s,%s,%s,%s,%s\n" "$(date +%s)" "$PHASE" "$CPU" "$RSSMB" "$GOROUTINES" \
        >> "$SAMPLE_CSV"
    sleep 1
  done
) &
SAMPLER_PID=$!
trap 'kill $SERVER_PID $SAMPLER_PID 2>/dev/null || true; rm -f "$CFG" "$PHASE_FILE"' EXIT

set_phase() {
    echo "$1" > "$PHASE_FILE"
    echo ""
    echo "==> phase: $1"
}

# ----- phase 1: idle (no publisher, no subscribers) -----
set_phase "idle-no-pub"
sleep 5

# ----- start publisher -----
echo "==> starting ffmpeg publisher (looping ./assets/nonchalant-test.mp4)"
ffmpeg -hide_banner -loglevel error -re -stream_loop -1 \
    -i "./assets/nonchalant-test.mp4" \
    -c copy -f flv "rtmp://127.0.0.1:${RTMP_PORT}/live/loadtest" \
    > "$DATA_DIR/publisher-${RUN_LABEL}.log" 2>&1 &
PUB_PID=$!
trap 'kill $PUB_PID $SERVER_PID $SAMPLER_PID 2>/dev/null || true; rm -f "$CFG" "$PHASE_FILE"' EXIT

# wait for stream to register
for _ in $(seq 1 100); do
    BODY=$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/streams" 2>/dev/null || echo "")
    if echo "$BODY" | grep -q '"name":"loadtest"' && \
       echo "$BODY" | grep -q '"has_publisher":true'; then
        break
    fi
    sleep 0.1
done

# ----- phase 2: idle subs, no consumers (just publish) -----
set_phase "publish-no-subs"
sleep 5

# ----- phase 3: sweep subscriber counts -----
URL="http://127.0.0.1:${HTTP_PORT}/live/loadtest.flv"
API="http://127.0.0.1:${HTTP_PORT}/api/streams"
DURATION=10
SUMMARY="$DATA_DIR/summary-${RUN_LABEL}.csv"
rm -f "$SUMMARY"
for N in 1 4 16 64 256 1024 4096; do
    set_phase "subs=${N}"
    LOADTEST_LABEL="${RUN_LABEL}-n${N}" ./bin/loadtest \
        -url "$URL" -api "$API" -n "$N" -d "${DURATION}s" \
        -out "$DATA_DIR/loadtest-${RUN_LABEL}-n${N}.csv" \
        -summary "$SUMMARY" \
        > "$DATA_DIR/loadtest-${RUN_LABEL}-n${N}.txt" 2>&1
    sleep 2  # cooldown
done

set_phase "cooldown"
sleep 3

echo ""
echo "==> done — data in $DATA_DIR/"
kill $PUB_PID 2>/dev/null || true
sleep 1
kill $SERVER_PID 2>/dev/null || true
kill $SAMPLER_PID 2>/dev/null || true
wait 2>/dev/null || true
