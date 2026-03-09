#!/usr/bin/env bash
set -euo pipefail

REPO="thecodefreak/xfer"
BIN_NAME="xfer"
INSTALL_DIR="${HOME}/.local/bin"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux|darwin) ;;
    *)
        echo "Unsupported OS: $OS" >&2
        exit 1
        ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l) ARCH="armv7" ;;
    i386|i686) ARCH="386" ;;
    *)
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

mkdir -p "$INSTALL_DIR"

# Get latest release tag safely
if command -v jq >/dev/null 2>&1; then
    TAG="$(curl -fsSL "$API_URL" | jq -r '.tag_name // empty')"
else
    JSON="$(curl -fsSL "$API_URL")"
    TAG="$(printf '%s\n' "$JSON" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
fi

if [ -z "${TAG:-}" ]; then
    echo "Failed to detect latest release tag" >&2
    exit 1
fi

VER="${TAG#release-v}"
ASSET_URL="https://github.com/${REPO}/releases/download/${TAG}/xfer_v${VER}_${OS}_${ARCH}.tar.gz"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${BIN_NAME} ${TAG} for ${OS}/${ARCH}..."

curl -fL "$ASSET_URL" -o "$TMP_DIR/${BIN_NAME}.tar.gz"

tar -xzf "$TMP_DIR/${BIN_NAME}.tar.gz" -C "$TMP_DIR"

# Check binary exists
if [ ! -f "$TMP_DIR/$BIN_NAME" ]; then
    echo "Binary '$BIN_NAME' not found in archive" >&2
    exit 1
fi

install -m 0755 "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

echo "Installed to $INSTALL_DIR/$BIN_NAME"

# Warn if install dir is not in PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        echo
        echo "Warning: $INSTALL_DIR is not in your PATH."
        echo "Add this to your shell profile:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        ;;
esac