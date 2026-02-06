#!/bin/sh
# dibra installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install.sh | sh
# Or with a specific version: curl -fsSL ... | VERSION=v1.2.3 sh

set -e

REPO="Gjergj/dibra"
BINARY_NAME="dibra"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
}

error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1"
    exit 1
}

# Detect OS
detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        linux) OS="linux" ;;
        darwin) OS="darwin" ;;
        mingw*|msys*|cygwin*) OS="windows" ;;
        *) error "Unsupported operating system: $OS" ;;
    esac
    echo "$OS"
}

# Detect architecture
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac
    echo "$ARCH"
}

# Get latest version from GitHub API
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/'
}

# Download and verify checksum
download_and_verify() {
    local version=$1
    local os=$2
    local arch=$3
    local tmp_dir
    
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT
    
    # Determine file extension
    local ext="tar.gz"
    if [ "$os" = "windows" ]; then
        ext="zip"
    fi
    
    local filename="${BINARY_NAME}_${version}_${os}_${arch}.${ext}"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${filename}"
    local checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
    
    info "Downloading ${filename}..."
    if ! curl -fsSL -o "${tmp_dir}/${filename}" "$download_url"; then
        error "Failed to download ${filename}"
    fi
    
    info "Downloading checksums..."
    if ! curl -fsSL -o "${tmp_dir}/checksums.txt" "$checksums_url"; then
        error "Failed to download checksums"
    fi
    
    info "Verifying checksum..."
    cd "$tmp_dir"
    
    # Extract expected checksum for our file
    expected_checksum=$(grep "${filename}" checksums.txt | awk '{print $1}')
    if [ -z "$expected_checksum" ]; then
        error "Checksum not found for ${filename}"
    fi
    
    # Calculate actual checksum
    if command -v sha256sum >/dev/null 2>&1; then
        actual_checksum=$(sha256sum "${filename}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual_checksum=$(shasum -a 256 "${filename}" | awk '{print $1}')
    else
        warn "Neither sha256sum nor shasum found, skipping checksum verification"
        actual_checksum="$expected_checksum"
    fi
    
    if [ "$expected_checksum" != "$actual_checksum" ]; then
        error "Checksum verification failed!\nExpected: ${expected_checksum}\nActual: ${actual_checksum}"
    fi
    
    info "Checksum verified!"
    
    # Extract
    info "Extracting..."
    if [ "$ext" = "zip" ]; then
        unzip -q "${filename}"
    else
        tar -xzf "${filename}"
    fi
    
    # Install
    local binary="${BINARY_NAME}"
    if [ "$os" = "windows" ]; then
        binary="${BINARY_NAME}.exe"
    fi
    
    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "$binary" "${INSTALL_DIR}/${binary}"
    else
        info "Elevated permissions required to install to ${INSTALL_DIR}"
        sudo mv "$binary" "${INSTALL_DIR}/${binary}"
    fi
    
    chmod +x "${INSTALL_DIR}/${binary}"
    
    info "Successfully installed ${BINARY_NAME} to ${INSTALL_DIR}/${binary}"
}

main() {
    info "dibra Installer"
    
    OS=$(detect_os)
    ARCH=$(detect_arch)
    
    info "Detected OS: ${OS}, Architecture: ${ARCH}"
    
    # Get version
    if [ -n "${VERSION:-}" ]; then
        VERSION="${VERSION}"
        info "Using specified version: ${VERSION}"
    else
        info "Fetching latest version..."
        VERSION=$(get_latest_version)
        if [ -z "$VERSION" ]; then
            error "Failed to determine latest version"
        fi
        info "Latest version: ${VERSION}"
    fi
    
    # Remove 'v' prefix if present for filename
    VERSION_NUM="${VERSION#v}"
    
    download_and_verify "$VERSION_NUM" "$OS" "$ARCH"
    
    echo ""
    info "Installation complete!"
    info "Run 'dibra --version' to verify"
}

main "$@"
