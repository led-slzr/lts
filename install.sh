#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
#  LTS (Led's Tree Script) Installer & Updater
#  Install/Update: curl -fsSL https://raw.githubusercontent.com/led-slzr/lts/main/install.sh | bash
# ============================================================================

REPO="led-slzr/lts"
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/lts"
BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
DIM='\033[2m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Detect if updating or fresh install
IS_UPDATE=false
OLD_VERSION=""
if [ -x "${INSTALL_DIR}/lts" ]; then
    IS_UPDATE=true
    OLD_VERSION=$("${INSTALL_DIR}/lts" --version 2>/dev/null || echo "unknown")
fi

echo ""
if $IS_UPDATE; then
    echo -e "${GREEN}${BOLD}Updating LTS - Led's Tree Script${NC}"
    echo -e "${DIM}Current: ${OLD_VERSION}${NC}"
else
    echo -e "${GREEN}${BOLD}Installing LTS - Led's Tree Script${NC}"
    echo -e "${DIM}Git worktree management TUI${NC}"
fi
echo ""

# Check Git
if ! command -v git &>/dev/null; then
    echo -e "${RED}Error: Git is not installed.${NC}"
    exit 1
fi

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Darwin) os="darwin" ;;
        Linux)  os="linux" ;;
        MINGW*|MSYS*|CYGWIN*)
            echo -e "${RED}Error: Windows is not supported. Use WSL.${NC}"
            exit 1
            ;;
        *)
            echo -e "${RED}Error: Unsupported OS: $(uname -s)${NC}"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac

    echo "${os}_${arch}"
}

# Try downloading pre-built binary from GitHub Releases
install_from_release() {
    local platform="$1"
    local tag url tmp_dir

    # Get latest release tag
    tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")

    if [ -z "$tag" ]; then
        return 1
    fi

    url="https://github.com/${REPO}/releases/download/${tag}/lts_${platform}.tar.gz"
    echo -e "${DIM}Downloading ${tag} for ${platform}...${NC}"

    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" RETURN

    if curl -fsSL "$url" -o "${tmp_dir}/lts.tar.gz" 2>/dev/null; then
        tar -xzf "${tmp_dir}/lts.tar.gz" -C "$tmp_dir"
        mkdir -p "$INSTALL_DIR"
        mv "${tmp_dir}/lts" "${INSTALL_DIR}/lts"
        chmod +x "${INSTALL_DIR}/lts"
        return 0
    else
        return 1
    fi
}

# Fallback: build from source (requires Go)
install_from_source() {
    echo -e "${YELLOW}Pre-built binary not available, building from source...${NC}"

    if ! command -v go &>/dev/null; then
        echo -e "${RED}Error: Go is not installed and no pre-built binary is available.${NC}"
        echo "Either:"
        echo "  1. Install Go from https://go.dev/dl/ and try again"
        echo "  2. Wait for a release with pre-built binaries"
        exit 1
    fi

    local go_version
    go_version=$(go version | awk '{print $3}' | sed 's/go//')
    echo -e "${DIM}Go version: ${go_version}${NC}"

    local build_dir
    build_dir=$(mktemp -d)
    trap "rm -rf $build_dir" RETURN

    echo -e "Cloning repository..."
    git clone --depth 1 "https://github.com/${REPO}.git" "$build_dir" 2>/dev/null

    echo -e "Building LTS..."
    cd "$build_dir"
    go build -o lts .

    mkdir -p "$INSTALL_DIR"
    mv lts "$INSTALL_DIR/lts"
    chmod +x "$INSTALL_DIR/lts"
}

# --- Main ---

PLATFORM=$(detect_platform)
echo -e "${DIM}Platform: ${PLATFORM}${NC}"

if ! install_from_release "$PLATFORM"; then
    install_from_source
fi

echo -e "${GREEN}Binary installed to ${INSTALL_DIR}/lts${NC}"

# Ensure on PATH (only if not already there)
if ! echo "$PATH" | tr ':' '\n' | grep -q "^${INSTALL_DIR}$"; then
    SHELL_RC=""
    case "${SHELL:-/bin/bash}" in
        */zsh)  SHELL_RC="$HOME/.zshrc" ;;
        */bash) SHELL_RC="$HOME/.bashrc" ;;
        */fish) SHELL_RC="$HOME/.config/fish/config.fish" ;;
    esac

    if [ -n "$SHELL_RC" ]; then
        if ! grep -q "${INSTALL_DIR}" "$SHELL_RC" 2>/dev/null; then
            echo "" >> "$SHELL_RC"
            echo "# LTS - Led's Tree Script" >> "$SHELL_RC"
            echo "export PATH=\"${INSTALL_DIR}:\$PATH\"" >> "$SHELL_RC"
            echo -e "${DIM}Added ${INSTALL_DIR} to PATH in ${SHELL_RC}${NC}"
        fi
    else
        echo -e "${DIM}Add this to your shell config:${NC}"
        echo -e "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
fi

# Create global config directory
mkdir -p "$CONFIG_DIR"

NEW_VERSION=$("${INSTALL_DIR}/lts" --version 2>/dev/null || echo "LTS v2.2.0")
echo ""
if $IS_UPDATE; then
    echo -e "${GREEN}${BOLD}${NEW_VERSION} updated successfully!${NC}"
    echo -e "${DIM}${OLD_VERSION} → ${NEW_VERSION}${NC}"
else
    echo -e "${GREEN}${BOLD}${NEW_VERSION} installed successfully!${NC}"
fi
echo ""
echo -e "Usage:"
echo -e "  ${BOLD}lts${NC}              Run in current directory"
echo -e "  ${BOLD}lts --dir ~/repos${NC} Run in specific directory"
echo ""
if ! $IS_UPDATE; then
    echo -e "${DIM}If 'lts' is not found, restart your terminal or run:${NC}"
    echo -e "  source ~/.zshrc  ${DIM}(or ~/.bashrc)${NC}"
    echo ""
fi
