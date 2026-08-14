.PHONY: build build-dev test test lint test-integration test-integration-up test-integration-down test-docker-container-integration test-docker-container-integration-only test-docker-image-integration test-docker-image-integration-only test-docker-resource-integration test-docker-resource-integration-only test-docker-compose-integration test-docker-compose-integration-only test-docker-swarm-integration test-docker-swarm-integration-only test-deploy-integration test-deploy-integration-only test-deploy-integration-up test-deploy-integration-down run-deploy-docker-host run-deploy-docker-host-down test-deploy-docker-host test-deploy-docker-host-down clean snapshot release-dry release-minor release-patch install

# Version info for local builds
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/gjergjiramku/dibra/internal/version.Version=$(VERSION) \
	-X github.com/gjergjiramku/dibra/internal/version.Commit=$(COMMIT) \
	-X github.com/gjergjiramku/dibra/internal/version.Date=$(DATE)

# Endpoint used by the Docker-host debugging target.
DEPLOY_ENDPOINT ?= http://host.docker.internal:8080/gettasks

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
	docker compose -f test/docker-compose.yaml up -d --build --wait --wait-timeout 180

# Stop test container
test-integration-down:
	docker compose -f test/docker-compose.yaml down -v

# Run integration tests (requires container to be running)
test-integration: test-integration-up
	@echo "Running integration tests..."
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration/... || (make test-integration-down && exit 1)
	go test -tags=integration -count=1 -v -timeout 10m ./test/deploy_integration/... || (make test-integration-down && exit 1)
	make test-integration-down

# Run integration tests without managing container
test-integration-only:
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration/...
	go test -tags=integration -count=1 -v -timeout 10m ./test/deploy_integration/...

# Explicit certification-lane test names. Add new *_parity tests here; do not
# use prefix matches, which pull in unrelated TestPlaybook_DockerContainer* names.
DOCKER_CONTAINER_INTEGRATION_RUN := ^(TestPlaybook_DockerContainerLifecycle|TestPlaybook_DockerContainerEntrypoint|TestPlaybook_DockerContainerLogging|TestPlaybook_DockerContainerCapabilities|TestPlaybook_DockerContainerHealthcheck|TestPlaybook_DockerContainerInit|TestPlaybook_DockerContainerTmpfs|TestPlaybook_DockerContainerShmSize|TestPlaybook_DockerContainerResources|TestPlaybook_DockerContainerUlimits|TestPlaybook_DockerContainerSysctls|TestPlaybook_DockerContainerSecurityOpt|TestPlaybook_DockerContainerNetworks|TestPlaybook_DockerContainerRecreatePolicy|TestPlaybook_DockerContainerPullPolicy|TestPlaybook_DockerContainerPortRangeExpansion|TestPlaybook_DockerContainerExec|TestPlaybook_DockerContainerExecParity|TestPlaybook_DockerContainerCopyInto|TestPlaybook_DockerContainerCopyIntoParity|TestPlaybook_DockerContainerInfo|TestPlaybook_DockerContainerInfoParity|TestPlaybook_DockerContainerCanonicalParity|TestPlaybook_DockerContainerPinnedUpstreamOptions|TestPlaybook_DockerContainerHealthyPausedAndCleanup|TestPlaybook_DockerContainerPinnedRegressionMatrix|TestPlaybook_DockerContainerStates)
DOCKER_IMAGE_INTEGRATION_RUN := ^(TestPlaybook_DockerImageLifecycle|TestPlaybook_DockerImageParity|TestPlaybook_DockerImagePullPolicies|TestPlaybook_DockerImageTagIdempotency|TestPlaybook_DockerImageStreamErrors|TestPlaybook_DockerImageForceRemove|TestPlaybook_DockerImageBackwardCompat|TestPlaybook_DockerImageBuild|TestPlaybook_DockerImageBuildParity|TestPlaybook_DockerImageExport|TestPlaybook_DockerImageExportParity|TestPlaybook_DockerImageInfoParity|TestPlaybook_DockerImageLoad|TestPlaybook_DockerImageLoadParity|TestPlaybook_DockerImagePullParity|TestPlaybook_DockerImagePushParity|TestPlaybook_DockerImageRemoveParity|TestPlaybook_DockerImageTagParity)

# Run the pinned Docker Engine 29.7.2 docker_container certification lane.
test-docker-container-integration: test-integration-up
	@echo "Running docker_container integration tests..."
	go test -tags=integration -count=1 -v -timeout 30m ./test/integration -run '$(DOCKER_CONTAINER_INTEGRATION_RUN)$$' || (make test-integration-down && exit 1)
	make test-integration-down

# Run the certification lane against an already-running integration host.
test-docker-container-integration-only:
	go test -tags=integration -count=1 -v -timeout 30m ./test/integration -run '$(DOCKER_CONTAINER_INTEGRATION_RUN)$$'

# Run the pinned Docker Engine 29.7.2 image-family certification lane.
test-docker-image-integration: test-integration-up
	@echo "Running Docker image-family integration tests..."
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '$(DOCKER_IMAGE_INTEGRATION_RUN)$$' || (make test-integration-down && exit 1)
	make test-integration-down

# Run the image-family lane against an already-running integration host.
test-docker-image-integration-only:
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '$(DOCKER_IMAGE_INTEGRATION_RUN)$$'

