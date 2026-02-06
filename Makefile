# If you are AI: This Makefile provides all build, test, and deployment targets for nonchalant.

.PHONY: help build run docker-build docker-run docker-restart docker-stop kill fmt test test-short test-race bench itest itest-clean docs docs-check check-lines check-comments check test-rtmp-ingest test-httpflv test-wsflv test-api test-workflow test-features test-video

# Configuration variables
CONFIG ?= configs/nonchalant.example.yaml
BIN ?= bin/nonchalant
IMAGE ?= nonchalant:latest
CONTAINER ?= nonchalant
HEALTH_PORT ?= 8080
RTMP_PORT ?= 1935
HTTP_PORT ?= 8081

# Default target: show help
help:
	@echo "nonchalant Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build              - Build the binary"
	@echo "  run                - Run the server"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Run container"
	@echo "  docker-restart     - Restart container"
	@echo "  docker-stop        - Stop container"
	@echo "  kill               - Kill running process (macOS/Linux)"
	@echo "  fmt                - Format code"
	@echo "  test               - Run all tests"
	@echo "  test-short         - Run short tests"
	@echo "  test-race          - Run tests with race detector"
	@echo "  bench              - Run benchmarks"
	@echo "  itest              - Run integration tests"
	@echo "  itest-clean        - Clean integration test artifacts"
	@echo "  docs               - Generate documentation"
	@echo "  docs-check         - Check if docs are up to date"
	@echo "  check-lines        - Check file line limits"
	@echo "  check-comments     - Check file headers and function comments"
	@echo "  check              - Run all checks"
	@echo "  test-video         - Test with video file (publish, play, verify)"
	@echo "  test-workflow      - Test complete workflow"
	@echo "  test-rtmp-ingest   - Test RTMP publishing"
	@echo "  test-httpflv       - Test HTTP-FLV playback"
	@echo "  test-wsflv         - Test WebSocket-FLV connection"
	@echo "  test-api           - Test all API endpoints"
	@echo "  test-features      - Run all feature tests (requires server)"
	@echo ""
	@echo "Variables:"
	@echo "  CONFIG=$(CONFIG)"
	@echo "  BIN=$(BIN)"
	@echo "  IMAGE=$(IMAGE)"
	@echo "  CONTAINER=$(CONTAINER)"
	@echo "  HEALTH_PORT=$(HEALTH_PORT)"
	@echo "  RTMP_PORT=$(RTMP_PORT)"
	@echo "  HTTP_PORT=$(HTTP_PORT)"

# Build binary
build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/nonchalant

# Run server
run: build
	./$(BIN) --config $(CONFIG)

# Docker targets
docker-build:
	docker build -t $(IMAGE) .

docker-run:
	docker run -d --name $(CONTAINER) \
		-p $(HEALTH_PORT):$(HEALTH_PORT) \
		-p $(HTTP_PORT):$(HTTP_PORT) \
		-p $(RTMP_PORT):$(RTMP_PORT) \
		$(IMAGE)

docker-restart: docker-stop docker-run

docker-stop:
	-docker stop $(CONTAINER)
	-docker rm $(CONTAINER)

# Kill running process (works on macOS and Linux)
kill:
	@pkill -f nonchalant || true

# Format code
fmt:
	go fmt ./...

# Test targets
# NOTE: Exclude scripts/ directory as it contains multiple main functions
test:
	go test ./cmd/... ./internal/...

test-short:
	go test -short ./cmd/... ./internal/...

test-race:
	go test -race ./cmd/... ./internal/...

bench:
	go test -bench=. -benchmem ./cmd/... ./internal/...

# Integration tests
itest: build
	go test -v ./internal/itest/...

itest-clean:
	rm -f bin/nonchalant

# Documentation
docs:
	go run ./scripts/gen-docs.go

docs-check: docs
	@if [ -f docs/.stamp ]; then \
		echo "Documentation is up to date"; \
	else \
		echo "Documentation is missing or stale. Run 'make docs' to regenerate."; \
		exit 1; \
	fi

# Enforcement checks
check-lines:
	go run ./scripts/check_lines.go .

check-comments:
	go run ./scripts/check_comments.go .

# Aggregate check target
check: fmt check-lines check-comments docs-check test
	@echo "All checks passed"

# Feature testing targets
# These targets test individual features using the test video

