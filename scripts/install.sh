#!/usr/bin/env bash
#
# Pi Couch installer (curl | bash friendly)
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/EstebanForge/pi-couch/main/scripts/install.sh | bash
#
# Flags via env:
#   INSTALL_DIR=/path # override install dir (default: /usr/local/bin or ~/.local/bin)
#   FORCE=1           # skip version check, always reinstall
#   VERSION=x         # install a specific release tag (default: latest)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}ℹ  $*${NC}" >&2; }
success() { echo -e "${GREEN}✓ $*${NC}" >&2; }
warn() { echo -e "${YELLOW}⚠  $*${NC}" >&2; }
error() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

REPO="EstebanForge/pi-couch"
BINARY="pi-couch"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"
FORCE="${FORCE:-0}"
workdir=""

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || error "Missing required command: $1"
}

detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        darwin) os="darwin" ;;
        linux) os="linux" ;;
        *) error "Unsupported OS: $os" ;;
    esac

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac

    echo "${os}-${arch}"
}

latest_version() {
    local tag
    if command -v curl >/dev/null 2>&1; then
        tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep -oE '"tag_name": *"[^"]+"' | cut -d'"' -f4)"
    elif command -v wget >/dev/null 2>&1; then
        tag="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep -oE '"tag_name": *"[^"]+"' | cut -d'"' -f4)"
    else
        error "curl or wget required to resolve latest version"
    fi
    [[ -n "$tag" ]] || error "Could not resolve the latest release"
    echo "$tag"
}

get_installed_version() {
    local binary_path="${1}/${BINARY}"
    if [[ -x "$binary_path" ]]; then
        "$binary_path" --version 2>/dev/null || echo ""
    else
        echo ""
    fi
}

pick_install_dir() {
    if [[ -n "$INSTALL_DIR" ]]; then
        echo "$INSTALL_DIR"
        return
    fi

    local default="/usr/local/bin"
    if [[ -w "$default" ]]; then
        echo "$default"
    else
        echo "${HOME}/.local/bin"
    fi
}

ensure_path_hint() {
    local dir="$1"
    case ":$PATH:" in
        *":$dir:"*) return ;;
        *) warn "Add $dir to PATH to use ${BINARY} globally" ;;
    esac
}

install_binary() {
    local src="$1" dest_dir="$2"
    mkdir -p "$dest_dir"
    local dest="${dest_dir}/${BINARY}"

    if install -m 0755 "$src" "$dest" 2>/dev/null; then
        :
    else
        info "Escalating to sudo for install into $dest_dir"
        sudo install -m 0755 "$src" "$dest"
    fi

    success "Installed: $dest"
}

download() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsL "$url" -o "$dest" || error "Download failed: $url"
    else
        wget -qO "$dest" "$url" || error "Download failed: $url"
    fi
    [[ -s "$dest" ]] || error "Download failed: empty file from $url"
}

main() {
    require_cmd uname
    require_cmd grep
    require_cmd sha256sum

    local platform
    platform="$(detect_platform)"

    local dest_dir
    dest_dir="$(pick_install_dir)"

    if [[ "$VERSION" == "latest" || -z "$VERSION" ]]; then
        info "Resolving latest version..."
        VERSION="$(latest_version)"
    fi

    if [[ "$FORCE" != "1" ]]; then
        local installed_version
        installed_version="$(get_installed_version "$dest_dir")"

        if [[ -n "$installed_version" && "$installed_version" == "$VERSION" ]]; then
            success "Already on latest version: ${VERSION}"
            echo -n "Do you want to reinstall? [y/N]: " >&2
            response=""
            if ! read -r response </dev/tty; then
                response=""
            fi
            response=$(echo "$response" | tr '[:upper:]' '[:lower:]')
            if [[ "$response" != "y" && "$response" != "yes" ]]; then
                info "Installation cancelled."
                exit 0
            fi
        elif [[ -n "$installed_version" ]]; then
            info "Upgrading: ${installed_version} → ${VERSION}"
        fi
    fi

    info "Platform: ${platform}"
    info "Version: ${VERSION}"

    # Global on purpose: the EXIT trap runs after main() returns.
    workdir="$(mktemp -d)"
    trap 'rm -rf "$workdir"' EXIT

    local base_url="https://github.com/${REPO}/releases/download/${VERSION}"
    info "Downloading pi-couch-${platform}..."
    download "${base_url}/pi-couch-${platform}" "${workdir}/pi-couch-${platform}"
    download "${base_url}/checksums.txt" "${workdir}/checksums.txt"
    # The release publishes checksums.txt; fail closed on a mismatch.
    (
        cd "$workdir"
        grep "pi-couch-${platform}\$" checksums.txt | sha256sum -c - >/dev/null 2>&1 \
            || error "Checksum mismatch for pi-couch-${platform}"
    )
    success "Checksum verified."

    chmod +x "${workdir}/pi-couch-${platform}"

    info "Install dir: ${dest_dir}"
    install_binary "${workdir}/pi-couch-${platform}" "$dest_dir"

    ensure_path_hint "$dest_dir"

    echo
    success "Pi Couch installed."
    echo "Verify with: ${BINARY} --version" >&2
    echo "Run it:      ${BINARY} --addr 127.0.0.1:8090   (see the README for the full setup)" >&2
}

main "$@"
