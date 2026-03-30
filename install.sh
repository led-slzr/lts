#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
#  LTS (Led's Tree Script) Installer & Updater
#  Install/Update: curl -fsSL https://raw.githubusercontent.com/led-slzr/lts/main/install.sh | bash
# ============================================================================

REPO_URL="https://github.com/led-slzr/lts.git"
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/lts"
BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
DIM='\033[2m'
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

# Check Go is installed
if ! command -v go &>/dev/null; then
    echo -e "${RED}Error: Go is not installed.${NC}"
    echo "Install Go from https://go.dev/dl/ and try again."
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "${DIM}Go version: ${GO_VERSION}${NC}"

# Check Git
if ! command -v git &>/dev/null; then
    echo -e "${RED}Error: Git is not installed.${NC}"
    exit 1
fi

# Create temp build directory
BUILD_DIR=$(mktemp -d)
trap "rm -rf $BUILD_DIR" EXIT

# Clone
echo -e "Cloning repository..."
git clone --depth 1 "$REPO_URL" "$BUILD_DIR" 2>/dev/null

# Build
echo -e "Building LTS..."
cd "$BUILD_DIR"
go build -o lts .

# Install
mkdir -p "$INSTALL_DIR"
mv lts "$INSTALL_DIR/lts"
chmod +x "$INSTALL_DIR/lts"
echo -e "${GREEN}Binary installed to ${INSTALL_DIR}/lts${NC}"

# Ensure on PATH (only on fresh install, skip if already there)
if ! echo "$PATH" | tr ':' '\n' | grep -q "^${INSTALL_DIR}$"; then
    # Also check if it's already in the shell config file
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

NEW_VERSION=$("${INSTALL_DIR}/lts" --version 2>/dev/null || echo "LTS v2.0.1")
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
