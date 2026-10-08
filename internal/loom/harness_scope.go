package loom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// Agents found on each machine have two independent choices:
//   - managed: Loom follows the agent there (account, versions and updates,
//     history import, usage). On by default on Loom's own machine; on other
//     machines only once the user chooses it (or already uses the agent).
//   - enabled: the agent can run Loom discussions (the existing connection on
//     this machine, or the registered remote agent on another one). Enabling
//     implies managing.
const harnessScopeKey = "harness_scope" // map["<machine>:<harness>"]managed

type agentInstallation struct {
	Machine     string `json:"machine"` // "local" or a machine id
	MachineName string `json:"machine_name"`
	Harness     string `json:"harness"` // family id (codex, claude-code…)
	Name        string `json:"name"`
	Logo        string `json:"logo"`
	RuntimeID   string `json:"runtime_id"` // the Loom runtime that drives it
	Installed   bool   `json:"installed"`
	Ready       bool   `json:"ready"`
	Version     string `json:"version,omitempty"`
	Managed     bool   `json:"managed"`
	Enabled     bool   `json:"enabled"`
}

func remoteRuntimeID(machine, harness string) string { return "custom-" + machine + "-" + harness }

func harnessManaged(machine, harness string) bool {
	scope := map[string]bool{}
	if getStoreJSON(bkState, harnessScopeKey, &scope) {
		if v, ok := scope[machine+":"+harness]; ok {
			return v
		}
	}
	if machine == "local" {
		return true
	}
	for _, m := range loadRemoteMachines() {
		if m.ID == machine {
			for _, h := range m.Harnesses {
				if h == remoteRuntimeID(machine, harness) {
					return true
				}
			}
		}
	}
	return false
}

func setHarnessManaged(machine, harness string, managed bool) error {
	scope := map[string]bool{}
	_ = getStoreJSON(bkState, harnessScopeKey, &scope)
	scope[machine+":"+harness] = managed
	return putStoreJSON(bkState, harnessScopeKey, scope)
}

// agentInstallations lists every known agent on every machine, from cached
// detection only (no SSH round trip).
func agentInstallations() []agentInstallation {
	out := []agentInstallation{}
	host, _ := os.Hostname()
	for _, d := range registeredRuntimes.catalog() {
		adapter, _ := registeredRuntimes.lookup(d.ID)
		acp, ok := adapter.(*acpAdapter)
		if !ok || acp.agent.Remote || acp.agent.Custom || d.Kind != "harness" {
			continue
		}
		installed := acp.agent.available()
		managed := harnessManaged("local", d.ID)
		out = append(out, agentInstallation{Machine: "local", MachineName: host, Harness: d.ID, Name: d.Name, Logo: d.ID, RuntimeID: d.ID,
			Installed: installed, Ready: installed, Managed: managed, Enabled: managed && installed && harnessConnected(acp.agent)})
	}
	for _, m := range loadRemoteMachines() {
		for _, offer := range remoteOffers(m) {
			id, _ := offer["id"].(string)
			name, _ := offer["name"].(string)
			logo, _ := offer["logo"].(string)
			installed, _ := offer["installed"].(bool)
			ready, _ := offer["ready"].(bool)
			version, _ := offer["version"].(string)
			enabled := false
			for _, h := range m.Harnesses {
				enabled = enabled || h == remoteRuntimeID(m.ID, id)
			}
			out = append(out, agentInstallation{Machine: m.ID, MachineName: m.Name, Harness: id, Name: name, Logo: logo, RuntimeID: remoteRuntimeID(m.ID, id),
				Installed: installed, Ready: ready, Version: strings.TrimSpace(version), Managed: enabled || harnessManaged(m.ID, id), Enabled: enabled})
		}
	}
	return out
}

// GET /api/agents/installations; POST {machine, harness, managed}.
func handleAgentInstallations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "installations": agentInstallations()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Machine string `json:"machine"`
		Harness string `json:"harness"`
		Managed *bool  `json:"managed"`
		Enabled *bool  `json:"enabled"`
		Consent bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	known := false
	for _, i := range agentInstallations() {
		known = known || i.Machine == req.Machine && i.Harness == req.Harness
	}
	if !known {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "agent not found on this machine"})
		return
	}
	managed, enabled := req.Managed, req.Enabled
	// Enabling implies managing; no longer managing implies disabling.
	if enabled != nil && *enabled {
		on := true
		managed = &on
	}
	if managed != nil && !*managed {
		off := false
		enabled = &off
	}
	if managed != nil {
		if err := setHarnessManaged(req.Machine, req.Harness, *managed); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	if enabled != nil {
		var err error
		switch {
		case req.Machine == "local" && *enabled:
			if !req.Consent {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "confirm sharing discussions with this agent (consent:true)"})
				return
			}
			err = putStoreJSON(bkHarnessConnections, req.Harness, harnessConnection{Connected: true, At: time.Now().UnixMilli()})
		case req.Machine == "local":
			err = putStoreJSON(bkHarnessConnections, req.Harness, harnessConnection{At: time.Now().UnixMilli()})
		default:
			err = setRemoteHarness(req.Machine, req.Harness, *enabled)
		}
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "installations": agentInstallations()})
}

// setRemoteHarness registers or removes one agent of a known machine without
// re-checking the machine (its cached description is enough).
func setRemoteHarness(machine, harness string, enabled bool) error {
	var m RemoteMachine
	found := false
	for _, x := range loadRemoteMachines() {
		if x.ID == machine {
			m, found = x, true
		}
	}
	if !found {
		return errors.New("machine not found")
	}
	id := remoteRuntimeID(machine, harness)
	agents := []acpAgent{}
	for _, a := range loadCustomACPAgents() {
		if a.Machine == machine && a.ID != id {
			agents = append(agents, a)
		}
	}
	if enabled {
		key := ""
		if !usesNodeHarness(m) {
			var err error
			key, _, err = loomSSHKey()
			if err != nil {
				return err
			}
		}
		a, err := remoteAgent(m, harness, key)
		if err != nil {
			return err
		}
		agents = append(agents, a)
		defer func() { go refreshACPProbe(context.Background(), a) }()
	}
	return saveRemoteMachine(m, agents)
}
