# If you are AI: This Dockerfile builds a minimal nonchalant server image.

FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN go build -o /bin/nonchalant ./cmd/nonchalant

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /bin/nonchalant /app/nonchalant
COPY configs/nonchalant.example.yaml /app/config.yaml

EXPOSE 8080 8081 1935

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["/app/nonchalant", "--config", "/app/config.yaml"]
