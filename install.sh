#!/bin/sh
set -e

# ==============================================================================
# Loom Installer (Linux & macOS)
# ==============================================================================
# Installs Loom — A local AI control station for models and coding agents.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh
#
# Node (Linux, without sudo): add sh -s -- --node [--listen lan|ADDR:PORT|local]
# Node binaries default to ~/.local/lib/loom-node; full Loom is separate.
# Options (via environment variables):
#   LOOM_INSTALL_DIR   Target directory for binary (default: /usr/local/bin)
#   LOOM_VERSION       Specific version tag (default: latest)
# ==============================================================================

REPO="lucas-lepajollec/loom"
INSTALL_DIR="${LOOM_INSTALL_DIR:-/usr/local/bin}"
BIN_NAME="loom"
MODE="full"
NODE_LISTEN="${LOOM_NODE_LISTEN:-}"
NODE_HOME="${LOOM_NODE_HOME:-}"
NODE_BIN=""
NODE_MODELS=""
NODE_START=1
NODE_SERVICE=1
while [ "$#" -gt 0 ]; do
  case "$1" in
    --node) MODE="node"; shift ;;
    --no-start) NODE_START=0; shift ;;
    --no-service) NODE_SERVICE=0; NODE_START=0; shift ;;
    --listen|--home|--bin|--models)
      option="$1"
      [ "$#" -ge 2 ] && [ -n "$2" ] || { echo "Error: $option requires a value." >&2; exit 1; }
      case "$option" in
        --listen) NODE_LISTEN="$2" ;;
        --home) NODE_HOME="$2" ;;
        --bin) NODE_BIN="$2" ;;
        --models) NODE_MODELS="$2" ;;
      esac
      shift 2 ;;
    --help)
      echo "Usage: sh install.sh [--node [--listen lan|ADDR:PORT|local] [--home DIR] [--bin PATH] [--models DIR] [--no-start|--no-service]]"
      exit 0 ;;
    *) echo "Error: unknown option $1" >&2; exit 1 ;;
  esac
done
if [ "$MODE" = "node" ]; then
  [ "$(uname -s)" = "Linux" ] && [ "$(id -u)" != "0" ] || { echo "Error: node installation requires a Linux user, without sudo." >&2; exit 1; }
  INSTALL_DIR="${LOOM_INSTALL_DIR:-$HOME/.local/lib/loom-node}"
else
  [ "$NODE_START" = 1 ] && [ "$NODE_SERVICE" = 1 ] && [ -z "$NODE_BIN$NODE_MODELS$NODE_HOME" ] && [ -z "$NODE_LISTEN" ] || { echo "Error: node options require --node." >&2; exit 1; }
fi

