package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Reuses the saved SSH connection only for the explicit migration action.
// Download failures stop before invoking the installer; the installer verifies
// the release and starts the user service. Pair details stay server-side.
const remoteNodeInstallScript = remotePathPreamble + `set -eu
file=$(mktemp)
trap 'rm -f "$file"' EXIT
curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh -o "$file"
LOOM_VERSION="${LOOM_NODE_VERSION:-latest}" LOOM_INSTALL_DIR="$HOME/.local/lib/loom-node" sh "$file" --node --listen lan
set -- node pair --json
if [ -f "$HOME/.local/lib/loom-node/node-home" ]; then
 set -- "$@" --home "$(cat "$HOME/.local/lib/loom-node/node-home")"
fi
printf '\nLOOM-NODE-PAIR '
"$HOME/.local/lib/loom-node/loom" "$@"
`

var machineMigrations = struct {
	sync.Mutex
	phases map[string]string
}{phases: map[string]string{}}

var runNodeMigrationSSH = func(ctx context.Context, m RemoteMachine, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(m, key, "sh", "-s")...)
	// Install the node build matching this Loom: its flags and protocol are the
	// ones this interface expects (a development build installs the edge build).
	cmd.Stdin = strings.NewReader("LOOM_NODE_VERSION=" + nodeReleaseForThisLoom() + "\nexport LOOM_NODE_VERSION\n" + remoteNodeInstallScript)
	var out, stderr harnessTail
	cmd.Stdout, cmd.Stderr = &out, &stderr
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("SSH node installation: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func parseMigrationPair(out string) (nodePairDetails, error) {
	var details nodePairDetails
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "LOOM-NODE-PAIR ") {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "LOOM-NODE-PAIR ")), &details); err != nil {
			return details, fmt.Errorf("unreadable node pairing details: %w", err)
		}
		if _, _, err := cleanNodeURL(details.Address); err != nil {
			return details, err
		}
		if len(normalizePairCode(details.Code)) != 8 {
			return details, fmt.Errorf("node did not return a pairing code")
		}
		return details, nil
	}
	return details, fmt.Errorf("SSH installer did not return node pairing details")
}
func handleMachineNodeMigrate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		machineMigrations.Lock()
		phase := machineMigrations.phases[m.ID]
		machineMigrations.Unlock()
		sendJSON(w, 200, map[string]any{"ok": true, "phase": phase})
		return
	}
	if m.User == "" {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "machine is already using Loom Node"})
		return
	}
	if m.OS != "" && !strings.EqualFold(m.OS, "linux") {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "Loom Node installation currently requires Linux"})
		return
	}
	machineMigrations.Lock()
	if machineMigrations.phases[m.ID] != "" {
		machineMigrations.Unlock()
		sendJSON(w, 409, map[string]any{"ok": false, "error": "migration is already running"})
		return
	}
	machineMigrations.phases[m.ID] = "installing"
	machineMigrations.Unlock()
	defer func() { machineMigrations.Lock(); delete(machineMigrations.phases, m.ID); machineMigrations.Unlock() }()
	key, _, err := loomSSHKey()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	out, err := runNodeMigrationSSH(ctx, m, key)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	details, err := parseMigrationPair(out)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	machineMigrations.Lock()
	machineMigrations.phases[m.ID] = "pairing"
	machineMigrations.Unlock()
	if !usageVaultAccess(w) {
		return
	}
	handleMachinePairInput(w, r.WithContext(ctx), machinePairInput{Address: details.Address, Code: details.Code, MachineID: m.ID})
}

// nodeReleaseForThisLoom names the release a remote node should install so that
// it speaks the same flags and protocol as this Loom.
func nodeReleaseForThisLoom() string {
	v := strings.TrimPrefix(Version, "v")
	if strings.Contains(v, "-dev") || v == "" || v == "dev" {
		return "edge"
	}
	if ok, _ := regexp.MatchString(`^[0-9]+\.[0-9]+\.[0-9]+$`, v); ok {
		return "v" + v
	}
	return "latest"
}
