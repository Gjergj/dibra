#!/usr/bin/env bash
# Build and run dibra-deploy in the Linux integration container while polling
# an API served by the Docker host.

set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPOSITORY_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
COMPOSE_FILE="$REPOSITORY_ROOT/test/docker-compose.yaml"
DEPLOY_ENDPOINT=${DEPLOY_ENDPOINT:-http://host.docker.internal:8080/gettasks}
DIBRA_VERSION=${DIBRA_VERSION:-dev}
DIBRA_COMMIT=${DIBRA_COMMIT:-}
DIBRA_BUILD_DATE=${DIBRA_BUILD_DATE:-}
TEMPORARY_DIR=""

info() {
    printf '%s\n' "[dibra-deploy-docker] $*"
}

error() {
    printf '%s\n' "[dibra-deploy-docker] error: $*" >&2
    exit 1
}

cleanup() {
    if [[ -n "$TEMPORARY_DIR" && -d "$TEMPORARY_DIR" ]]; then
        rm -rf -- "$TEMPORARY_DIR"
    fi
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || error "required command not found: $1"
}

compose() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

wait_for_container() {
    local attempt

    for ((attempt = 1; attempt <= 45; attempt++)); do
        if compose exec -T testhost systemctl is-active --quiet ssh >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done
    error "test container did not become ready within 45 seconds"
}

verify_host_gateway() {
    if ! compose exec -T testhost getent hosts host.docker.internal >/dev/null 2>&1; then
        error "host.docker.internal does not resolve inside the test container"
    fi
}

container_go_arch() {
    local architecture

    architecture=$(compose exec -T testhost uname -m | tr -d '\r\n')
    case "$architecture" in
        x86_64)
            printf '%s\n' amd64
            ;;
        aarch64|arm64)
            printf '%s\n' arm64
            ;;
        *)
            error "unsupported container architecture: $architecture"
            ;;
    esac
}

resolve_build_metadata() {
    if [[ -z "$DIBRA_COMMIT" ]]; then
        DIBRA_COMMIT=$(git -C "$REPOSITORY_ROOT" rev-parse --short HEAD 2>/dev/null || printf '%s\n' none)
    fi
    if [[ -z "$DIBRA_BUILD_DATE" ]]; then
        DIBRA_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    fi
}

build_linux_binary() {
    local go_arch=$1
    local package_path=$2
    local output_path=$3
    local ldflags

    ldflags="-s -w"
    ldflags+=" -X github.com/gjergjiramku/dibra/internal/version.Version=$DIBRA_VERSION"
    ldflags+=" -X github.com/gjergjiramku/dibra/internal/version.Commit=$DIBRA_COMMIT"
    ldflags+=" -X github.com/gjergjiramku/dibra/internal/version.Date=$DIBRA_BUILD_DATE"

    (
        cd "$REPOSITORY_ROOT"
        GOOS=linux GOARCH="$go_arch" CGO_ENABLED=0 \
            go build -ldflags "$ldflags" -o "$output_path" "$package_path"
    )
}

main() {
    local go_arch

    require_command docker
    require_command go
    require_command git
    require_command mktemp
    docker compose version >/dev/null 2>&1 || error "Docker Compose is not available"

    resolve_build_metadata
    TEMPORARY_DIR=$(mktemp -d "${TMPDIR:-/tmp}/dibra-deploy-docker.XXXXXX")
    trap cleanup EXIT

    info "starting the Linux test container"
    compose up -d --build --no-deps testhost
    wait_for_container
    verify_host_gateway

    go_arch=$(container_go_arch)
    info "building Linux/$go_arch binaries"
    build_linux_binary "$go_arch" ./cmd/dibra-deploy "$TEMPORARY_DIR/dibra-deploy"
    build_linux_binary "$go_arch" ./cmd/agent "$TEMPORARY_DIR/dibra-agent"

    info "installing development binaries in the container"
    compose cp "$TEMPORARY_DIR/dibra-deploy" testhost:/usr/local/bin/dibra-deploy
    compose cp "$TEMPORARY_DIR/dibra-agent" testhost:/usr/local/bin/dibra-agent
    compose exec -T testhost chmod 0755 /usr/local/bin/dibra-deploy /usr/local/bin/dibra-agent

    info "polling $DEPLOY_ENDPOINT; press Ctrl-C to stop dibra-deploy"
    info "the container will remain running for inspection"
    compose exec testhost /usr/local/bin/dibra-deploy \
        --agent-path /usr/local/bin/dibra-agent \
        --endpoint "$DEPLOY_ENDPOINT" \
        --force-agent-upload \
        --verbose
}

main "$@"
