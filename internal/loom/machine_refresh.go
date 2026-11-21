package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"
)

// A node announces its modules (engine, voice, terminal, files…) when it is
// paired, but it can gain some later: an update adds a module, the user
// enables voice or terminals there. Loom reads the node's identity again
// at most once a minute while the machines are listed, so the bricks a
// machine can provide follow the node without pairing it again.

const machineRefreshEvery = time.Minute

var machineRefresh = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

type nodeIdentity struct {
	Modules   []string `json:"modules"`
	Handshake int      `json:"handshake"`
	Version   string   `json:"version"`
}

func refreshNodeMachines(ctx context.Context) {
	due := []RemoteMachine{}
	machineRefresh.Lock()
	for _, m := range loadRemoteMachines() {
		if m.NodeID != "" && time.Since(machineRefresh.at[m.ID]) >= machineRefreshEvery {
			machineRefresh.at[m.ID] = time.Now()
			due = append(due, m)
		}
	}
	machineRefresh.Unlock()
	if len(due) == 0 {
		return
	}
	found := make([]*nodeIdentity, len(due))
	var wg sync.WaitGroup
	for i, m := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			var info nodeIdentity
			if nodeMachineJSON(c, m, "GET", "/api/node/info", nil, &info) == nil && len(info.Modules) > 0 {
				found[i] = &info
			}
		}()
	}
	wg.Wait()
	for i, m := range due {
		if found[i] != nil {
			updateMachineNode(m.ID, *found[i])
			continue
		}
		// Unreachable (restarting after an update, asleep): try again soon.
		machineRefresh.Lock()
		machineRefresh.at[m.ID] = time.Now().Add(15*time.Second - machineRefreshEvery)
		machineRefresh.Unlock()
	}
}

// updateMachineNode records what the node announced, when it changed.
func updateMachineNode(id string, info nodeIdentity) {
	machines := loadRemoteMachines()
	changed := false
	for i := range machines {
		if machines[i].ID == id && (!slices.Equal(machines[i].Modules, info.Modules) || info.Handshake != 0 && machines[i].Handshake != info.Handshake) {
			machines[i].Modules = slices.Clone(info.Modules)
			if info.Handshake != 0 {
				machines[i].Handshake = info.Handshake
			}
			changed = true
		}
	}
	if changed {
		_ = putStoreJSON(bkState, remoteMachinesState, machines)
	}
	var n engineNode
	if getStoreJSON(bkState, machineNodePrefix+id, &n) && n.URL != "" && (!slices.Equal(n.Modules, info.Modules) || info.Version != "" && n.Version != info.Version) {
		n.Modules = slices.Clone(info.Modules)
		if info.Version != "" {
			n.Version = info.Version
		}
		_ = putStoreJSON(bkState, machineNodePrefix+id, n)
	}
}

// forgetMachineRefresh makes the next listing read the node again (after an update).
func forgetMachineRefresh(id string) {
	machineRefresh.Lock()
	delete(machineRefresh.at, id)
	machineRefresh.Unlock()
}

// Node modules are switched from the main Loom, never on the node itself.
var nodeModuleKeys = map[string]string{
	"voice":    "node_voice_disabled",
	"terminal": nodeTerminalDisabledKey, // terminals and files share this trust
	"observe":  nodeObserveDisabledKey,
}

// handleNodeModules runs on a node: POST {module, enabled}.
func handleNodeModules(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Module  string `json:"module"`
		Enabled *bool  `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "enabled is required"})
		return
	}
	key, ok := nodeModuleKeys[req.Module]
	if !ok {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown module"})
		return
	}
	if err := putBool(bkState, key, !*req.Enabled); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "modules": nodeModules()})
}

// handleMachineNodeModules runs on the main Loom and forwards the switch.
func handleMachineNodeModules(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	var req struct {
		Module  string `json:"module"`
		Enabled *bool  `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "enabled is required"})
		return
	}
	if _, known := nodeModuleKeys[req.Module]; !known {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown module"})
		return
	}
	body, _ := json.Marshal(req)
	var out struct {
		OK      bool     `json:"ok"`
		Error   string   `json:"error"`
		Modules []string `json:"modules"`
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := nodeMachineJSON(ctx, m, http.MethodPost, "/api/node/modules", bytes.NewReader(body), &out); err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !out.OK || len(out.Modules) == 0 {
		msg := out.Error
		if msg == "" {
			msg = "update this machine's Loom to change its modules from here"
		}
		sendJSON(w, 409, map[string]any{"ok": false, "error": msg})
		return
	}
	updateMachineNode(m.ID, nodeIdentity{Modules: out.Modules})
	sendJSON(w, 200, map[string]any{"ok": true, "modules": out.Modules})
}
