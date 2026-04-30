#!/usr/bin/env bash
# If you are AI: This is a focused idle-CPU experiment for the WOP report.
# Start nonchalant, register a stream with a publisher BUT publish nothing,
# spawn N subscribers via loadtest pointed at that stream, sample CPU/RSS
# of the server for some seconds, then tear down. Repeats for each N.
#
# Usage: idle.sh <label>   # label = "wop" or "pre-wop"

set -euo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd)
cd "$REPO"

LABEL="${1:-idle-wop}"
HTTP_PORT=18091
RTMP_PORT=11936
HEALTH_PORT=18090
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

./bin/nonchalant --config "$CFG" > "$DATA_DIR/idle-server-${LABEL}.log" 2>&1 &
SERVER_PID=$!
trap 'kill $SERVER_PID 2>/dev/null || true; rm -f "$CFG"' EXIT
for _ in $(seq 1 50); do
    curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" >/dev/null 2>&1 && break
    sleep 0.1
done

# Bring up a publisher so the stream is registered, then immediately stop it.
# Subscribers connect to a stream with HasPublisher=true but no live frames
# arriving (we'll use a trickle: ffmpeg paused indefinitely after first frame).
# Easiest: ffmpeg sends a black frame at 1 fps. Subscribers will mostly idle.
ffmpeg -hide_banner -loglevel error -re \
    -f lavfi -i "color=color=black:size=160x120:rate=1" \
    -c:v libx264 -preset ultrafast -tune zerolatency -g 30 \
    -f flv "rtmp://127.0.0.1:${RTMP_PORT}/live/idle" \
    > /dev/null 2>&1 &
PUB_PID=$!
trap 'kill $PUB_PID $SERVER_PID 2>/dev/null || true; rm -f "$CFG"' EXIT

for _ in $(seq 1 100); do
    BODY=$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/streams" 2>/dev/null || echo "")
    echo "$BODY" | grep -q '"name":"idle"' && \
        echo "$BODY" | grep -q '"has_publisher":true' && break
    sleep 0.1
done

OUT="$DATA_DIR/idle-cpu-${LABEL}.csv"
echo "n,cpu_avg,cpu_max,goroutines,rss_mb" > "$OUT"

URL="http://127.0.0.1:${HTTP_PORT}/live/idle.flv"
for N in 0 64 256 1024 4096; do
    echo "==> idle test N=$N"
    if [ "$N" -gt 0 ]; then
        ./bin/loadtest -url "$URL" -n "$N" -d 12s \
            > "$DATA_DIR/idle-loadtest-${LABEL}-n${N}.txt" 2>&1 &
        LP=$!
        # Let connections establish.
        sleep 2
    fi
    # Sample 8 seconds of steady-state.
    cpu_sum=0 cpu_max=0 cnt=0
    last_g=0 rss_mb=0
    for _ in $(seq 1 8); do
        L=$(ps -o pcpu=,rss= -p "$SERVER_PID" 2>/dev/null || echo "0 0")
        c=$(echo "$L" | awk '{print $1}')
        r=$(echo "$L" | awk '{print $2}')
        cpu_sum=$(awk -v a=$cpu_sum -v b=$c 'BEGIN{print a+b}')
        # update max
        cpu_max=$(awk -v a=$cpu_max -v b=$c 'BEGIN{print (b>a)?b:a}')
        rss_mb=$(awk -v r=$r 'BEGIN{printf "%.1f", r/1024}')
        cnt=$((cnt+1))
        sleep 1
    done
    avg=$(awk -v s=$cpu_sum -v c=$cnt 'BEGIN{printf "%.2f", s/c}')
    g=$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/metrics" 2>/dev/null | awk '/^go_goroutines /{print $2}')
    [ -z "$g" ] && g=0
    echo "$N,$avg,$cpu_max,$g,$rss_mb" >> "$OUT"
    echo "  N=$N avg_cpu=$avg max_cpu=$cpu_max goroutines=$g rss_mb=$rss_mb"
    if [ "$N" -gt 0 ]; then
        wait $LP 2>/dev/null || true
    fi
done

kill $PUB_PID $SERVER_PID 2>/dev/null || true
wait 2>/dev/null || true
echo "==> wrote $OUT"
