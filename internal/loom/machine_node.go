package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Machine maintenance links are independent of the active inference engine.
// Credentials use the same protected store as engine_node, never machine JSON.
const machineNodePrefix = "machine_engine_node:"

func savedMachineNode(m RemoteMachine) *engineNode {
	var n engineNode
	if getStoreJSON(bkState, machineNodePrefix+m.ID, &n) && n.URL != "" {
		return &n
	}
	// Existing active links need no second key entry. Persisted maintenance links
	// remain usable after another engine is selected.
	if active := currentEngineNode(); active != nil && !active.Direct && active.Role == "engine-node" {
		u, err := url.Parse(active.URL)
		if err == nil && (u.Hostname() == m.Host || active.Hostname == m.Hostname && m.Hostname != "") {
			return active
		}
	}
	return nil
}

func nodeMaintenanceProbe(ctx context.Context, rawURL, key string) (*engineNode, error) {
	base, _, err := cleanNodeURL(rawURL)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return nil, errors.New("valid engine node control key required")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/node/info", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, errors.New("engine node unreachable")
	}
	defer resp.Body.Close()
	var info struct {
		ID        string   `json:"id"`
		Modules   []string `json:"modules"`
		Handshake int      `json:"handshake"`
		OK        bool     `json:"ok"`
		Hostname  string   `json:"hostname"`
		Version   string   `json:"version"`
		Role      string   `json:"role"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info) != nil || !info.OK || info.Role != "engine-node" {
		return nil, errors.New("engine node rejected the key or does not support maintenance")
	}
	if err := checkNodeHandshake(info.Handshake); err != nil {
		return nil, err
	}
	return &engineNode{NodeID: info.ID, Modules: info.Modules, Handshake: info.Handshake, URL: base, WebKey: key, Hostname: info.Hostname, Version: info.Version, Role: info.Role, LinkedAt: time.Now().UnixMilli()}, nil
}

func machineForNode(w http.ResponseWriter, r *http.Request) (RemoteMachine, bool) {
	for _, m := range loadRemoteMachines() {
		if m.ID == r.PathValue("id") {
			return m, true
		}
	}
	sendJSON(w, 404, map[string]any{"ok": false, "error": "machine not found"})
	return RemoteMachine{}, false
}

func handleMachineNode(w http.ResponseWriter, r *http.Request) {
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
	if r.Method == http.MethodPost {
		var req struct {
			URL    string `json:"url"`
			Key    string `json:"key"`
			Unlink bool   `json:"unlink"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		var err error
		if req.Unlink {
			err = putBytes(bkState, machineNodePrefix+m.ID, nil)
		} else {
			var n *engineNode
			n, err = nodeMaintenanceProbe(r.Context(), req.URL, req.Key)
			if err != nil {
				sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if !usageVaultAccess(w) {
				return
			}
			m.NodeID, m.Modules, m.Handshake = n.NodeID, n.Modules, n.Handshake
			m, err = savePairedMachine(m, n)
		}
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "maintenance link could not be saved"})
			return
		}
	}
	n := savedMachineNode(m)
	if n == nil {
		sendJSON(w, 200, map[string]any{"ok": true, "linked": false})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "linked": true, "url": n.URL, "hostname": n.Hostname, "version": n.Version, "role": n.Role, "modules": n.Modules, "handshake": n.Handshake})
}

func handleMachineNodeUpdate(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	n := savedMachineNode(m)
	if n == nil || n.Direct || n.Role != "engine-node" {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "link this machine's engine node first"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/machines/"+m.ID+"/node/update")
	switch path {
	case "":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		path = "/api/update"
	case "/ping":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		path = "/api/ping"
	case "/apply":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		path = "/api/update/apply"
	default:
		http.NotFound(w, r)
		return
	}
	cloned := r.Clone(r.Context())
	copied := *r.URL
	cloned.URL = &copied
	cloned.URL.Path, cloned.URL.RawPath, cloned.URL.RawQuery = path, "", "channel="+updateChannel()
	proxyEngineNode(w, cloned, n)
}
