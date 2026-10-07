package loom

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type harnessHistorySource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Machine string `json:"machine"`
}

func historySource(id string) (acpAgent, error) {
	if strings.HasPrefix(id, "local:") {
		a, ok := acpAgentFor(strings.TrimPrefix(id, "local:"))
		if ok && !a.Remote && a.available() {
			return a, nil
		}
	} else {
		parts := strings.Split(id, ":")
		if len(parts) == 3 && parts[0] == "remote" {
			for _, m := range loadRemoteMachines() {
				if m.ID != parts[1] {
					continue
				}
				for _, offer := range remoteOffers(m) {
					if offer["id"] == parts[2] && offer["ready"] == true {
						key, _, err := loomSSHKey()
						if err != nil {
							return acpAgent{}, err
						}
						return remoteAgent(m, parts[2], key)
					}
				}
			}
		}
	}
	return acpAgent{}, errors.New("installed history source not found; refresh this machine first")
}
func harnessHistorySources() []harnessHistorySource {
	out := []harnessHistorySource{}
	for _, d := range registeredRuntimes.catalog() {
		adapter, _ := registeredRuntimes.lookup(d.ID)
		if acp, ok := adapter.(*acpAdapter); ok && !acp.agent.Remote && acp.agent.available() && (acp.agent.Custom || harnessManaged("local", d.ID)) {
			out = append(out, harnessHistorySource{"local:" + d.ID, d.Name, "local"})
		}
	}
	for _, m := range loadRemoteMachines() {
		for _, offer := range remoteOffers(m) {
			if id, _ := offer["id"].(string); offer["ready"] == true && harnessManaged(m.ID, id) {
				name, _ := offer["name"].(string)
				out = append(out, harnessHistorySource{"remote:" + m.ID + ":" + id, name, m.Name})
			}
		}
	}
	return out
}

// Transfer uses native list/load only on the source. The destination gets a
// fresh Loom discussion routed to that harness, with portable text on its first
// user-initiated turn. No native database/auth/approval files are copied.
func transferHarnessHistory(ctx context.Context, source acpAgent, info acpSessionInfo, choiceID, projectID string) (RuntimeSession, error) {
	var target ModelChoice
	for _, c := range modelCatalog(workspaceSessions.providers()) {
		if c.ID == choiceID && c.Kind == "harness" && c.Ready && c.Enabled {
			target = c
			break
		}
	}
	if target.ID == "" {
		return RuntimeSession{}, errors.New("choose a connected, enabled destination harness model")
	}
	for _, s := range workspaceSessions.list() {
		if s.ImportSource != nil && s.ImportSource.TargetChoice == choiceID && s.ImportSource.RuntimeID == source.ID && s.ImportSource.MachineID == source.Machine && s.ImportSource.SessionID == info.SessionID {
			full, _ := workspaceSessions.get(s.ID)
			return full, nil
		}
	}
	original, err := readNativeACPSession(ctx, source, info, projectID, true)
	if err != nil {
		return RuntimeSession{}, err
	}
	s := cloneRuntimeSession(original)
	s.ID = newSessionID()
	s.ProjectID = projectID
	s.ImportSource.TargetChoice = choiceID
	s.NativeArchive, s.SourceArchive = "", ""
	s.ACPState = ACPState{Permission: "ask"}
	s.Instructions, s.LastRequestID, s.RequestIDs = "", "", nil
	s.Workdir, s.WorkspaceID, s.WorkspaceTarget = "", "", ""
	s.AdditionalDirs = nil
	s.Status, s.Error = "idle", ""
	s.CreatedAt = time.Now().UnixMilli()
	s.UpdatedAt = s.CreatedAt
	s.RuntimeID = "llama.cpp" // selectModel resolves the target machine/workspace.
	if p := prepareDiscussion(s, "Continue"); p.Problem != "" {
		return RuntimeSession{}, errors.New(p.Problem)
	}
	if err = putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		return RuntimeSession{}, err
	}
	selected, err := workspaceSessions.selectModelContext(ctx, s.ID, choiceID, true)
	if err != nil {
		_ = putBytes(bkRuntimeSessions, s.ID, nil)
		return RuntimeSession{}, err
	}
	return selected, nil
}

var historyTransferGate = make(chan struct{}, 1)

func handleHarnessHistory(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	id := r.URL.Query().Get("source")
	if id == "" {
		sendJSON(w, 200, map[string]any{"ok": true, "sources": harnessHistorySources()})
		return
	}
	a, err := historySource(id)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	rows, err := listACPSessions(ctx, a)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sessions": rows})
}
func handleHarnessTransfer(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		Source  string `json:"source"`
		Choice  string `json:"choice_id"`
		Project string `json:"project_id"`
		Consent bool   `json:"consent"`
		acpSessionInfo
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !req.Consent || req.SessionID == "" || len(req.SessionID) > 200 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "session and explicit transcript-sharing consent required"})
		return
	}
	a, err := historySource(req.Source)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	select {
	case historyTransferGate <- struct{}{}:
		defer func() { <-historyTransferGate }()
	default:
		sendJSON(w, 409, map[string]any{"ok": false, "error": "another history transfer is in progress"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	s, err := transferHarnessHistory(ctx, a, req.acpSessionInfo, req.Choice, req.Project)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s), "native_materialized": false})
}
