package loom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Orchestration metadata, scoped per-session MCP tokens, shared coordination
// board, and live SSE event fan-out for Loom's subagent tree.
//
// Codeman owns CLI processes and tmux panes; Loom persists lineage, worktree
// metadata, guardrails, scoped tokens and inter-agent board messages in BoltDB
// (bkState) so they survive Loom restarts and stay decoupled from Codeman's DB.

const (
	orchestrationNodesKey    = "orchestration_nodes"
	orchestrationBoardKey    = "orchestration_board"
	orchestrationTokensKey   = "orchestration_tokens"
	orchestrationSettingsKey = "orchestration_settings"

	orchMaxBoardMessagesPerRoot = 200
	orchMaxBoardTextLen         = 16 << 10
	orchDefaultMaxDepth         = 2
	orchDefaultMaxChildren      = 6
)

type OrchestrationSettings struct {
	AutoInjectMCP        bool   `json:"auto_inject_mcp"`
	AutoWriteWorktreeMCP bool   `json:"auto_write_worktree_mcp"`
	MaxDepth             int    `json:"max_depth"`
	MaxChildrenPerRoot   int    `json:"max_children_per_root"`
	SubagentPermissions  string `json:"subagent_permissions"` // "inherit" | "worktree_auto" | "full_access"
}

func defaultOrchestrationSettings() OrchestrationSettings {
	return OrchestrationSettings{
		AutoInjectMCP:        true,
		AutoWriteWorktreeMCP: true,
		MaxDepth:             orchDefaultMaxDepth,
		MaxChildrenPerRoot:   orchDefaultMaxChildren,
		SubagentPermissions:  "inherit",
	}
}

func clampOrchestrationSettings(s OrchestrationSettings) OrchestrationSettings {
	if s.MaxDepth < 1 {
		s.MaxDepth = 1
	} else if s.MaxDepth > 3 {
		s.MaxDepth = 3
	}
	if s.MaxChildrenPerRoot < 1 {
		s.MaxChildrenPerRoot = 1
	} else if s.MaxChildrenPerRoot > 16 {
		s.MaxChildrenPerRoot = 16
	}
	switch strings.ToLower(strings.TrimSpace(s.SubagentPermissions)) {
	case "inherit", "worktree_auto", "full_access":
		s.SubagentPermissions = strings.ToLower(strings.TrimSpace(s.SubagentPermissions))
	default:
		s.SubagentPermissions = "inherit"
	}
	return s
}

