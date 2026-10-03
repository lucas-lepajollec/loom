package loom

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type harnessConnection struct {
	Connected bool  `json:"connected"`
	At        int64 `json:"at"`
}

// Installation is not consent to expose a harness in the conversation picker.
// Existing conversations and explicitly added custom/remote adapters migrate
// without reopening accounts or changing the user's native configuration.
func harnessConnected(agent acpAgent) bool {
	var c harnessConnection
	if getStoreJSON(bkHarnessConnections, agent.ID, &c) && c.At > 0 {
		return c.Connected
	}
	if agent.Custom || agent.Machine != "" {
		return true
	}
	if getStr(bkHarnessConnections, agent.ID) != "" {
		return true
	}

	return false
}

// One startup migration, rather than scanning every full conversation on each
// descriptor/picker refresh. It records only the presence of existing usage.
func migrateHarnessConnections() {
	used := map[string]bool{}
	for id := range allKV(bkRuntimeSessions) {
		var route struct {
			RuntimeID       string `json:"runtime_id"`
			NativeRuntimeID string `json:"native_runtime_id"`
		}
		if getStoreJSON(bkRuntimeSessions, id, &route) {
			used[route.RuntimeID] = true
			used[route.NativeRuntimeID] = true
		}
	}
	for id := range used {
		if _, ok := acpAgentFor(id); ok && getStr(bkHarnessConnections, id) == "" {
			_ = putStoreJSON(bkHarnessConnections, id, harnessConnection{Connected: true, At: time.Now().UnixMilli()})
		}
	}
}

func (a *acpAdapter) Connect(ctx context.Context, consent bool) (any, error) {
	if !consent {
		return nil, errors.New("confirm connecting this harness and reading its native catalog")
	}
	p := refreshACPProbe(ctx, a.agent)
	if p.Error != "" {
		return nil, errors.New(p.Error)
	}
	if err := runtimeVaultError(); err != nil {
		return nil, err
	}
	if err := putStoreJSON(bkHarnessConnections, a.agent.ID, harnessConnection{Connected: true, At: time.Now().UnixMilli()}); err != nil {
		return nil, errors.New("harness connection could not be saved")
	}
	return p.Config, nil
}

func handleHarnessDisconnect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	id := r.PathValue("id")
	if _, ok := acpAgentFor(id); !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness not found"})
		return
	}
	var req struct{}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := putStoreJSON(bkHarnessConnections, id, harnessConnection{At: time.Now().UnixMilli()}); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "connection could not be saved"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// Return only the native login command; OAuth and credentials remain owned by
// the harness. This does not inspect or copy a native credential file.
func handleHarnessLogin(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	agent, ok := acpAgentFor(r.PathValue("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness not found"})
		return
	}
	command, note := "", ""
	switch usageHarnessID(agent) {
	case "claude-code":
		command = "claude auth login"
	case "codex":
		command = "codex login --device-auth"
	case "opencode":
		command = "opencode auth login"
	case "pi":
		command = "pi"
		note = "Use /login in the native terminal."
	case "hermes":
		command = "hermes setup model"
		note = "Choose the provider and complete its native account setup in the terminal."
	case "antigravity":
		command = "agy"
		note = "Complete the native sign-in flow in the terminal."
	}
	target := workspaceTarget(agent)
	if command == "" || target == "" {
		sendJSON(w, 501, map[string]any{"ok": false, "error": "native account login is not available for this custom launcher; use its own CLI"})
		return
	}
	if target == "local" && runtime.GOOS != "windows" {
		// Prefer the same user-installed executable as installation/inspection,
		// even when the service or login shell has an older PATH.
		binary := strings.Fields(command)[0]
		path, err := lifecycleLookPath(binary)
		if err != nil {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "Install the native CLI on this machine before signing in: " + binary})
			return
		}
		command = shellQuote(path) + strings.TrimPrefix(command, binary)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "target": target, "command": command, "title": agent.Name + " · account", "note": note})
}