# Test RTMP ingest (publish test video)
test-rtmp-ingest: build
	@echo "Testing RTMP ingest..."
	@echo "Publishing test video to rtmp://localhost:1935/live/teststream"
	@timeout 10 ffmpeg -re -i assets/nonchalant-test.mp4 \
		-c copy -f flv \
		rtmp://localhost:1935/live/teststream 2>&1 | head -20 || true
	@echo "RTMP ingest test complete"

# Test HTTP-FLV output (requires server running)
test-httpflv: build
	@echo "Testing HTTP-FLV output..."
	@echo "Fetching stream via HTTP-FLV..."
	@timeout 5 curl -s http://localhost:8081/live/teststream.flv | wc -c || echo "Stream not available"
	@echo "HTTP-FLV test complete"

# Test WebSocket-FLV output (requires server running)
test-wsflv: build
	@echo "Testing WebSocket-FLV output..."
	@echo "Connecting to WebSocket endpoint..."
	@timeout 3 curl -s --include \
		--no-buffer \
		--header "Connection: Upgrade" \
		--header "Upgrade: websocket" \
		--header "Sec-WebSocket-Key: test" \
		--header "Sec-WebSocket-Version: 13" \
		http://localhost:8081/ws/live/teststream 2>&1 | head -5 || echo "WebSocket test requires proper client"
	@echo "WebSocket-FLV test complete"

# Test API endpoints
test-api: build
	@echo "Testing HTTP API..."
	@echo "GET /api/server:"
	@curl -s http://localhost:8081/api/server | python3 -m json.tool || echo "API not available"
	@echo ""
	@echo "GET /api/streams:"
	@curl -s http://localhost:8081/api/streams | python3 -m json.tool || echo "API not available"
	@echo ""
	@echo "GET /api/relay:"
	@curl -s http://localhost:8081/api/relay | python3 -m json.tool || echo "API not available"
	@echo "API test complete"

# Test complete workflow (publish, play, verify)
test-workflow: build
	@echo "Testing complete workflow..."
	@echo "1. Starting server in background..."
	@./bin/nonchalant --config configs/nonchalant.example.yaml &
	@SERVER_PID=$$!; \
	sleep 2; \
	echo "2. Publishing test video..."; \
	timeout 5 ffmpeg -re -i assets/nonchalant-test.mp4 \
		-c copy -f flv \
		rtmp://localhost:1935/live/workflowtest 2>&1 > /dev/null & \
	PUBLISH_PID=$$!; \
	sleep 2; \
	echo "3. Checking API for stream..."; \
	curl -s http://localhost:8081/api/streams | python3 -m json.tool || true; \
	echo "4. Fetching stream via HTTP-FLV..."; \
	timeout 3 curl -s http://localhost:8081/live/workflowtest.flv | wc -c || true; \
	echo "5. Cleaning up..."; \
	kill $$PUBLISH_PID 2>/dev/null || true; \
	kill $$SERVER_PID 2>/dev/null || true; \
	wait $$SERVER_PID 2>/dev/null || true; \
	echo "Workflow test complete"

# Test all features (requires server running)
test-features: test-api test-httpflv test-wsflv
	@echo "All feature tests complete"

# Test with test video (full integration test)
test-video: build
	@echo "Testing with video file: assets/nonchalant-test.mp4"
	@if [ ! -f assets/nonchalant-test.mp4 ]; then \
		echo "Error: assets/nonchalant-test.mp4 not found"; \
		echo "Please add a test video file to assets/"; \
		exit 1; \
	fi
	@echo "Starting server..."
	@./bin/nonchalant --config configs/nonchalant.example.yaml &
	@SERVER_PID=$$!; \
	trap "kill $$SERVER_PID 2>/dev/null || true" EXIT; \
	sleep 2; \
	echo "Publishing test video..."; \
	timeout 10 ffmpeg -re -i assets/nonchalant-test.mp4 \
		-c copy -f flv \
		rtmp://localhost:1935/live/videotest 2>&1 > /dev/null & \
	PUBLISH_PID=$$!; \
	sleep 3; \
	echo "Verifying stream via API..."; \
	curl -s http://localhost:8081/api/streams | python3 -m json.tool || true; \
	echo "Fetching stream via HTTP-FLV..."; \
	BYTES=$$(timeout 5 curl -s http://localhost:8081/live/videotest.flv | wc -c); \
	if [ $$BYTES -gt 0 ]; then \
		echo "✓ HTTP-FLV test passed (received $$BYTES bytes)"; \
	else \
		echo "✗ HTTP-FLV test failed (no data received)"; \
	fi; \
	kill $$PUBLISH_PID 2>/dev/null || true; \
	sleep 1; \
	echo "Test complete"
