.PHONY: build build-dev test test-integration test-integration-up test-integration-down clean snapshot release-dry install

# Version info for local builds
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/gjergjiramku/goansible/internal/version.Version=$(VERSION) \
	-X github.com/gjergjiramku/goansible/internal/version.Commit=$(COMMIT) \
	-X github.com/gjergjiramku/goansible/internal/version.Date=$(DATE)

# Basic build (no version injection)
build:
	go build ./...

# Development build with version info
build-dev:
	go build -ldflags "$(LDFLAGS)" -o bin/goansible ./cmd/controller
	go build -ldflags "$(LDFLAGS)" -o bin/goansible-agent ./cmd/agent

# Install to $GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/controller
	go install -ldflags "$(LDFLAGS)" ./cmd/agent

test:
	go test ./... -v

# Start test container
test-integration-up:
	docker compose -f test/docker-compose.yaml up -d --build
	@echo "Waiting for SSH to be ready..."
	@sleep 3

# Stop test container
test-integration-down:
	docker compose -f test/docker-compose.yaml down -v

# Run integration tests (requires container to be running)
test-integration: test-integration-up
	@echo "Running integration tests..."
	go test -tags=integration -v -timeout 20m ./test/integration/... || (make test-integration-down && exit 1)
	make test-integration-down

# Run integration tests without managing container
test-integration-only:
	go test -tags=integration -v -timeout 20m ./test/integration/...

# Clean up
clean:
	docker compose -f test/docker-compose.yaml down -v
	rm -f /tmp/goansible-test-agent
	rm -rf bin/ dist/
	go clean

# GoReleaser: build snapshot (no publish)
snapshot:
	goreleaser build --snapshot --clean

# GoReleaser: dry run release (no publish)
release-dry:
	goreleaser release --snapshot --skip=publish --clean

# Check GoReleaser config
release-check:
	goreleaser check

# go run ./cmd/controller -config playbook.yaml --force-agent-upload 2>&1 | head -10  
# go test -tags=integration -v -timeout 10m -count=1 ./test/integration/... -run "TestPlaybook_GitCloneBareIdempotent" 2>&1