#!/bin/sh
set -e

# ==============================================================================
# Loom Installer (Linux & macOS)
# ==============================================================================
# Installs Loom — Workstation control plane and test bench for llama.cpp.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh
#
# Options (via environment variables):
#   LOOM_INSTALL_DIR   Target directory for binary (default: /usr/local/bin)
#   GITHUB_TOKEN       Token for accessing private GitHub repository releases
#   LOOM_VERSION       Specific version tag (default: latest)
# ==============================================================================

REPO="lucas-lepajollec/loom"
INSTALL_DIR="${LOOM_INSTALL_DIR:-/usr/local/bin}"
BIN_NAME="loom"

# 1. Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux)
    TARGET_OS="linux"
    ;;
  darwin)
    TARGET_OS="macos"
    ;;
  *)
    echo "Error: Unsupported operating system '$OS'. Loom supports Linux and macOS (use install.ps1 on Windows)." >&2
    exit 1
    ;;
esac

# 2. Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)
    TARGET_ARCH=""
    ;;
  aarch64|arm64)
    TARGET_ARCH="-arm"
    ;;
  *)
    echo "Error: Unsupported CPU architecture '$ARCH'." >&2
    exit 1
    ;;
esac

ASSET_NAME="loom-${TARGET_OS}${TARGET_ARCH}"
echo "--> Detected platform: ${TARGET_OS} (${ARCH:-x86_64}) -> asset: ${ASSET_NAME}"

# 3. Determine download URL
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'loom-install')"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

AUTH_HEADER=""
if [ -n "$GITHUB_TOKEN" ]; then
  AUTH_HEADER="Authorization: Bearer $GITHUB_TOKEN"
fi

echo "--> Checking available releases for ${REPO}..."

DOWNLOAD_URL=""
if [ -n "$LOOM_VERSION" ] && [ "$LOOM_VERSION" != "latest" ]; then
  RELEASE_TAG="$LOOM_VERSION"
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${ASSET_NAME}"
else
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
fi

# Download binary
HTTP_CODE=$(curl -sL -w "%{http_code}" ${AUTH_HEADER:+-H "$AUTH_HEADER"} -o "${TMP_DIR}/${BIN_NAME}" "$DOWNLOAD_URL" || true)

if [ "$HTTP_CODE" != "200" ] || [ ! -s "${TMP_DIR}/${BIN_NAME}" ]; then
  echo "" >&2
  echo "Notice: No prebuilt release asset found at ${DOWNLOAD_URL} (HTTP ${HTTP_CODE})." >&2
  
  # Check if Go is installed locally to offer a fallback build
  if command -v go >/dev/null 2>&1; then
    echo "--> Go is installed. Attempting to build from source via 'go install'..."
    if [ -n "$GITHUB_TOKEN" ]; then
      git config --global url."https://${GITHUB_TOKEN}:x-oauth-basic@github.com/".insteadOf "https://github.com/"
    fi
    GOBIN="$TMP_DIR" go install "github.com/${REPO}/cmd/loom@latest" || true
  fi

  if [ ! -s "${TMP_DIR}/${BIN_NAME}" ]; then
    echo "" >&2
    echo "==========================================================================" >&2
    echo "The repository '${REPO}' does not currently have public binary releases." >&2
    echo "If you have already cloned the repository locally, build and install with:" >&2
    echo "   make build" >&2
    echo "   sudo ./bin/loom install" >&2
    echo "==========================================================================" >&2
    exit 1
  fi
fi

chmod +x "${TMP_DIR}/${BIN_NAME}"

# 4. Install binary to system
echo "--> Installing ${BIN_NAME} to ${INSTALL_DIR}..."

SUDO=""
if [ "$(id -u)" != "0" ]; then
  if [ -w "$INSTALL_DIR" ]; then
    SUDO=""
  elif command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "Error: Need root privileges to write to ${INSTALL_DIR}. Run as root or install sudo." >&2
    exit 1
  fi
fi

$SUDO mkdir -p "$INSTALL_DIR"
$SUDO cp "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
$SUDO chmod 755 "${INSTALL_DIR}/${BIN_NAME}"

echo "--> Running 'loom install' to configure system services..."
$SUDO "${INSTALL_DIR}/${BIN_NAME}" install

echo ""
echo "=========================================================================="
echo "✓ Loom successfully installed at ${INSTALL_DIR}/${BIN_NAME}"
echo "=========================================================================="
echo "To start the web interface in foreground:"
echo "   loom web 8091"
echo ""
echo "To start as a background system service:"
echo "   sudo systemctl start loom-ui"
echo "=========================================================================="