# Run the Engine 29.7.2 resource/info certification lane (network, volume, login, prune, host/context/plugin/facts).
test-docker-resource-integration: test-integration-up
	@echo "Running Docker resource-family integration tests..."
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerNetwork|TestPlaybook_DockerVolume|TestPlaybook_DockerLogin|TestPlaybook_DockerPrune|TestPlaybook_DockerImagePrune|TestPlaybook_DockerHostInfo|TestPlaybook_DockerContextInfo|TestPlaybook_DockerPlugin|TestPlaybook_CurrentContainerFacts|TestPlaybook_DockerTLSConnectionParity|TestDockerConnectionEnvironmentFallbackAndArgumentPrecedence)' || (make test-integration-down && exit 1)
	make test-integration-down

# Run that lane with the integration container already running.
test-docker-resource-integration-only:
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerNetwork|TestPlaybook_DockerVolume|TestPlaybook_DockerLogin|TestPlaybook_DockerPrune|TestPlaybook_DockerImagePrune|TestPlaybook_DockerHostInfo|TestPlaybook_DockerContextInfo|TestPlaybook_DockerPlugin|TestPlaybook_CurrentContainerFacts|TestPlaybook_DockerTLSConnectionParity|TestDockerConnectionEnvironmentFallbackAndArgumentPrecedence)'

# Run the pinned Compose 5.4.0 docker_compose_v2 certification lane.
test-docker-compose-integration: test-integration-up
	@echo "Running docker_compose_v2 integration tests..."
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerComposeLifecycle|TestPlaybook_DockerComposeV2Parity|TestPlaybook_DockerComposeV2Examples|TestPlaybook_DockerComposeV2ExecParity|TestPlaybook_DockerComposeV2PullParity|TestPlaybook_DockerComposeV2RunParity)$$' || (make test-integration-down && exit 1)
	make test-integration-down

test-docker-compose-integration-only:
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerComposeLifecycle|TestPlaybook_DockerComposeV2Parity|TestPlaybook_DockerComposeV2Examples|TestPlaybook_DockerComposeV2ExecParity|TestPlaybook_DockerComposeV2PullParity|TestPlaybook_DockerComposeV2RunParity)$$'

# Run the Engine 29.7.2 Swarm node/info certification lane.
test-docker-swarm-integration: test-integration-up
	@echo "Running Docker Swarm node/service/config-family integration tests..."
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerSwarmParity|TestPlaybook_DockerSwarmInfoParity|TestPlaybook_DockerSwarmLifecycle|TestPlaybook_DockerSwarmService|TestPlaybook_DockerSwarmServiceParity|TestPlaybook_DockerSwarmServiceInfo|TestPlaybook_DockerSwarmServiceInfoParity|TestPlaybook_DockerNode|TestPlaybook_DockerNodeLabelsToRemove|TestPlaybook_DockerNodeInfo|TestPlaybook_DockerNodeParity|TestPlaybook_DockerNodeInfoParity|TestPlaybook_DockerConfigLifecycle|TestPlaybook_DockerConfigHashIdempotency|TestPlaybook_DockerConfigParity)$$' || (make test-integration-down && exit 1)
	make test-integration-down

test-docker-swarm-integration-only:
	go test -tags=integration -count=1 -v -timeout 60m ./test/integration -run '^(TestPlaybook_DockerSwarmParity|TestPlaybook_DockerSwarmInfoParity|TestPlaybook_DockerSwarmLifecycle|TestPlaybook_DockerSwarmService|TestPlaybook_DockerSwarmServiceParity|TestPlaybook_DockerSwarmServiceInfo|TestPlaybook_DockerSwarmServiceInfoParity|TestPlaybook_DockerNode|TestPlaybook_DockerNodeLabelsToRemove|TestPlaybook_DockerNodeInfo|TestPlaybook_DockerNodeParity|TestPlaybook_DockerNodeInfoParity|TestPlaybook_DockerConfigLifecycle|TestPlaybook_DockerConfigHashIdempotency|TestPlaybook_DockerConfigParity)$$'

# Start the shared Linux/systemd test container for dibra-deploy tests
test-deploy-integration-up: test-integration-up

# Stop the shared Linux/systemd test container
test-deploy-integration-down: test-integration-down

# Run only the dibra-deploy black-box integration suite
test-deploy-integration: test-deploy-integration-up
	@echo "Running dibra-deploy integration tests..."
	go test -tags=integration -count=1 -v -timeout 10m ./test/deploy_integration/... || (make test-deploy-integration-down && exit 1)
	make test-deploy-integration-down

# Run dibra-deploy integration tests with an already-running test container
test-deploy-integration-only:
	go test -tags=integration -count=1 -v -timeout 10m ./test/deploy_integration/...

# Run dibra-deploy in Linux against an API on the Docker host.
# make run-deploy-docker-host \
#   DEPLOY_ENDPOINT=http://host.docker.internal:9000/gettasks
run-deploy-docker-host:
	DEPLOY_ENDPOINT="$(DEPLOY_ENDPOINT)" DIBRA_VERSION="$(VERSION)" DIBRA_COMMIT="$(COMMIT)" DIBRA_BUILD_DATE="$(DATE)" ./scripts/run-deploy-docker-host.sh

# Stop and remove the container used by run-deploy-docker-host.
run-deploy-docker-host-down:
	docker compose -f test/docker-compose.yaml down -v

# Backward-compatible names for the original debugging targets.
test-deploy-docker-host: run-deploy-docker-host

test-deploy-docker-host-down: run-deploy-docker-host-down

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
