#!/bin/sh
# Real candidate binary, verified local artifacts, simulated systemd user bus.
# No host services, release downloads, model deletion or account execution.
set -eu
[ -n "${LOOM_TEST_BINARY:-}" ] || { echo 'Set LOOM_TEST_BINARY to a built candidate.' >&2; exit 1; }
[ "$(id -u)" != 0 ] || { echo 'Run this acceptance test as a normal user.' >&2; exit 1; }
ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM
mkdir -p "$TMP/bin" "$TMP/home" "$TMP/models"
cp "$LOOM_TEST_BINARY" "$TMP/candidate"
sha256sum "$TMP/candidate" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
cat > "$TMP/bin/curl" <<'SH'
#!/bin/sh
url=$2
case "$url" in
 https://github.com/lucas-lepajollec/loom/releases/download/v99.0.0/loom-linux) cp "$FIXTURE_ARTIFACT" "$4" ;;
 https://github.com/lucas-lepajollec/loom/releases/download/v99.0.0/SHA256SUMS.txt) cp "$FIXTURE_SUMS" "$4" ;;
 *) echo 'Unexpected download target' >&2; exit 1 ;;
esac
SH
cat > "$TMP/bin/systemctl" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE_SYSTEMD_LOG"
[ "$1" = '--user' ] || { echo 'Unexpected system scope' >&2; exit 1; }
case "$2" in
 is-active) [ -f "$FIXTURE_ACTIVE" ] ;;
 stop) rm -f "$FIXTURE_ACTIVE" ;;
 start|enable) touch "$FIXTURE_ACTIVE" ;;
 daemon-reload) [ "${FIXTURE_FAIL_UNIT:-0}" = 0 ] ;;
 *) exit 1 ;;
esac
SH
chmod +x "$TMP/bin/curl" "$TMP/bin/systemctl"
export HOME="$TMP/home" XDG_CONFIG_HOME="$TMP/home/.config" XDG_DATA_HOME="$TMP/home/.local/share"
export PATH="$TMP/bin:$PATH" LOOM_INSTALL_DIR="$TMP/installed" LOOM_VERSION=v99.0.0
export FIXTURE_ARTIFACT="$TMP/candidate" FIXTURE_SUMS="$TMP/sums" FIXTURE_SYSTEMD_LOG="$TMP/systemd.log" FIXTURE_ACTIVE="$TMP/active"
sh "$ROOT/install.sh" --node --listen 127.0.0.1:2622 --home "$TMP/data" --models "$TMP/models" --bin /bin/true > "$TMP/install.log"
[ -f "$TMP/active" ]
[ ! -e "$HOME/.local/share/loom" ]
[ ! -e "$TMP/data/memory" ]
[ ! -e "$XDG_CONFIG_HOME/systemd/user/loom-ui.service" ]
[ -f "$TMP/data/node.token" ]
grep -F '127.0.0.1:2622' "$XDG_CONFIG_HOME/systemd/user/loom-node.service" >/dev/null
cp "$TMP/data/node.token" "$TMP/token-before"
# Repeat without home/listen flags: retain custom home, listener and credentials.
sh "$ROOT/install.sh" --node > "$TMP/reinstall.log"
cmp "$TMP/token-before" "$TMP/data/node.token"
grep -F '127.0.0.1:2622' "$XDG_CONFIG_HOME/systemd/user/loom-node.service" >/dev/null
[ -f "$TMP/installed/loom.previous" ]
# Invalid release must not stop or replace the working installation.
printf '#!/bin/sh\nexit 1\n' > "$TMP/old-release"
chmod +x "$TMP/old-release"
export FIXTURE_ARTIFACT="$TMP/old-release"
sha256sum "$TMP/old-release" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
cp "$FIXTURE_SYSTEMD_LOG" "$TMP/log-before"
if sh "$ROOT/install.sh" --node > "$TMP/refused.log" 2>&1; then echo 'Accepted unsupported release' >&2; exit 1; fi
cmp "$TMP/log-before" "$FIXTURE_SYSTEMD_LOG"
cmp "$TMP/candidate" "$TMP/installed/loom"
# Failed unit installation restores the previous executable/unit and active state.
export FIXTURE_ARTIFACT="$TMP/candidate" FIXTURE_FAIL_UNIT=1
sha256sum "$TMP/candidate" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
cp "$XDG_CONFIG_HOME/systemd/user/loom-node.service" "$TMP/unit-before"
if sh "$ROOT/install.sh" --node --listen 127.0.0.1:2623 > "$TMP/rollback.log" 2>&1; then echo 'Ignored service failure' >&2; exit 1; fi
cmp "$TMP/unit-before" "$XDG_CONFIG_HOME/systemd/user/loom-node.service"
cmp "$TMP/candidate" "$TMP/installed/loom"
[ -f "$TMP/active" ]
export FIXTURE_FAIL_UNIT=0
# Checksum mismatch also leaves the active service/binary intact.
printf '%064d  loom-linux\n' 0 > "$TMP/sums"
if sh "$ROOT/install.sh" --node > "$TMP/checksum.log" 2>&1; then echo 'Accepted bad checksum' >&2; exit 1; fi
cmp "$TMP/candidate" "$TMP/installed/loom"
[ -f "$TMP/active" ]
# --no-start leaves an existing active node stopped after a successful reinstall.
sha256sum "$TMP/candidate" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
sh "$ROOT/install.sh" --node --no-start > "$TMP/no-start.log"
[ ! -f "$TMP/active" ]
# Foreground-only installation works without generating/reloading any unit.
export LOOM_INSTALL_DIR="$TMP/portable"
sha256sum "$TMP/candidate" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
cp "$FIXTURE_SYSTEMD_LOG" "$TMP/log-before"
sh "$ROOT/install.sh" --node --no-service --home "$TMP/portable-data" > "$TMP/portable.log"
cmp "$TMP/log-before" "$FIXTURE_SYSTEMD_LOG"
[ -x "$TMP/portable/loom" ]
echo 'PASS: node installer, repeat, old release refusal, checksum, rollback, portable mode.'
