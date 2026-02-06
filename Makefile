# If you are AI: This Makefile provides all build, test, and deployment targets for nonchalant.

.PHONY: help build run docker-build docker-run docker-restart docker-stop kill fmt test test-short test-race bench itest itest-clean docs docs-check check-lines check-comments check

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
