#!/bin/sh
# Install dibra-deploy and its systemd unit from a GitHub release.
#
# Latest release with a project token:
#   curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh | \
#     sh -s -- 'PROJECT_JWT' --endpoint https://orchestrator.example/gettasks
#
# Pinned release, enabled immediately:
#   curl -fsSL https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh | \
#     sh -s -- 'PROJECT_JWT' --version v1.2.3 --enable

set -eu

REPO="${DIBRA_DEPLOY_REPO:-Gjergj/dibra}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SYSTEMD_UNIT_DIR="${SYSTEMD_UNIT_DIR:-/etc/systemd/system}"
CONFIG_DIR="${DIBRA_DEPLOY_CONFIG_DIR:-/etc/dibra-deploy}"
VERSION="${VERSION:-}"
TOKEN="${DIBRA_DEPLOY_TOKEN:-}"
ENDPOINT="${DIBRA_DEPLOY_ENDPOINT:-http://localhost:8080/gettasks}"
ENABLE_SERVICE=0
RELEASE_BASE_URL="${DIBRA_DEPLOY_RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download}"
LATEST_RELEASE_URL="${DIBRA_DEPLOY_LATEST_RELEASE_URL:-https://api.github.com/repos/${REPO}/releases/latest}"
SYSTEMCTL="${SYSTEMCTL:-systemctl}"
TMP_DIR=""

info() {
    printf '%s\n' "[dibra-deploy] $*"
}

error() {
    printf '%s\n' "[dibra-deploy] error: $*" >&2
    exit 1
}

usage() {
    cat <<'EOF'
Install dibra-deploy and its systemd service on Linux.

Usage:
  install-dibra-deploy.sh PROJECT_JWT [options]

Options:
  --token JWT           Project deployment token (alternative to positional JWT).
  --endpoint URL        Orchestrator gettasks URL.
  --version VERSION     Install a specific release (for example, v1.2.3).
  --install-dir DIR     Install the binary in DIR (default: /usr/local/bin).
  --unit-dir DIR        Install the unit in DIR (default: /etc/systemd/system).
  --config-dir DIR      Store the root-only environment file in DIR
                        (default: /etc/dibra-deploy).
  --enable              Enable and start the service after installation.
  -h, --help            Show this help.

The installer verifies the release archive against checksums.txt. It reloads
systemd, but does not enable or start dibra-deploy unless --enable is supplied.
EOF
}

cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf -- "$TMP_DIR"
    fi
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || error "required command not found: $1"
}

as_root() {
    if [ "$(id -u)" -eq 0 ] || [ "${DIBRA_DEPLOY_NO_SUDO:-0}" = "1" ]; then
        "$@"
    elif command -v sudo >/dev/null 2>&1; then
        sudo "$@"
    else
        error "root privileges are required; run as root or install sudo"
    fi
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --token)
                [ "$#" -ge 2 ] || error "--token requires a value"
                TOKEN=$2
                shift 2
                ;;
            --endpoint)
                [ "$#" -ge 2 ] || error "--endpoint requires a value"
                ENDPOINT=$2
                shift 2
                ;;
            --version)
                [ "$#" -ge 2 ] || error "--version requires a value"
                VERSION=$2
                shift 2
                ;;
            --install-dir)
                [ "$#" -ge 2 ] || error "--install-dir requires a value"
                INSTALL_DIR=$2
                shift 2
                ;;
            --unit-dir)
                [ "$#" -ge 2 ] || error "--unit-dir requires a value"
                SYSTEMD_UNIT_DIR=$2
                shift 2
                ;;
            --config-dir)
                [ "$#" -ge 2 ] || error "--config-dir requires a value"
                CONFIG_DIR=$2
                shift 2
                ;;
            --enable)
                ENABLE_SERVICE=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                if [ -z "$TOKEN" ]; then
                    TOKEN=$1
                    shift
                else
                    error "unknown option: $1 (use --help for usage)"
                fi
                ;;
        esac
    done
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)
            printf '%s\n' amd64
            ;;
        aarch64|arm64)
            printf '%s\n' arm64
            ;;
        *)
            error "unsupported Linux architecture: $(uname -m)"
            ;;
    esac
}

