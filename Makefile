.PHONY: build build-dev test test lint test-integration test-integration-up test-integration-down clean snapshot release-dry release-minor release-patch install

# Version info for local builds
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/gjergjiramku/dibra/internal/version.Version=$(VERSION) \
	-X github.com/gjergjiramku/dibra/internal/version.Commit=$(COMMIT) \
	-X github.com/gjergjiramku/dibra/internal/version.Date=$(DATE)

# Basic build (no version injection)
build:
	go build ./...

# Development build with version info
build-dev:
	go build -ldflags "$(LDFLAGS)" -o bin/dibra ./cmd/controller
	go build -ldflags "$(LDFLAGS)" -o bin/dibra-agent ./cmd/agent
	go build -ldflags "$(LDFLAGS)" -o bin/dibra-deploy ./cmd/dibra-deploy

build-dev-cross-agent-linux:
	env GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/dibra ./cmd/controller
	env GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/dibra-agent ./cmd/agent
	env GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/dibra-deploy ./cmd/dibra-deploy

# Install to $GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/controller
	go install -ldflags "$(LDFLAGS)" ./cmd/agent
	go install -ldflags "$(LDFLAGS)" ./cmd/dibra-deploy

test:
	go test -v -race ./...

# Run golangci-lint (same as CI)
lint:
	go tool golangci-lint run ./...

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
	rm -f /tmp/dibra-test-agent
	rm -rf bin/ dist/
	go clean

# GoReleaser: build snapshot (no publish)
snapshot:
	goreleaser build --snapshot --clean

# GoReleaser: dry run release (no publish)
release-dry:
	goreleaser release --snapshot --skip=publish --clean

# Create and push the next minor version tag (for example, v0.0.28 -> v0.1.0)
# release-minor:
# 	@latest=$$(git tag --list 'v*' --sort=-v:refname | head -n 1); \
# 	if [ -z "$$latest" ]; then latest=v0.0.0; fi; \
# 	version=$${latest#v}; \
# 	IFS=.; set -- $$version; \
# 	next="v$$1.$$(( $$2 + 1 )).0"; \
# 	echo "Creating and pushing $$next"; \
# 	git tag "$$next"; \
# 	git push origin "$$next"

# Create and push the next patch version tag (for example, v0.0.28 -> v0.0.29)
release-patch:
	@latest=$$(git tag --list 'v*' --sort=-v:refname | head -n 1); \
	if [ -z "$$latest" ]; then latest=v0.0.0; fi; \
	version=$${latest#v}; \
	IFS=.; set -- $$version; \
	next="v$$1.$$2.$$(( $$3 + 1 ))"; \
	echo "Creating and pushing $$next"; \
	git tag "$$next"; \
	git push origin "$$next"

# Check GoReleaser config
release-check:
	goreleaser check

# go run ./cmd/controller -config playbook.yaml --force-agent-upload 2>&1 | head -10  
# go test -tags=integration -v -timeout 10m -count=1 ./test/integration/... -run "TestPlaybook_GitCloneBareIdempotent" 2>&1
