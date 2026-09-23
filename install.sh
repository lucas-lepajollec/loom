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

echo "--> Checking available releases for ${REPO}..."

DOWNLOAD_URL=""
if [ -n "$LOOM_VERSION" ] && [ "$LOOM_VERSION" != "latest" ]; then
  if ! printf '%s' "$LOOM_VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Error: LOOM_VERSION must be a release tag such as v0.1.0." >&2
    exit 1
  fi
  RELEASE_TAG="$LOOM_VERSION"
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${ASSET_NAME}"
else
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
fi

# Download binary and verify its release checksum.
CHECKSUM_URL="${DOWNLOAD_URL%/*}/SHA256SUMS.txt"
if ! curl -fLsS "$DOWNLOAD_URL" -o "${TMP_DIR}/${BIN_NAME}" || [ ! -s "${TMP_DIR}/${BIN_NAME}" ]; then
  echo "Notice: No prebuilt release asset found at ${DOWNLOAD_URL}." >&2
  echo "No matching release binary is available. For an authorized source checkout:" >&2
  echo "   make build" >&2
  echo "   ./bin/loom web 8091" >&2
  exit 1
fi
if ! curl -fLsS "$CHECKSUM_URL" -o "${TMP_DIR}/SHA256SUMS.txt"; then
  echo "Error: Release checksum manifest is missing; installation cancelled." >&2
  exit 1
fi
EXPECTED_SHA=$(awk -v asset="$ASSET_NAME" '$2 == asset || $2 == "*" asset { print $1 }' "${TMP_DIR}/SHA256SUMS.txt")
if [ "$(printf '%s' "$EXPECTED_SHA" | wc -c)" -ne 64 ] || ! printf '%s' "$EXPECTED_SHA" | LC_ALL=C grep -Eq '^[[:xdigit:]]{64}$'; then
  echo "Error: No valid SHA-256 entry for ${ASSET_NAME}; installation cancelled." >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_SHA=$(sha256sum "${TMP_DIR}/${BIN_NAME}" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL_SHA=$(shasum -a 256 "${TMP_DIR}/${BIN_NAME}" | awk '{ print $1 }')
else
  echo "Error: sha256sum or shasum is required to verify the release." >&2
  exit 1
fi
if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
  echo "Error: SHA-256 mismatch for ${ASSET_NAME}; installation cancelled." >&2
  exit 1
fi

chmod +x "${TMP_DIR}/${BIN_NAME}"

# 4. Install binary to system
echo "--> Installing ${BIN_NAME} to ${INSTALL_DIR}..."

SUDO=""
if [ "$(id -u)" != "0" ]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "Error: Loom's system-service installer requires root privileges. Run as root or install sudo." >&2
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
if [ "$TARGET_OS" = "linux" ]; then
  echo "To start as a background system service:"
  echo "   sudo systemctl start loom-ui"
else
  echo "macOS services are managed by launchd; inspect the installed plists before enabling them."
fi
echo "=========================================================================="
