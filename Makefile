.PHONY: build test test-integration test-integration-up test-integration-down clean

build:
	go build ./...

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
	go test -tags=integration -v -timeout 5m ./test/integration/... || (make test-integration-down && exit 1)
	make test-integration-down

# Run integration tests without managing container
test-integration-only:
	go test -tags=integration -v -timeout 5m ./test/integration/...

# Clean up
clean:
	docker compose -f test/docker-compose.yaml down -v
	rm -f /tmp/goansible-test-agent
	go clean
