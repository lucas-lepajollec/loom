package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

// Static configuration and portable rewrites invalidate a snapshot. Mutable
// memory, handoffs, queries and native-reported settings never enter this hash.
func frozenContextRevision(s RuntimeSession) string {
	if registered, ok := registeredRuntimes.lookup(s.RuntimeID); ok {
		if acp, ok := registered.(*acpAdapter); ok {
			if s.Workdir == "" {
				dir, extra := projectFolders(s.ProjectID, acp.agent)
				if dir == "" && acp.agent.Remote {
					dir = acp.agent.RemoteHome
				}
				s.Workdir = dir
				if len(s.AdditionalDirs) == 0 {
					s.AdditionalDirs = extra
				}
			}
			attachPrimarySecondBrain(&s, acp.agent)
		}
	}
	tuple := []any{s.ID, s.ProjectID, s.Instructions, s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.ReasoningEffort, s.Workdir, s.AdditionalDirs, s.Permission, s.FilesystemPolicy, s.MCPServers, s.ContinuedFrom, len(s.Compactions), discussion.PortableRevision(s.FrozenSnapshot, s.PortableMessages)}
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
	var preferences sharedPreferences
	getStoreJSON(bkState, sharedPreferencesKey, &preferences)
	tuple = append(tuple, preferences)
	for _, part := range primarySecondBrainParts() {
		tuple = append(tuple, part.text)
	}
	encoded, _ := json.Marshal(tuple)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func discussionContextFor(s RuntimeSession, query string) DiscussionContext {
	// Reading still validates accessible selected sources. Their mutable contents
	// do not affect the revision or replace a valid snapshot.
	c := assembleDiscussionContext(s, "")
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
	if loomTranscript(s) && strings.TrimSpace(query) != "" {
		fresh := assembleDiscussionContext(s, query)
		if fresh.Problem != "" {
			c.Problem = fresh.Problem
		}
		extra := DiscussionContext{Budget: discussion.ContextBudget{ByKind: map[string]int{}}}
		for _, item := range fresh.Items {
			if item.Kind != "memory" && item.Kind != "brain_passage" || strings.Contains(c.System, item.Text) {
				continue
			}
			item.Frozen = false
			appendContextPart(&extra, contextPart{text: item.Text, separator: "\n\n", item: item})
			if item.Kind == "brain_passage" {
				c.BrainCitations = append(c.BrainCitations, item.Label)
			}
		}
		c.Extras = extra.System
		if c.Extras != "" {
			// Delimiters are sent too; charge their framing to the first extra item.
			framing := brain.Tokens("<loom-context>\n"+c.Extras+"\n</loom-context>\n\n") - brain.Tokens(c.Extras)
			extra.Items[0].Tokens += framing
			extra.Budget.ByKind[extra.Items[0].Kind] += framing
			c.Items = append(c.Items, extra.Items...)
			for kind, tokens := range extra.Budget.ByKind {
				c.Budget.ByKind[kind] += tokens
			}
			for _, item := range extra.Items {
				if item.Kind == "memory" {
					c.Budget.Memory.Used += item.Tokens
					class := c.Budget.Memory.Classes[item.Class]
					class.Used += item.Tokens
					c.Budget.Memory.Classes[item.Class] = class
				}
			}
		}
	}
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
	m.closeACP(id)
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
