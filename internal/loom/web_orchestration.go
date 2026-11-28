package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Authenticated web routes for Loom's subagent orchestration tree, shared
// team board, settings, and live SSE updates.

func registerOrchestrationRoutes(mux *http.ServeMux, api func(string, http.HandlerFunc)) {
	api("/api/orchestration/state", handleOrchestrationState)
	api("/api/orchestration/catalog", handleOrchestrationCatalog)
	api("/api/orchestration/settings", handleOrchestrationSettings)
	api("/api/orchestration/board", handleOrchestrationBoard)
	api("/api/orchestration/spawn", handleOrchestrationSpawn)
	api("/api/orchestration/reparent", handleOrchestrationReparent)
	api("/api/orchestration/prune-stopped", handleOrchestrationPruneStopped)
	api("/api/orchestration/stop-tree", handleOrchestrationStopTree)
	api("/api/orchestration/cleanup-worktree", handleOrchestrationCleanupWorktree)
	mux.HandleFunc("/api/orchestration/events", requireWebAuth(handleOrchestrationEvents))
}

func handleOrchestrationCatalog(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	cat := buildSubagentCatalog(r.Context())
	sendJSON(w, 200, map[string]any{"ok": true, "catalog": cat})
}

func handleOrchestrationState(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	// Best-effort quick sync with Codeman so deleted sessions mark stopped
	// and live sessions always have their 0600 bridge file ready.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	if live, err := codemanListSessions(ctx); err == nil {
		ids := make(map[string]bool, len(live))
		for _, s := range live {
			ids[s.ID] = true
		}
		orchSyncWithLive(ids)
		orchEnsureBridgeFiles(live)
	}
	cancel()
	sendJSON(w, 200, map[string]any{
		"ok":           true,
		"nodes":        orchAllNodes(),
		"board_counts": orchBoardCounts(),
		"settings":     loadOrchestrationSettings(),
	})
}

func handleOrchestrationReparent(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		ParentID  string `json:"parent_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	liveMap := map[string]codemanSessionInfo{}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	if live, err := codemanListSessions(ctx); err == nil {
		for _, s := range live {
			liveMap[s.ID] = s
		}
	}
	cancel()
	node, err := orchReparent(req.SessionID, req.ParentID, liveMap)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "node": node})
}

func handleOrchestrationPruneStopped(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		RootID           string `json:"root_id"`
		CleanupWorktrees bool   `json:"cleanup_worktrees"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	rootID := strings.TrimSpace(req.RootID)
	if rootID != "" {
		rootID = orchResolveScope(rootID).RootID
	}
	removed := orchPruneStopped(r.Context(), rootID, req.CleanupWorktrees)
	sendJSON(w, 200, map[string]any{"ok": true, "removed": removed})
}

func handleOrchestrationSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendJSON(w, 200, map[string]any{"ok": true, "settings": loadOrchestrationSettings()})
	case http.MethodPost:
		var req OrchestrationSettings
		if !workspaceDecode(w, r, &req) {
			return
		}
		saved, err := saveOrchestrationSettings(req)
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "settings": saved})
	default:
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
	}
}

func handleOrchestrationBoard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		root := strings.TrimSpace(r.URL.Query().Get("root"))
		since := strings.TrimSpace(r.URL.Query().Get("since"))
		if root == "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "root is required"})
			return
		}
		// Allow passing any session in the tree — resolve to its root.
		scope := orchResolveScope(root)
		msgs := orchReadBoard(scope.RootID, since, 100)
		sendJSON(w, 200, map[string]any{"ok": true, "root_id": scope.RootID, "messages": msgs})
	case http.MethodPost:
		var req struct {
			RootID    string `json:"root_id"`
			ToSession string `json:"to_session"`
			Text      string `json:"text"`
			Kind      string `json:"kind"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		scope := orchResolveScope(req.RootID)
		if scope.RootID == "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "root_id is required"})
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = "human"
		}
		msg, err := orchPostBoard(BoardMessage{
			RootID:    scope.RootID,
			FromRole:  "human",
			ToSession: strings.TrimSpace(req.ToSession),
			Kind:      kind,
			Text:      req.Text,
		})
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "message": msg})
	default:
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
	}
}

func handleOrchestrationSpawn(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var args subagentSpawnArgs
	if !workspaceDecode(w, r, &args) {
		return
	}
	if strings.TrimSpace(args.ParentID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "parent_id is required"})
		return
	}
	// Derive working_dir strictly from the parent Codeman session / registered node,
	// never from an untrusted HTTP path parameter.
	args.WorkingDir = ""
	caller := orchResolveScope(args.ParentID)
	res, err := spawnLoomSubagent(r.Context(), caller, args)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "subagent": res})
}

func handleOrchestrationStopTree(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		SessionID        string `json:"session_id"`
		Mode             string `json:"mode"` // "close" (default) | "interrupt"
		IncludeRoot      bool   `json:"include_root"`
		CleanupWorktrees bool   `json:"cleanup_worktrees"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	sid := strings.TrimSpace(req.SessionID)
	if sid == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "session_id is required"})
		return
	}
	targets := orchDescendants(sid, false)
	if req.IncludeRoot {
		if rootNode, ok := orchGetNode(sid); ok && rootNode.Status != "stopped" {
			targets = append(targets, rootNode)
		}
	}
	// Stop deepest children first so parent sees a clean shutdown.
	stopped := []string{}
	errs := []string{}
	for i := len(targets) - 1; i >= 0; i-- {
		node := targets[i]
		if strings.ToLower(strings.TrimSpace(req.Mode)) == "interrupt" {
			if err := codemanInterruptSession(r.Context(), node.SessionID); err != nil {
				errs = append(errs, node.SessionID+": "+err.Error())
				continue
			}
			stopped = append(stopped, node.SessionID)
			continue
		}
		if err := codemanDeleteSession(r.Context(), node.SessionID, true); err != nil {
			low := strings.ToLower(err.Error())
			if !strings.Contains(low, "not_found") && !strings.Contains(low, "not found") {
				errs = append(errs, node.SessionID+": "+err.Error())
				continue
			}
		}
		orchMarkStopped(node.SessionID)
		stopped = append(stopped, node.SessionID)
		if req.CleanupWorktrees && node.WorktreePath != "" && node.BaseRepo != "" {
			if err := removeLoomWorktree(r.Context(), node.BaseRepo, node.WorktreePath, false); err == nil {
				orchClearWorktree(node.SessionID)
			}
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "stopped": stopped, "errors": errs})
}

func handleOrchestrationCleanupWorktree(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Force     bool   `json:"force"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	node, ok := orchGetNode(req.SessionID)
	if !ok || node.WorktreePath == "" || node.BaseRepo == "" {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "no worktree recorded for this session"})
		return
	}
	if err := removeLoomWorktree(r.Context(), node.BaseRepo, node.WorktreePath, req.Force); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	orchClearWorktree(node.SessionID)
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleOrchestrationEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeEvt := func(name string, payload any) bool {
		raw, err := json.Marshal(payload)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	ch := orchHub.subscribe()
	defer orchHub.unsubscribe(ch)
	if !writeEvt("init", map[string]any{
		"nodes":        orchAllNodes(),
		"board_counts": orchBoardCounts(),
		"settings":     loadOrchestrationSettings(),
	}) {
		return
	}

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-ch:
			if !writeEvt(ev.Name, ev.Data) {
				return
			}
		}
	}
}