latest_version() {
    curl -fsSL "$LATEST_RELEASE_URL" |
        awk '
            /"tag_name"[[:space:]]*:/ {
                line = $0
                sub(/^.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", line)
                sub(/".*$/, "", line)
                print line
                exit
            }
        '
}

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        error "sha256sum or shasum is required to verify the release archive"
    fi
}

validate_paths() {
    case "$INSTALL_DIR" in
        /*) ;;
        *) error "--install-dir must be an absolute path" ;;
    esac
    case "$INSTALL_DIR" in
        *[[:space:]]*) error "--install-dir cannot contain whitespace" ;;
    esac
    case "$SYSTEMD_UNIT_DIR" in
        /*) ;;
        *) error "--unit-dir must be an absolute path" ;;
    esac
    case "$CONFIG_DIR" in
        /*) ;;
        *) error "--config-dir must be an absolute path" ;;
    esac
    case "$INSTALL_DIR$SYSTEMD_UNIT_DIR$CONFIG_DIR" in
        *[[:space:]]*) error "installation paths cannot contain whitespace" ;;
    esac
}

validate_credentials() {
    [ -n "$TOKEN" ] || error "PROJECT_JWT is required"
    [ "${#TOKEN}" -le 8192 ] || error "PROJECT_JWT is too long"
    case "$TOKEN" in
        *[!A-Za-z0-9._-]*) error "PROJECT_JWT contains invalid characters" ;;
    esac
    JWT_HEADER=${TOKEN%%.*}
    JWT_REMAINDER=${TOKEN#*.}
    JWT_PAYLOAD=${JWT_REMAINDER%%.*}
    JWT_SIGNATURE=${JWT_REMAINDER#*.}
    [ -n "$JWT_HEADER" ] && [ -n "$JWT_PAYLOAD" ] && [ -n "$JWT_SIGNATURE" ] ||
        error "PROJECT_JWT must be a JWT with three non-empty segments"
    case "$JWT_SIGNATURE" in *.*) error "PROJECT_JWT must be a JWT with three segments" ;; esac
    case "$ENDPOINT" in
        http://?*|https://?*) ;;
        *) error "--endpoint must be an absolute HTTP(S) URL" ;;
    esac
    case "$ENDPOINT" in
        *[[:space:]\"\'\\]*) error "--endpoint contains unsupported characters" ;;
    esac
}

main() {
    parse_args "$@"
    validate_paths
    validate_credentials

    [ "$(uname -s)" = "Linux" ] || error "dibra-deploy is supported only on Linux"

    require_command curl
    require_command tar
    require_command awk
    require_command install
    require_command id
    require_command "$SYSTEMCTL"

    ARCH=$(detect_arch)

    if [ -z "$VERSION" ]; then
        info "finding the latest release"
        VERSION=$(latest_version)
        [ -n "$VERSION" ] || error "could not determine the latest GitHub release"
    fi

    case "$VERSION" in
        v*) TAG=$VERSION ;;
        *) TAG="v$VERSION" ;;
    esac
    VERSION_NUM=${TAG#v}
    case "$VERSION_NUM" in
        ""|*[!a-zA-Z0-9._+-]*) error "invalid release version: $VERSION" ;;
    esac

    FILENAME="dibra-deploy_${VERSION_NUM}_linux_${ARCH}.tar.gz"
    DOWNLOAD_URL="${RELEASE_BASE_URL}/${TAG}/${FILENAME}"
    CHECKSUMS_URL="${RELEASE_BASE_URL}/${TAG}/checksums.txt"

    TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/dibra-deploy-install.XXXXXX")
    trap cleanup EXIT
    trap 'exit 1' HUP INT TERM
    mkdir "$TMP_DIR/extracted"

    info "downloading $FILENAME"
    curl -fsSL -o "$TMP_DIR/$FILENAME" "$DOWNLOAD_URL" ||
        error "failed to download $DOWNLOAD_URL"
    curl -fsSL -o "$TMP_DIR/checksums.txt" "$CHECKSUMS_URL" ||
        error "failed to download $CHECKSUMS_URL"

    EXPECTED_CHECKSUM=$(awk -v file="$FILENAME" '
        {
            name = $2
            sub(/^\*/, "", name)
            if (name == file) {
                print $1
                exit
            }
        }
    ' "$TMP_DIR/checksums.txt")
    [ -n "$EXPECTED_CHECKSUM" ] || error "checksum not found for $FILENAME"

    ACTUAL_CHECKSUM=$(sha256_file "$TMP_DIR/$FILENAME")
    [ "$EXPECTED_CHECKSUM" = "$ACTUAL_CHECKSUM" ] ||
        error "checksum verification failed for $FILENAME"
    info "checksum verified"

    tar -xzf "$TMP_DIR/$FILENAME" -C "$TMP_DIR/extracted" \
        dibra-deploy dibra-deploy.service ||
        error "release archive is missing dibra-deploy or dibra-deploy.service"
    [ -f "$TMP_DIR/extracted/dibra-deploy" ] &&
        [ ! -L "$TMP_DIR/extracted/dibra-deploy" ] ||
        error "release archive contains an invalid dibra-deploy binary"
    [ -f "$TMP_DIR/extracted/dibra-deploy.service" ] &&
        [ ! -L "$TMP_DIR/extracted/dibra-deploy.service" ] ||
        error "release archive contains an invalid systemd unit"

    BINARY_PATH="$INSTALL_DIR/dibra-deploy"
    UNIT_PATH="$SYSTEMD_UNIT_DIR/dibra-deploy.service"
    CONFIG_PATH="$CONFIG_DIR/environment"
    awk -v executable="$BINARY_PATH" -v environment="$CONFIG_PATH" '
        /^\[Service\]$/ {
            print
            print "EnvironmentFile=" environment
            next
        }
        /^EnvironmentFile=/ { next }
        /^ExecStart=/ {
            print "ExecStart=" executable
            next
        }
        { print }
    ' "$TMP_DIR/extracted/dibra-deploy.service" >"$TMP_DIR/dibra-deploy.service"
    awk -v expected="ExecStart=$BINARY_PATH" '
        $0 == expected { found = 1 }
        END { exit !found }
    ' "$TMP_DIR/dibra-deploy.service" ||
        error "could not configure ExecStart in the systemd unit"
    awk -v expected="EnvironmentFile=$CONFIG_PATH" '
        $0 == expected { found = 1 }
        END { exit !found }
    ' "$TMP_DIR/dibra-deploy.service" ||
        error "could not configure EnvironmentFile in the systemd unit"

    umask 077
    {
        printf 'DIBRA_DEPLOY_TOKEN=%s\n' "$TOKEN"
        printf 'DIBRA_DEPLOY_ENDPOINT=%s\n' "$ENDPOINT"
    } >"$TMP_DIR/environment"

    info "installing $BINARY_PATH"
    as_root install -d -m 0755 "$INSTALL_DIR"
    as_root install -m 0755 "$TMP_DIR/extracted/dibra-deploy" "$BINARY_PATH"
    as_root install -d -m 0755 "$SYSTEMD_UNIT_DIR"
    as_root install -m 0644 "$TMP_DIR/dibra-deploy.service" "$UNIT_PATH"
    info "installing root-only configuration in $CONFIG_PATH"
    as_root install -d -m 0700 "$CONFIG_DIR"
    as_root install -m 0600 "$TMP_DIR/environment" "$CONFIG_PATH"

    info "reloading systemd"
    as_root "$SYSTEMCTL" daemon-reload

    if [ "$ENABLE_SERVICE" -eq 1 ]; then
        info "enabling and starting dibra-deploy.service"
        as_root "$SYSTEMCTL" enable --now dibra-deploy.service
    else
        info "installed without enabling the service"
        info "start it with: sudo systemctl enable --now dibra-deploy.service"
    fi

    info "installation complete"
    info "dibra-deploy will fetch jobs from $ENDPOINT"
}

main "$@"
