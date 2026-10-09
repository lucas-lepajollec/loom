#!/bin/sh
# Real candidate binary, verified local artifacts, simulated systemd user bus.
# No host services, release downloads, model deletion or account execution.
set -eu
[ -n "${LOOM_TEST_BINARY:-}" ] || { echo 'Set LOOM_TEST_BINARY to a built candidate.' >&2; exit 1; }
[ "$(id -u)" != 0 ] || { echo 'Run this acceptance test as a normal user.' >&2; exit 1; }
ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d)
cleanup() {
  status=$?
  if [ "$status" != 0 ]; then
    for log in "$TMP"/*.log; do
      [ ! -f "$log" ] || { printf '\n%s\n' "Installer fixture: $(basename "$log")"; tail -30 "$log"; }
    done
  fi
  rm -rf "$TMP"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' INT TERM
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
 start|enable|restart) touch "$FIXTURE_ACTIVE" ;;
 daemon-reload) [ "${FIXTURE_FAIL_UNIT:-0}" = 0 ] ;;
 *) exit 1 ;;
esac
SH
chmod +x "$TMP/bin/curl" "$TMP/bin/systemctl"
export LOOM_HOME="$TMP/main-fixture"
export HOME="$TMP/home" XDG_CONFIG_HOME="$TMP/home/.config" XDG_DATA_HOME="$TMP/home/.local/share"
export PATH="$TMP/bin:$PATH" LOOM_INSTALL_DIR="$TMP/installed" LOOM_VERSION=v99.0.0
export FIXTURE_ARTIFACT="$TMP/candidate" FIXTURE_SUMS="$TMP/sums" FIXTURE_SYSTEMD_LOG="$TMP/systemd.log" FIXTURE_ACTIVE="$TMP/active"
sh "$ROOT/install.sh" --node --listen 127.0.0.1:2622 --home "$TMP/data" --models "$TMP/models" --bin /bin/true > "$TMP/install.log"
[ -f "$TMP/active" ]
[ ! -e "$HOME/.local/share/loom" ]
[ ! -e "$TMP/main-fixture" ]
[ ! -e "$TMP/data/memory" ]
[ ! -e "$XDG_CONFIG_HOME/systemd/user/loom-ui.service" ]
[ -f "$TMP/data/node.token" ]
grep -F '127.0.0.1:2622' "$XDG_CONFIG_HOME/systemd/user/loom-node.service" >/dev/null
grep -F 'Node address: http://127.0.0.1:2622' "$TMP/install.log" >/dev/null
grep -E 'Pairing code: [0-9A-Z]{4}-[0-9A-Z]{4}' "$TMP/install.log" >/dev/null
grep -E 'Expires: [0-9]{4}-' "$TMP/install.log" >/dev/null
grep -F 'In Loom: Machines › Add a machine, or let Loom find it on the network' "$TMP/install.log" >/dev/null
cp "$TMP/data/node.token" "$TMP/token-before"
# Repeat without home/listen flags: retain custom home, listener and credentials.
sh "$ROOT/install.sh" --node > "$TMP/reinstall.log"
cmp "$TMP/token-before" "$TMP/data/node.token"
grep -F '127.0.0.1:2622' "$XDG_CONFIG_HOME/systemd/user/loom-node.service" >/dev/null
[ -f "$TMP/installed/loom.previous" ]
first_code=$(sed -n 's/.*Pairing code: \([A-Z0-9-]*\).*/\1/p' "$TMP/install.log")
next_code=$(sed -n 's/.*Pairing code: \([A-Z0-9-]*\).*/\1/p' "$TMP/reinstall.log")
[ -n "$next_code" ] && [ "$first_code" != "$next_code" ]
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
# LAN detection: primary private route first, fallback ignores public addresses.
cat > "$TMP/bin/ip" <<'SH'
#!/bin/sh
printf '%s\n' "1.1.1.1 via 192.168.1.1 src ${FIXTURE_ROUTE_IP:-192.168.1.42}"
SH
cat > "$TMP/bin/hostname" <<'SH'
#!/bin/sh
if [ "$1" = '-I' ]; then printf '%s\n' "${FIXTURE_HOST_IPS:-203.0.113.9 100.64.2.3}"; else echo fixture-node; fi
SH
chmod +x "$TMP/bin/ip" "$TMP/bin/hostname"
# A new non-TTY installation defaults to loopback in the release layout.
unset LOOM_INSTALL_DIR
sh "$ROOT/install.sh" --node --home "$TMP/default-data" > "$TMP/default.log"
[ -x "$HOME/.local/lib/loom-node/loom" ]
grep -F 'Node address: http://127.0.0.1:2511' "$TMP/default.log" >/dev/null
sh "$ROOT/install.sh" --node --listen lan > "$TMP/lan.log"
grep -F 'Node address: http://192.168.1.42:2511' "$TMP/lan.log" >/dev/null
export FIXTURE_ROUTE_IP=203.0.113.8
sh "$ROOT/install.sh" --node --listen lan > "$TMP/cgnat.log"
grep -F 'Node address: http://100.64.2.3:2511' "$TMP/cgnat.log" >/dev/null
export FIXTURE_HOST_IPS='203.0.113.9 172.32.0.1 100.128.1.2'
cp "$FIXTURE_SYSTEMD_LOG" "$TMP/log-before"
if sh "$ROOT/install.sh" --node --listen lan > "$TMP/public.log" 2>&1; then echo 'Selected public LAN address' >&2; exit 1; fi
cmp "$TMP/log-before" "$FIXTURE_SYSTEMD_LOG"
unset FIXTURE_ROUTE_IP FIXTURE_HOST_IPS
# Change later with the installed binary: persists and restarts the user unit.
"$HOME/.local/lib/loom-node/loom" node listen lan --home "$TMP/default-data" > "$TMP/listen-lan.log"
grep -F 'Node address: http://192.168.1.42:2511' "$TMP/listen-lan.log" >/dev/null
"$HOME/.local/lib/loom-node/loom" node listen local --home "$TMP/default-data" > "$TMP/listen.log"
grep -F -- '--user restart loom-node' "$FIXTURE_SYSTEMD_LOG" >/dev/null
sh "$ROOT/install.sh" --node > "$TMP/local.log"
grep -F 'Node address: http://127.0.0.1:2511' "$TMP/local.log" >/dev/null
# Exercise the interactive default through a TTY, when Python is available.
if command -v python3 >/dev/null 2>&1; then
  export FIXTURE_INSTALL_SCRIPT="$ROOT/install.sh" FIXTURE_TTY_LOG="$TMP/tty.log"
  python3 - <<'PYTTY'
import os, pty, subprocess
for answer, address in [(b'\n', '192.168.1.42'), (b'n\n', '127.0.0.1')]:
    master, slave = pty.openpty()
    p = subprocess.Popen(['sh', os.environ['FIXTURE_INSTALL_SCRIPT'], '--node'], stdin=slave, stdout=slave, stderr=slave)
    os.close(slave)
    os.write(master, answer)
    output = bytearray()
    while True:
        try:
            chunk = os.read(master, 4096)
        except OSError:
            break
        if not chunk:
            break
        output.extend(chunk)
    os.close(master)
    assert p.wait(timeout=30) == 0, output.decode()
    text = output.decode()
    assert 'Make this node reachable from your other machines on the local network? [Y/n]' in text
    assert 'Node address: http://' + address + ':2511' in text
    with open(os.environ['FIXTURE_TTY_LOG'], 'a') as f:
        f.write(text)
PYTTY
fi
# Paired CLI fixture: installer requests --if-unpaired and prints its result,
# never invokes an explicit code replacement. Real pairing state is covered in Go.
cat > "$TMP/paired-candidate" <<'SH'
#!/bin/sh
if [ "$1 $2 $3" = 'node pair --if-unpaired' ]; then
 echo 'Node address: http://192.168.1.42:2511'
 echo 'already paired with Fixture Loom'
 echo 'In Loom: Machines › Add a machine, or let Loom find it on the network'
 exit 0
fi
exec "$FIXTURE_REAL_BINARY" "$@"
SH
chmod +x "$TMP/paired-candidate"
export FIXTURE_REAL_BINARY="$TMP/candidate" FIXTURE_ARTIFACT="$TMP/paired-candidate"
sha256sum "$FIXTURE_ARTIFACT" | awk '{print $1 "  loom-linux"}' > "$TMP/sums"
sh "$ROOT/install.sh" --node > "$TMP/paired.log"
grep -F 'already paired with Fixture Loom' "$TMP/paired.log" >/dev/null
if grep -F 'Pairing code:' "$TMP/paired.log" >/dev/null; then echo 'Issued a paired node code' >&2; exit 1; fi
echo 'PASS: node installer, summary, LAN/TTY/local, reinstall, paired, checksums, rollback, portable mode.'
