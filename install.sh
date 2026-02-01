#!/bin/bash
#
# AIPilot CLI Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/softwarity/aipilot-cli/main/install.sh | bash
#

set -e

REPO="softwarity/aipilot-cli"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="aipilot-cli"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "macos" ;;
        *)       error "Unsupported OS: $(uname -s). Use Windows installer for Windows." ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest release version
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
}

# Main installation
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║      AIPilot CLI Installer            ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""

    # Detect platform
    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Detected: $OS/$ARCH"

    # Get latest version
    info "Fetching latest version..."
    VERSION=$(get_latest_version)
    if [ -z "$VERSION" ]; then
        error "Failed to fetch latest version"
    fi
    info "Latest version: $VERSION"

    # Build download URL
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY_NAME-$OS-$ARCH"
    info "Downloading from: $DOWNLOAD_URL"

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    # Download binary
    TEMP_FILE=$(mktemp)
    if ! curl -fsSL "$DOWNLOAD_URL" -o "$TEMP_FILE"; then
        rm -f "$TEMP_FILE"
        error "Failed to download binary"
    fi

    # Install binary
    mv "$TEMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"

    info "Installed to: $INSTALL_DIR/$BINARY_NAME"

    # Check if install dir is in PATH
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        echo ""
        warn "$INSTALL_DIR is not in your PATH"
        echo ""
        echo "Add this to your shell config (~/.bashrc, ~/.zshrc, etc.):"
        echo ""
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        echo "Then restart your shell or run:"
        echo ""
        echo "  source ~/.bashrc  # or ~/.zshrc"
        echo ""
    fi

    # Verify installation
    if command -v "$BINARY_NAME" &> /dev/null || [ -x "$INSTALL_DIR/$BINARY_NAME" ]; then
        echo ""
        info "Installation complete! ✓"
        echo ""
        echo "Run 'aipilot-cli' to start (or '$INSTALL_DIR/$BINARY_NAME' if not in PATH)"
        echo ""
    else
        warn "Binary installed but may not be in PATH yet"
    fi
}

main "$@"
