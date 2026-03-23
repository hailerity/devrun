#!/bin/sh
# Install devrun — download the latest binary from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hailerity/devrun/main/scripts/install.sh | sh
#
# Options (environment variables):
#   DEVRUN_VERSION   Install a specific version, e.g. "v1.2.3" (default: latest)
#   DEVRUN_INSTALL   Installation directory (default: /usr/local/bin, or ~/.local/bin if no sudo)

set -e

REPO="hailerity/devrun"
BINARY="devrun"

# --- helpers -----------------------------------------------------------------

say()  { printf '\033[1m%s\033[0m\n' "$*"; }
err()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "required tool not found: $1"; }

# --- detect OS and arch ------------------------------------------------------

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)  OS="linux"  ;;
  Darwin) OS="darwin" ;;
  *)      err "Unsupported OS: $OS. Download manually from https://github.com/$REPO/releases" ;;
esac

case "$ARCH" in
  x86_64)          ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *)               err "Unsupported architecture: $ARCH. Download manually from https://github.com/$REPO/releases" ;;
esac

# --- resolve version ---------------------------------------------------------

need curl

if [ -z "$DEVRUN_VERSION" ]; then
  say "Fetching latest release..."
  DEVRUN_VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')"
  [ -n "$DEVRUN_VERSION" ] || err "Could not determine latest version"
fi

say "Installing devrun $DEVRUN_VERSION ($OS/$ARCH)"

# --- resolve install dir -----------------------------------------------------

if [ -n "$DEVRUN_INSTALL" ]; then
  INSTALL_DIR="$DEVRUN_INSTALL"
elif [ -w /usr/local/bin ]; then
  INSTALL_DIR="/usr/local/bin"
elif sudo -n true 2>/dev/null; then
  INSTALL_DIR="/usr/local/bin"
  USE_SUDO=1
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

# --- download and install ----------------------------------------------------

ARCHIVE="${BINARY}_${DEVRUN_VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$DEVRUN_VERSION/$ARCHIVE"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

say "Downloading $URL"
curl -fsSL "$URL" -o "$TMP/$ARCHIVE"

need tar
tar -xzf "$TMP/$ARCHIVE" -C "$TMP" "$BINARY"

if [ -n "$USE_SUDO" ]; then
  sudo mv "$TMP/$BINARY" "$INSTALL_DIR/$BINARY"
  sudo chmod +x "$INSTALL_DIR/$BINARY"
else
  mv "$TMP/$BINARY" "$INSTALL_DIR/$BINARY"
  chmod +x "$INSTALL_DIR/$BINARY"
fi

# --- verify ------------------------------------------------------------------

if command -v "$BINARY" >/dev/null 2>&1; then
  say "devrun installed successfully!"
  "$BINARY" --version
else
  say "devrun installed to $INSTALL_DIR/$BINARY"
  say "Add $INSTALL_DIR to your PATH if it is not already there:"
  say "  export PATH=\"\$PATH:$INSTALL_DIR\""
fi