# Automatic LAN selection must never choose a public or wildcard address.
if [ "$MODE" = node ]; then
  if [ -z "$NODE_LISTEN" ] && [ -t 0 ]; then
    printf 'Make this node reachable from your other machines on the local network? [Y/n] '
    read -r reach || reach=n
    case "$reach" in n|N|no|NO) NODE_LISTEN=127.0.0.1:2511 ;; *) NODE_LISTEN=lan ;; esac
  fi
  if [ "$NODE_LISTEN" = lan ]; then
    ROUTE_ADDRESS=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="src") print $(i+1)}')
    NODE_ADDRESS=$(printf '%s\n' "$ROUTE_ADDRESS" "$(hostname -I 2>/dev/null || true)" | awk '
      {for(i=1;i<=NF;i++) {n=split($i,a,"."); valid=(n==4); for(j=1;j<=n;j++) if(a[j] !~ /^[0-9]+$/ || a[j]>255) valid=0;
       if(valid && (a[1]==10 || (a[1]==172 && a[2]>=16 && a[2]<=31) || (a[1]==192 && a[2]==168) || (a[1]==100 && a[2]>=64 && a[2]<=127))) {print $i; exit}}}')
    [ -n "$NODE_ADDRESS" ] || { echo 'Error: no private LAN address found; choose --listen ADDR:PORT or local.' >&2; exit 1; }
    NODE_LISTEN="$NODE_ADDRESS:2511"
  fi
  [ "$NODE_LISTEN" != local ] || NODE_LISTEN=127.0.0.1:2511
fi

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
  echo "   ./bin/loom web 2510" >&2
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

# Node mode is user-only and has its own stable binary/service/data.
# Refuse an older release BEFORE touching the installation or stopping anything.
if [ "$MODE" = "node" ]; then
  if [ "$("${TMP_DIR}/${BIN_NAME}" node capabilities 2>/dev/null)" != "engine-node-v1" ]; then
    echo "Error: this release does not support engine nodes. Nothing was installed; select a release with node support." >&2
    exit 1
  fi
  [ "$NODE_SERVICE" = 0 ] || command -v systemctl >/dev/null 2>&1 || { echo "Error: systemctl missing; use --no-service for foreground mode." >&2; exit 1; }
  mkdir -p "$INSTALL_DIR"
  INSTALL_DIR="$(cd "$INSTALL_DIR" && pwd -P)"
  TARGET="$INSTALL_DIR/$BIN_NAME"
  if [ -z "$NODE_HOME" ] && [ -f "$INSTALL_DIR/node-home" ]; then NODE_HOME="$(cat "$INSTALL_DIR/node-home")"; fi
  [ ! -L "$TARGET" ] || { echo "Error: node target must not be a symlink." >&2; exit 1; }
  if [ -n "$NODE_HOME" ]; then
    case "$NODE_HOME" in /*) ;; *) NODE_HOME="$PWD/$NODE_HOME" ;; esac
  fi
  UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  HAD_BINARY=0
  HAD_UNIT=0
  WAS_ACTIVE=0
  [ ! -f "$TARGET" ] || { cp "$TARGET" "$TMP_DIR/previous"; HAD_BINARY=1; }
  [ ! -f "$UNIT_DIR/loom-node.service" ] || { cp "$UNIT_DIR/loom-node.service" "$TMP_DIR/previous-unit"; HAD_UNIT=1; }
  rollback_node() {
    [ "$NODE_SERVICE" = 0 ] || systemctl --user stop loom-node 2>/dev/null || true
    if [ "$HAD_BINARY" = 1 ]; then
      cp "$TMP_DIR/previous" "$INSTALL_DIR/.loom.restore"
      mv -f "$INSTALL_DIR/.loom.restore" "$TARGET"
    else
      rm -f "$TARGET"
    fi
    if [ "$NODE_SERVICE" = 1 ]; then
      if [ "$HAD_UNIT" = 1 ]; then cp "$TMP_DIR/previous-unit" "$UNIT_DIR/loom-node.service"; else rm -f "$UNIT_DIR/loom-node.service"; fi
      systemctl --user daemon-reload || true
      [ "$WAS_ACTIVE" = 0 ] || systemctl --user start loom-node || true
    fi
    rm -rf "$TMP_DIR"
  }
  trap rollback_node EXIT
  trap 'exit 1' INT TERM
  if [ "$NODE_SERVICE" = 1 ] && systemctl --user is-active --quiet loom-node 2>/dev/null; then
    WAS_ACTIVE=1
    systemctl --user stop loom-node
  fi
  # init cannot hold the running node's database lock. Explicitly stop first.
  set -- node init
  [ -z "$NODE_HOME" ] || set -- "$@" --home "$NODE_HOME"
  [ -z "$NODE_BIN" ] || set -- "$@" --bin "$NODE_BIN"
  [ -z "$NODE_MODELS" ] || set -- "$@" --models "$NODE_MODELS"
  "${TMP_DIR}/${BIN_NAME}" "$@"
  cp "${TMP_DIR}/${BIN_NAME}" "$INSTALL_DIR/.loom.new"
  chmod 755 "$INSTALL_DIR/.loom.new"
  mv -f "$INSTALL_DIR/.loom.new" "$TARGET"
  if [ "$NODE_SERVICE" = 1 ]; then
    set -- node install
    [ -z "$NODE_LISTEN" ] || set -- "$@" --listen "$NODE_LISTEN"
    [ -z "$NODE_HOME" ] || set -- "$@" --home "$NODE_HOME"
    "$TARGET" "$@"
    if [ "$NODE_START" = 1 ]; then
      systemctl --user enable --now loom-node
      systemctl --user is-active --quiet loom-node
    fi
  fi
  [ "$HAD_BINARY" = 0 ] || cp "$TMP_DIR/previous" "$TARGET.previous"
  [ -z "$NODE_HOME" ] || printf '%s\n' "$NODE_HOME" > "$INSTALL_DIR/node-home"
  trap 'rm -rf "$TMP_DIR"' EXIT
  echo "Loom Node installed: $TARGET"
  echo "Listener: ${NODE_LISTEN:-saved listener or 127.0.0.1:2511} (choose --listen for LAN/VPN)"
  if [ "$NODE_SERVICE" = 0 ]; then
    echo "Start: $TARGET node serve --listen '${NODE_LISTEN:-127.0.0.1:2511}' (add --home if configured). Stop a foreground node before reinstalling."
  else
    echo "Service: systemctl --user status loom-node"
    echo "Logs: journalctl --user -u loom-node"
    echo "For startup without a login, an administrator may need: loginctl enable-linger <user>"
  fi
  if [ "$NODE_SERVICE" = 1 ] && [ "$NODE_START" = 1 ]; then
    echo ""
    echo "=========================================================================="
    set -- node pair --if-unpaired
    [ -z "$NODE_HOME" ] || set -- "$@" --home "$NODE_HOME"
    "$TARGET" "$@"
    echo "=========================================================================="
  fi
  exit 0
fi

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
# Copy next to the target, then rename: replacing a running binary in place
# fails ("Text file busy"), while a rename lets running services keep the old
# file until they restart.
$SUDO cp "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/.${BIN_NAME}.new"
$SUDO chmod 755 "${INSTALL_DIR}/.${BIN_NAME}.new"
$SUDO mv -f "${INSTALL_DIR}/.${BIN_NAME}.new" "${INSTALL_DIR}/${BIN_NAME}"

echo "--> Running 'loom install' to configure system services..."
$SUDO "${INSTALL_DIR}/${BIN_NAME}" install

# An update: services already running restart on the new binary.
if command -v systemctl >/dev/null 2>&1; then
  for unit in loom-ui loom-engine; do
    if systemctl is-active --quiet "$unit" 2>/dev/null; then
      echo "--> Restarting $unit on the new version..."
      $SUDO systemctl restart "$unit"
    fi
  done
fi

echo ""
echo "=========================================================================="
echo "✓ Loom successfully installed at ${INSTALL_DIR}/${BIN_NAME}"
echo "=========================================================================="
echo "To start the web interface in foreground:"
echo "   loom web 2510"
echo ""
if [ "$TARGET_OS" = "linux" ]; then
  echo "To start as a background system service:"
  echo "   sudo systemctl start loom-ui"
else
  echo "macOS services are managed by launchd; inspect the installed plists before enabling them."
fi
echo "=========================================================================="