type OrchestrationNode struct {
	SessionID     string `json:"session_id"`
	ParentID      string `json:"parent_id,omitempty"`
	RootID        string `json:"root_id"`
	Depth         int    `json:"depth"`
	Role          string `json:"role,omitempty"`
	Name          string `json:"name,omitempty"`
	CLI           string `json:"cli,omitempty"`
	Model         string `json:"model,omitempty"`
	Effort        string `json:"effort,omitempty"`
	WorkspaceMode string `json:"workspace_mode,omitempty"` // "inherit" | "worktree"
	WorkingDir    string `json:"working_dir,omitempty"`
	BaseRepo      string `json:"base_repo,omitempty"`
	WorktreePath  string `json:"worktree_path,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Status        string `json:"status"` // "active" | "stopped"
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	StoppedAt     int64  `json:"stopped_at,omitempty"`
}

type BoardMessage struct {
	ID          string `json:"id"`
	RootID      string `json:"root_id"`
	FromSession string `json:"from_session,omitempty"`
	FromRole    string `json:"from_role,omitempty"`
	ToSession   string `json:"to_session,omitempty"`
	Kind        string `json:"kind"` // "update" | "question" | "result" | "handoff" | "human"
	Text        string `json:"text"`
	CreatedAt   int64  `json:"created_at"`
}

type SessionScope struct {
	SessionID  string `json:"session_id"`
	ParentID   string `json:"parent_id,omitempty"`
	RootID     string `json:"root_id"`
	Depth      int    `json:"depth"`
	WorkingDir string `json:"working_dir,omitempty"`
}

type sessionTokenRecord struct {
	Scope     SessionScope `json:"scope"`
	CreatedAt int64        `json:"created_at"`
}

type sessionScopeCtxKey struct{}

func withSessionScope(ctx context.Context, scope SessionScope) context.Context {
	return context.WithValue(ctx, sessionScopeCtxKey{}, scope)
}

func sessionScopeFromContext(ctx context.Context) (SessionScope, bool) {
	if ctx == nil {
		return SessionScope{}, false
	}
	sc, ok := ctx.Value(sessionScopeCtxKey{}).(SessionScope)
	return sc, ok && strings.TrimSpace(sc.SessionID) != ""
}

type orchEvent struct {
	Name string
	Data any
}

type orchEventHub struct {
	mu   sync.Mutex
	subs map[chan orchEvent]struct{}
}

func (h *orchEventHub) subscribe() chan orchEvent {
	ch := make(chan orchEvent, 32)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[chan orchEvent]struct{}{}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *orchEventHub) unsubscribe(ch chan orchEvent) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *orchEventHub) publish(name string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- orchEvent{Name: name, Data: data}:
		default:
		}
	}
}

var (
	orchMu  sync.Mutex
	orchHub orchEventHub
)

func loadOrchestrationSettings() OrchestrationSettings {
	orchMu.Lock()
	defer orchMu.Unlock()
	return loadOrchestrationSettingsLocked()
}

func loadOrchestrationSettingsLocked() OrchestrationSettings {
	s := defaultOrchestrationSettings()
	if raw := getBytes(bkState, orchestrationSettingsKey); len(raw) > 0 {
		var saved OrchestrationSettings
		if json.Unmarshal(raw, &saved) == nil {
			s = clampOrchestrationSettings(saved)
		}
	}
	return s
}

func saveOrchestrationSettings(s OrchestrationSettings) (OrchestrationSettings, error) {
	s = clampOrchestrationSettings(s)
	orchMu.Lock()
	defer orchMu.Unlock()
	if err := putStoreJSON(bkState, orchestrationSettingsKey, s); err != nil {
		return s, err
	}
	orchHub.publish("settings", s)
	return s, nil
}

func loadOrchNodesLocked() map[string]OrchestrationNode {
	out := map[string]OrchestrationNode{}
	getStoreJSON(bkState, orchestrationNodesKey, &out)
	return out
}

func saveOrchNodesLocked(nodes map[string]OrchestrationNode) error {
	return putStoreJSON(bkState, orchestrationNodesKey, nodes)
}

func loadOrchBoardLocked() map[string][]BoardMessage {
	out := map[string][]BoardMessage{}
	getStoreJSON(bkState, orchestrationBoardKey, &out)
	return out
}

func saveOrchBoardLocked(board map[string][]BoardMessage) error {
	return putStoreJSON(bkState, orchestrationBoardKey, board)
}

func loadOrchTokensLocked() map[string]sessionTokenRecord {
	out := map[string]sessionTokenRecord{}
	getStoreJSON(bkState, orchestrationTokensKey, &out)
	return out
}

func saveOrchTokensLocked(tokens map[string]sessionTokenRecord) error {
	return putStoreJSON(bkState, orchestrationTokensKey, tokens)
}

func randomHexID(nbytes int) string {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b)
}

// mintSessionToken creates an opaque bearer token bound to a single session's
// orchestration scope. Only its hash is stored in BoltDB.
func mintSessionToken(scope SessionScope) (string, error) {
	scope.SessionID = strings.TrimSpace(scope.SessionID)
	if scope.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	if strings.TrimSpace(scope.RootID) == "" {
		scope.RootID = scope.SessionID
	}
	return storeSessionTokenRecord(scope)
}

// mintPendingSessionToken reserves a scoped bearer token before Codeman assigns
// the session ID in POST /api/v1/sessions, then bindSessionToken attaches the
// real session_id before POST /sessions/:id/interactive launches the CLI.
func mintPendingSessionToken(parentID, rootID string, depth int, workingDir string) (string, error) {
	return storeSessionTokenRecord(SessionScope{
		SessionID:  "pending_" + randomHexID(8),
		ParentID:   strings.TrimSpace(parentID),
		RootID:     strings.TrimSpace(rootID),
		Depth:      depth,
		WorkingDir: strings.TrimSpace(workingDir),
	})
}

func bindSessionToken(rawToken, sessionID, rootID string) {
	rawToken = strings.TrimSpace(rawToken)
	sessionID = strings.TrimSpace(sessionID)
	if rawToken == "" || sessionID == "" {
		return
	}
	hash := hashWebKey(rawToken)
	orchMu.Lock()
	defer orchMu.Unlock()
	tokens := loadOrchTokensLocked()
	rec, ok := tokens[hash]
	if !ok {
		return
	}
	rec.Scope.SessionID = sessionID
	if strings.TrimSpace(rootID) != "" {
		rec.Scope.RootID = strings.TrimSpace(rootID)
	} else if rec.Scope.RootID == "" || strings.HasPrefix(rec.Scope.RootID, "pending_") {
		rec.Scope.RootID = sessionID
	}
	tokens[hash] = rec
	_ = saveOrchTokensLocked(tokens)
}

func deleteRawSessionToken(rawToken string) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return
	}
	hash := hashWebKey(rawToken)
	orchMu.Lock()
	defer orchMu.Unlock()
	tokens := loadOrchTokensLocked()
	if _, ok := tokens[hash]; ok {
		delete(tokens, hash)
		_ = saveOrchTokensLocked(tokens)
	}
}

func storeSessionTokenRecord(scope SessionScope) (string, error) {
	raw := "lm_ses_" + randomHexID(24)
	hash := hashWebKey(raw)

	orchMu.Lock()
	defer orchMu.Unlock()
	tokens := loadOrchTokensLocked()
	// Bound stored tokens so long-lived installs never accumulate dead entries.
	if len(tokens) > 512 {
		cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
		for k, v := range tokens {
			if v.CreatedAt < cutoff {
				delete(tokens, k)
			}
		}
	}
	tokens[hash] = sessionTokenRecord{Scope: scope, CreatedAt: time.Now().Unix()}
	if err := saveOrchTokensLocked(tokens); err != nil {
		return "", err
	}
	return raw, nil
}

// lookupScopedBearer checks whether the Authorization Bearer token matches an
// active scoped per-session token minted by Loom.
func lookupScopedBearer(r *http.Request) (SessionScope, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 8 || !strings.EqualFold(auth[:7], "Bearer ") {
		return SessionScope{}, false
	}
	tok := strings.TrimSpace(auth[7:])
	if !strings.HasPrefix(tok, "lm_ses_") {
		return SessionScope{}, false
	}
	hash := hashWebKey(tok)
	orchMu.Lock()
	tokens := loadOrchTokensLocked()
	rec, ok := tokens[hash]
	nodes := loadOrchNodesLocked()
	orchMu.Unlock()
	if !ok {
		return SessionScope{}, false
	}
	// Refresh scope fields from latest node metadata if present.
	if node, exists := nodes[rec.Scope.SessionID]; exists {
		rec.Scope.ParentID = node.ParentID
		if node.RootID != "" {
			rec.Scope.RootID = node.RootID
		}
		rec.Scope.Depth = node.Depth
		if node.WorkingDir != "" {
			rec.Scope.WorkingDir = node.WorkingDir
		}
	}
	return rec.Scope, true
}

type sessionBridgeFile struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
}

func safeSessionFileName(sessionID string) string {
	s := strings.TrimSpace(sessionID)
	if s == "" || len(s) > 128 || strings.ContainsAny(s, "/\\:*?\"<>|\x00") || strings.Contains(s, "..") {
		return ""
	}
	return s + ".json"
}

func sessionBridgeDir() string {
	return filepath.Join(LoomHome(), "mcp-sessions")
}

// writeSessionBridgeFile writes an owner-only (0600) session credential file
// under <loomDir>/mcp-sessions/<session_id>.json so `loom mcp-bridge` inside a
// Codeman pane automatically discovers its scoped MCP URL and token via
// $CODEMAN_SESSION_ID without passing disallowed envOverrides to Codeman.
func writeSessionBridgeFile(sessionID, targetURL, rawToken string) error {
	name := safeSessionFileName(sessionID)
	if name == "" || strings.TrimSpace(rawToken) == "" {
		return nil
	}
	dir := sessionBridgeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(sessionBridgeFile{URL: targetURL, Token: rawToken, SessionID: sessionID})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), raw, 0o600)
}

func removeSessionBridgeFile(sessionID string) {
	name := safeSessionFileName(sessionID)
	if name == "" {
		return
	}
	_ = os.Remove(filepath.Join(sessionBridgeDir(), name))
}

func revokeSessionToken(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	removeSessionBridgeFile(sessionID)
	orchMu.Lock()
	defer orchMu.Unlock()
	tokens := loadOrchTokensLocked()
	changed := false
	for h, rec := range tokens {
		if rec.Scope.SessionID == sessionID {
			delete(tokens, h)
			changed = true
		}
	}
	if changed {
		_ = saveOrchTokensLocked(tokens)
	}
}

func orchRegisterNode(node OrchestrationNode) (OrchestrationNode, error) {
	node.SessionID = strings.TrimSpace(node.SessionID)
	if node.SessionID == "" {
		return OrchestrationNode{}, errors.New("session_id is required")
	}
	now := time.Now().Unix()
	if node.CreatedAt == 0 {
		node.CreatedAt = now
	}
	node.UpdatedAt = now
	if node.Status == "" {
		node.Status = "active"
	}
	if node.RootID == "" {
		node.RootID = node.SessionID
	}
	if node.WorkspaceMode == "" {
		node.WorkspaceMode = "inherit"
	}

	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	// If parent exists, derive root_id and depth reliably from parent.
	if node.ParentID != "" {
		if parent, ok := nodes[node.ParentID]; ok {
			if parent.RootID != "" {
				node.RootID = parent.RootID
			} else {
				node.RootID = parent.SessionID
			}
			node.Depth = parent.Depth + 1
		} else {
			node.RootID = node.ParentID
			if node.Depth == 0 {
				node.Depth = 1
			}
		}
	}
	nodes[node.SessionID] = node
	err := saveOrchNodesLocked(nodes)
	orchMu.Unlock()
	if err != nil {
		return OrchestrationNode{}, err
	}
	orchHub.publish("tree", map[string]any{"action": "upsert", "node": node})
	return node, nil
}

func orchGetNode(sessionID string) (OrchestrationNode, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return OrchestrationNode{}, false
	}
	orchMu.Lock()
	defer orchMu.Unlock()
	nodes := loadOrchNodesLocked()
	n, ok := nodes[sessionID]
	return n, ok
}

func orchAllNodes() map[string]OrchestrationNode {
	orchMu.Lock()
	defer orchMu.Unlock()
	return loadOrchNodesLocked()
}

func orchMarkStopped(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	node, ok := nodes[sessionID]
	if !ok {
		orchMu.Unlock()
		revokeSessionToken(sessionID)
		return
	}
	now := time.Now().Unix()
	node.Status = "stopped"
	node.StoppedAt = now
	node.UpdatedAt = now
	nodes[sessionID] = node
	_ = saveOrchNodesLocked(nodes)
	orchMu.Unlock()
	revokeSessionToken(sessionID)
	orchHub.publish("tree", map[string]any{"action": "stopped", "node": node})
}

func orchClearWorktree(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	node, ok := nodes[sessionID]
	if ok {
		node.WorktreePath = ""
		node.UpdatedAt = time.Now().Unix()
		nodes[sessionID] = node
		_ = saveOrchNodesLocked(nodes)
	}
	orchMu.Unlock()
	if ok {
		orchHub.publish("tree", map[string]any{"action": "updated", "node": node})
	}
}

// orchResolveScope builds a SessionScope for a known or ad-hoc session ID.
func orchResolveScope(sessionID string) SessionScope {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionScope{}
	}
	if n, ok := orchGetNode(sessionID); ok {
		root := n.RootID
		if root == "" {
			root = n.SessionID
		}
		return SessionScope{
			SessionID:  n.SessionID,
			ParentID:   n.ParentID,
			RootID:     root,
			Depth:      n.Depth,
			WorkingDir: n.WorkingDir,
		}
	}
	return SessionScope{SessionID: sessionID, RootID: sessionID, Depth: 0}
}

func orchTreeForRoot(rootID string, includeStopped bool) []OrchestrationNode {
	rootID = strings.TrimSpace(rootID)
	if rootID == "" {
		return nil
	}
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	orchMu.Unlock()
	var out []OrchestrationNode
	for _, n := range nodes {
		if n.RootID != rootID && n.SessionID != rootID {
			continue
		}
		if !includeStopped && n.Status == "stopped" {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func orchDescendants(sessionID string, includeStopped bool) []OrchestrationNode {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	orchMu.Unlock()
	byParent := map[string][]OrchestrationNode{}
	for _, n := range nodes {
		if n.ParentID == "" {
			continue
		}
		byParent[n.ParentID] = append(byParent[n.ParentID], n)
	}
	var out []OrchestrationNode
	queue := []string{sessionID}
	seen := map[string]bool{sessionID: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		kids := byParent[cur]
		sort.Slice(kids, func(i, j int) bool { return kids[i].CreatedAt < kids[j].CreatedAt })
		for _, k := range kids {
			if seen[k.SessionID] {
				continue
			}
			seen[k.SessionID] = true
			if includeStopped || k.Status != "stopped" {
				out = append(out, k)
			}
			queue = append(queue, k.SessionID)
		}
	}
	return out
}

func orchActiveChildrenCount(rootID string) int {
	rootID = strings.TrimSpace(rootID)
	if rootID == "" {
		return 0
	}
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	orchMu.Unlock()
	count := 0
	for _, n := range nodes {
		if n.RootID == rootID && n.ParentID != "" && n.Status != "stopped" {
			count++
		}
	}
	return count
}

// orchCanTarget verifies that caller and target belong to the same orchestration
// tree (fail-closed: a worker can never message or stop an unrelated user session).
func orchCanTarget(caller SessionScope, targetID string) (OrchestrationNode, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return OrchestrationNode{}, errors.New("target session_id is required")
	}
	node, ok := orchGetNode(targetID)
	if !ok {
		return OrchestrationNode{}, errors.New("target session is not managed in your orchestration tree")
	}
	if caller.SessionID == "" {
		// Unscoped global token: allow targeting any Loom-managed subagent node.
		if node.ParentID == "" {
			return OrchestrationNode{}, errors.New("cannot target a top-level user session without a matching orchestration tree")
		}
		return node, nil
	}
	callerRoot := caller.RootID
	if callerRoot == "" {
		callerRoot = caller.SessionID
	}
	targetRoot := node.RootID
	if targetRoot == "" {
		targetRoot = node.SessionID
	}
	if callerRoot != targetRoot {
		return OrchestrationNode{}, errors.New("target session belongs to a different orchestration tree")
	}
	return node, nil
}

// orchSyncWithLive reconciles stored active nodes with Codeman's live session set.
func orchSyncWithLive(liveIDs map[string]bool) {
	if liveIDs == nil {
		return
	}
	now := time.Now().Unix()
	pruneBefore := now - 24*3600
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	changed := false
	var stoppedIDs []string
	for id, n := range nodes {
		if n.Status != "stopped" && !liveIDs[id] {
			n.Status = "stopped"
			n.StoppedAt = now
			n.UpdatedAt = now
			nodes[id] = n
			stoppedIDs = append(stoppedIDs, id)
			changed = true
		} else if n.Status == "stopped" && n.WorktreePath == "" && n.StoppedAt > 0 && n.StoppedAt < pruneBefore {
			delete(nodes, id)
			changed = true
		}
	}
	if changed {
		_ = saveOrchNodesLocked(nodes)
	}
	orchMu.Unlock()
	for _, id := range stoppedIDs {
		revokeSessionToken(id)
	}
	if changed {
		orchHub.publish("tree", map[string]any{"action": "sync"})
	}
}

// orchEnsureBridgeFiles guarantees every live Codeman session has an owner-only
// 0600 bridge file under <LoomHome>/mcp-sessions/<id>.json when AutoInjectMCP is
// enabled, so sessions created before a Loom restart or outside Loom UI still
// connect seamlessly through `loom mcp-bridge`.
func orchEnsureBridgeFiles(live []codemanSessionInfo) {
	if !loadOrchestrationSettings().AutoInjectMCP || len(live) == 0 {
		return
	}
	dir := sessionBridgeDir()
	_ = os.MkdirAll(dir, 0o700)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer root.Close()
	for _, s := range live {
		if strings.TrimSpace(s.ID) == "" {
			continue
		}
		name := safeSessionFileName(s.ID)
		if name == "" {
			continue
		}
		if _, err := root.Stat(name); err == nil {
			continue
		}
		scope := orchResolveScope(s.ID)
		if scope.WorkingDir == "" {
			scope.WorkingDir = s.WorkingDir
		}
		if tok, err := mintSessionToken(scope); err == nil {
			_ = writeSessionBridgeFile(s.ID, gatewayURL(), tok)
		}
	}
}

// orchReparent attaches an existing session under a new parent orchestrator
// (or detaches it to a root session when newParentID is empty), updating root_id
// and depth recursively with cycle protection.
func orchReparent(sessionID, newParentID string, liveMap map[string]codemanSessionInfo) (OrchestrationNode, error) {
	sessionID = strings.TrimSpace(sessionID)
	newParentID = strings.TrimSpace(newParentID)
	if sessionID == "" {
		return OrchestrationNode{}, errors.New("session_id is required")
	}
	if sessionID == newParentID {
		return OrchestrationNode{}, errors.New("cannot attach a session to itself")
	}

	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	// Prevent cycles: newParentID cannot be a descendant of sessionID.
	if newParentID != "" {
		cur := newParentID
		seen := map[string]bool{}
		for cur != "" && !seen[cur] {
			if cur == sessionID {
				orchMu.Unlock()
				return OrchestrationNode{}, errors.New("cannot attach a session under its own descendant")
			}
			seen[cur] = true
			cur = nodes[cur].ParentID
		}
	}

	now := time.Now().Unix()
	if newParentID != "" {
		if _, ok := nodes[newParentID]; !ok {
			p := OrchestrationNode{SessionID: newParentID, RootID: newParentID, Depth: 0, Status: "active", CreatedAt: now, UpdatedAt: now}
			if live, ok := liveMap[newParentID]; ok {
				p.Name = live.Name
				p.CLI = live.Mode
				p.WorkingDir = live.WorkingDir
			}
			nodes[newParentID] = p
		}
	}
	node, exists := nodes[sessionID]
	if !exists {
		node = OrchestrationNode{SessionID: sessionID, Status: "active", WorkspaceMode: "inherit", CreatedAt: now}
		if live, ok := liveMap[sessionID]; ok {
			node.Name = live.Name
			node.CLI = live.Mode
			node.WorkingDir = live.WorkingDir
		}
	}
	if newParentID == "" {
		node.ParentID = ""
		node.RootID = sessionID
		node.Depth = 0
	} else {
		parent := nodes[newParentID]
		root := parent.RootID
		if root == "" {
			root = parent.SessionID
		}
		node.ParentID = newParentID
		node.RootID = root
		node.Depth = parent.Depth + 1
	}
	node.UpdatedAt = now
	nodes[sessionID] = node

	// Recompute root_id and depth for all descendants of sessionID.
	queue := []string{sessionID}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		pNode := nodes[pid]
		for id, child := range nodes {
			if child.ParentID == pid {
				child.RootID = pNode.RootID
				child.Depth = pNode.Depth + 1
				child.UpdatedAt = now
				nodes[id] = child
				queue = append(queue, id)
			}
		}
	}
	err := saveOrchNodesLocked(nodes)
	orchMu.Unlock()
	if err != nil {
		return OrchestrationNode{}, err
	}
	orchHub.publish("tree", map[string]any{"action": "sync"})
	return node, nil
}

// orchPruneStopped removes stopped sub-session records for a given root (or all
// roots when rootID is empty), optionally removing clean worktrees first.
func orchPruneStopped(ctx context.Context, rootID string, cleanupWorktrees bool) int {
	rootID = strings.TrimSpace(rootID)
	orchMu.Lock()
	nodes := loadOrchNodesLocked()
	var candidates []OrchestrationNode
	for _, n := range nodes {
		if n.Status != "stopped" || n.ParentID == "" {
			continue
		}
		if rootID != "" && n.RootID != rootID {
			continue
		}
		candidates = append(candidates, n)
	}
	orchMu.Unlock()

	removed := 0
	for _, n := range candidates {
		if cleanupWorktrees && n.WorktreePath != "" && n.BaseRepo != "" {
			if err := removeLoomWorktree(ctx, n.BaseRepo, n.WorktreePath, false); err == nil {
				orchClearWorktree(n.SessionID)
				n.WorktreePath = ""
			}
		}
		if n.WorktreePath != "" {
			continue // Keep record visible while an uncleaned worktree still exists.
		}
		orchMu.Lock()
		cur := loadOrchNodesLocked()
		if curNode, ok := cur[n.SessionID]; ok && curNode.Status == "stopped" && curNode.WorktreePath == "" {
			delete(cur, n.SessionID)
			_ = saveOrchNodesLocked(cur)
			removed++
		}
		orchMu.Unlock()
	}
	if removed > 0 {
		orchHub.publish("tree", map[string]any{"action": "sync"})
	}
	return removed
}

func normalizeBoardKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "update", "question", "result", "handoff", "human":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "update"
	}
}

func orchPostBoard(msg BoardMessage) (BoardMessage, error) {
	msg.RootID = strings.TrimSpace(msg.RootID)
	msg.Text = strings.TrimSpace(msg.Text)
	if msg.RootID == "" {
		return BoardMessage{}, errors.New("root_id is required")
	}
	if msg.Text == "" {
		return BoardMessage{}, errors.New("message text is required")
	}
	if len(msg.Text) > orchMaxBoardTextLen {
		return BoardMessage{}, errors.New("message text exceeds 16 KiB limit")
	}
	msg.Kind = normalizeBoardKind(msg.Kind)
	msg.ID = "bm_" + randomHexID(8)
	msg.CreatedAt = time.Now().UnixMilli()

	orchMu.Lock()
	board := loadOrchBoardLocked()
	list := append(board[msg.RootID], msg)
	if len(list) > orchMaxBoardMessagesPerRoot {
		list = list[len(list)-orchMaxBoardMessagesPerRoot:]
	}
	board[msg.RootID] = list
	err := saveOrchBoardLocked(board)
	orchMu.Unlock()
	if err != nil {
		return BoardMessage{}, err
	}
	orchHub.publish("board", msg)
	return msg, nil
}

func orchReadBoard(rootID, sinceID string, limit int) []BoardMessage {
	rootID = strings.TrimSpace(rootID)
	if rootID == "" {
		return []BoardMessage{}
	}
	if limit <= 0 || limit > orchMaxBoardMessagesPerRoot {
		limit = 50
	}
	orchMu.Lock()
	board := loadOrchBoardLocked()
	list := append([]BoardMessage(nil), board[rootID]...)
	orchMu.Unlock()
	if sinceID != "" {
		idx := -1
		for i, m := range list {
			if m.ID == sinceID {
				idx = i
				break
			}
		}
		if idx >= 0 {
			list = list[idx+1:]
		}
	}
	if len(list) > limit {
		list = list[len(list)-limit:]
	}
	if list == nil {
		return []BoardMessage{}
	}
	return list
}

func orchBoardCounts() map[string]int {
	orchMu.Lock()
	board := loadOrchBoardLocked()
	orchMu.Unlock()
	counts := make(map[string]int, len(board))
	for root, list := range board {
		if len(list) > 0 {
			counts[root] = len(list)
		}
	}
	return counts
}
