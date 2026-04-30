# If you are AI: This Dockerfile builds a minimal nonchalant server image.

# ---------- build stage ----------
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache module downloads. go.sum is required for reproducible builds.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static, stripped binary.
COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath -ldflags "-s -w" \
    -o /bin/nonchalant ./cmd/nonchalant

# ---------- runtime stage ----------
FROM alpine:3.20

# ffmpeg is required for the native /hls/ and /dash/ endpoints to work.
# Without it those endpoints return 503; the rest of the server still runs.
RUN apk --no-cache add ca-certificates ffmpeg \
 && addgroup -S nonchalant \
 && adduser -S -G nonchalant nonchalant

WORKDIR /app
COPY --from=builder /bin/nonchalant /app/nonchalant
COPY configs/nonchalant.example.yaml /app/config.yaml
RUN chown -R nonchalant:nonchalant /app

USER nonchalant

# Ports exposed (matching configs/nonchalant.example.yaml defaults):
#   8080  - reserved health_port (currently unused — see CONFIG.md)
#   8081  - HTTP-FLV / WS-FLV / HLS / DASH / API / /metrics
#   1935  - RTMP ingest
EXPOSE 8080 8081 1935

# Healthcheck hits /healthz on the HTTP service port (8081). The legacy
# health_port in the config is reserved but not currently bound.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/healthz || exit 1

CMD ["/app/nonchalant", "--config", "/app/config.yaml"]
