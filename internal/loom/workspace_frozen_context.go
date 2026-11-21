package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

// Static configuration and portable rewrites invalidate a snapshot. Mutable
// memory, queries and native-reported settings never enter this hash.
func frozenContextRevision(s RuntimeSession) string {
	// Upgrade old class-based snapshots once; subsequent file edits stay frozen.
	tuple := []any{"discussion-instructions-v2", s.ID, s.ProjectID, s.Instructions, s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.ReasoningEffort, s.Workdir, s.AdditionalDirs, s.Permission, s.FilesystemPolicy, s.MCPServers, s.ContinuedFrom, len(s.Compactions), discussion.PortableRevision(s.FrozenSnapshot, s.PortableMessages)}
	if p, ok := getProject(s.ProjectID); ok {
		// Project configuration is static; working state lives in Brain memory.
		if p.Continuity != nil {
			continuity := *p.Continuity
			continuity.WorkingState, continuity.StateUpdatedAt = "", time.Time{}
			p.Continuity = &continuity
		}
		tuple = append(tuple, p)
		for _, id := range p.CapabilityIDs {
			skill, ok := getCapability(id)
			tuple = append(tuple, id, ok, skill.Name, skill.Instructions)
		}
	}
	encoded, _ := json.Marshal(tuple)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func discussionContextFor(s RuntimeSession, query string) DiscussionContext {
	c := assembleDiscussionContext(s)
	snapshot := discussion.CloneFrozenSnapshot(s.FrozenSnapshot)
	revision := frozenContextRevision(s)
	if snapshot.FrozenRevision == "" || snapshot.FrozenRevision != revision {
		for i := range c.Items {
			c.Items[i].Frozen = true
		}
		snapshot = discussion.FrozenSnapshot{
			FrozenContext: c.System, FrozenRevision: revision,
			FrozenItems: c.Items, FrozenBudget: &c.Budget,
			FrozenPortableCount:     len(s.PortableMessages),
			FrozenPortableHash:      discussion.PortableHash(s.PortableMessages),
			FrozenPortableRevision:  discussion.PortableRevision(s.FrozenSnapshot, s.PortableMessages),
			FrozenGlobalPreferences: c.GlobalPreferences, FrozenMinimum: c.Minimum,
			FrozenReferenceIDs: c.ReferenceIDs,
		}
	}
	c.GlobalPreferences, c.Minimum, c.ReferenceIDs = snapshot.FrozenGlobalPreferences, snapshot.FrozenMinimum, snapshot.FrozenReferenceIDs
	c.System, c.Items = snapshot.FrozenContext, append([]ContextItem{}, snapshot.FrozenItems...)
	if snapshot.FrozenBudget != nil {
		c.Budget = *discussion.CloneFrozenSnapshot(snapshot).FrozenBudget
	}
	c.BrainCitations = nil
	c.Snapshot = snapshot
	c.EstimatedTokens = 0
	for _, item := range c.Items {
		c.EstimatedTokens += item.Tokens
	}
	c.Revision = discussionContextRevision(s, c)
	return c
}

// Refresh is explicit and never performs retrieval or starts a generation.
func (m *runtimeSessions) refreshContext(id string) (RuntimeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.getLocked(id)
	if !ok {
		return s, errors.New("discussion not found or locked")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(s) {
		return s, errors.New("a response is already in progress")
	}
	s.FrozenSnapshot = discussion.FrozenSnapshot{}
	s.ContextExtras = ""
	s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		return s, err
	}
	m.cancelPolicyRequests(id)
	if s.NativeArchive != "" {
		if a, ok := loadArchive(s.NativeArchive); ok {
			a.FrozenSnapshot = discussion.FrozenSnapshot{}
			a.ContextExtras = ""
			if err := saveArchive(a); err != nil {
				return s, err
			}
		}
		conv.mu.Lock()
		if conv.ID == s.NativeArchive {
			conv.FrozenSnapshot = discussion.FrozenSnapshot{}
			conv.ContextExtras = ""
		}
		conv.mu.Unlock()
	}
	m.publishLocked(id, DiscussionEvent{"type": "context_refreshed", "session": s})
	return s, nil
}

// POST /api/runtime/sessions/refresh-context {id}: invalidate for the next turn.
func handleRuntimeSessionRefreshContext(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.refreshContext(req.ID)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s)})
}

func recordTurnContext(turn *RuntimeTurnRecord, c DiscussionContext) {
	snapshot := discussion.CloneFrozenSnapshot(discussion.FrozenSnapshot{FrozenItems: c.Items, FrozenBudget: &c.Budget})
	turn.ContextItems, turn.ContextBudget = snapshot.FrozenItems, snapshot.FrozenBudget
	turn.FrozenRevision = c.Snapshot.FrozenRevision
}
