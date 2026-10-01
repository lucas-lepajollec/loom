package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Harness history: list the sessions a harness already has (session/list),
// and import one into Loom by replaying it (session/load). The Loom discussion
// stays bound to the native session, so continuing from Loom resumes it with
// the harness's full memory.

type acpSessionInfo struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Imported  string `json:"imported,omitempty"` // Loom discussion already bound to it
}

func startACPReader(ctx context.Context, agent acpAgent, cwd string, notify func(acpFrame)) (*acpClient, map[string]any, error) {
	if !agent.available() {
		return nil, nil, errors.New("CLI du harness ou lanceur ACP indisponible")
	}
	c, err := startACPClient(agent.Command, agent.Args, cwd, acpLaunchEnv(agent.ID, "")...)
	if err != nil {
		return nil, nil, err
	}
	c.handler = func(*acpFrame) (any, error) { return nil, errors.New("lecture Loom") }
	c.notify = notify
	if err := c.start(); err != nil {
		c.close()
		return nil, nil, err
	}
	var init struct {
		ProtocolVersion   int            `json:"protocolVersion"`
		AgentCapabilities map[string]any `json:"agentCapabilities"`
	}
	if err := c.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}, "clientInfo": map[string]string{"name": "loom", "version": Version}}, &init); err != nil || init.ProtocolVersion != 1 {
		c.close()
		return nil, nil, errors.New("l’agent ne répond pas au protocole ACP")
	}
	return c, init.AgentCapabilities, nil
}

func listACPSessions(ctx context.Context, agent acpAgent) ([]acpSessionInfo, error) {
	home, _ := os.UserHomeDir()
	c, caps, err := startACPReader(ctx, agent, home, func(acpFrame) {})
	if err != nil {
		return nil, err
	}
	defer c.close()
	sc, _ := caps["sessionCapabilities"].(map[string]any)
	if _, ok := sc["list"]; !ok {
		return nil, errors.New("ce harness ne liste pas ses sessions")
	}
	out := []acpSessionInfo{}
	cursor := ""
	for page := 0; page < 5 && len(out) < 200; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var r struct {
			Sessions   []acpSessionInfo `json:"sessions"`
			NextCursor string           `json:"nextCursor"`
		}
		if err := c.call(ctx, "session/list", params, &r); err != nil {
			if len(out) > 0 {
				break
			}
			return nil, errors.New("liste des sessions refusée par le harness")
		}
		out = append(out, r.Sessions...)
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	bound := map[string]string{}
	for _, s := range workspaceSessions.list() {
		if s.RuntimeID == agent.ID && s.NativeSessionID != "" {
			bound[s.NativeSessionID] = s.ID
		}
	}
	for i := range out {
		out[i].Imported = bound[out[i].SessionID]
	}
	return out, nil
}

// replayBuilder turns the session/update stream of a session/load into Loom
// messages and turn records (same event vocabulary as a live turn).
type replayBuilder struct {
	mu       sync.Mutex
	binding  *acpBinding
	messages []Message
	turns    []RuntimeTurnRecord
	user     strings.Builder
	answer   strings.Builder
	events   []DiscussionEvent
	inAgent  bool
	commands []map[string]any
}

func (b *replayBuilder) flush(agent acpAgent, native string) {
	if b.user.Len() == 0 && b.answer.Len() == 0 && len(b.events) == 0 {
		return
	}
	user := strings.TrimSpace(b.user.String())
	if user == "" {
		user = "(suite)"
	}
	b.messages = append(b.messages, Message{Role: "user", Content: user}, Message{Role: "assistant", Content: b.answer.String()})
	b.turns = append(b.turns, RuntimeTurnRecord{MessageIndex: len(b.messages) - 1, RuntimeID: agent.ID, ProviderName: agent.Name, Model: "default", NativeSessionID: native, ACPEvents: b.events})
	b.user.Reset()
	b.answer.Reset()
	b.events, b.inAgent = nil, false
}

func (b *replayBuilder) notify(agent acpAgent, native string) func(acpFrame) {
	return func(f acpFrame) {
		var params struct {
			Update map[string]any `json:"update"`
		}
		if json.Unmarshal(f.Params, &params) != nil {
			return
		}
		u := params.Update
		b.mu.Lock()
		defer b.mu.Unlock()
		text := func() string {
			c, _ := u["content"].(map[string]any)
			t, _ := c["text"].(string)
			return t
		}
		switch u["sessionUpdate"] {
		case "user_message_chunk":
			if b.inAgent {
				b.flush(agent, native)
			}
			b.user.WriteString(text())
		case "agent_message_chunk":
			b.inAgent = true
			t := text()
			b.answer.WriteString(t)
			b.events = append(b.events, DiscussionEvent{"type": "text_delta", "text": t})
		case "agent_thought_chunk":
			b.inAgent = true
			b.events = append(b.events, DiscussionEvent{"type": "reasoning_delta", "text": text()})
		case "tool_call", "tool_call_update":
			b.inAgent = true
			if u["sessionUpdate"] == "tool_call" {
				id, _ := u["toolCallId"].(string)
				delete(b.binding.tools, id)
			}
			t := b.binding.tool(u)
			kind := "tool_delta"
			if t["status"] == "completed" || t["status"] == "failed" {
				kind = "tool_end"
			}
			b.events = append(b.events, DiscussionEvent{"type": kind, "tool": t})
		case "plan", "plan_update":
			b.events = append(b.events, DiscussionEvent{"type": "plan", "entries": u["entries"]})
		case "available_commands_update":
			if list, ok := u["availableCommands"].([]any); ok {
				b.commands = nil
				for _, raw := range list {
					if m, ok := raw.(map[string]any); ok {
						b.commands = append(b.commands, m)
					}
				}
			}
		}
	}
}

func importACPSession(ctx context.Context, agent acpAgent, info acpSessionInfo, projectID string) (RuntimeSession, error) {
	var s RuntimeSession
	check := acpDirectory
	if agent.Remote {
		check = remoteWorkdir
	}
	cwd, err := check(info.Cwd)
	if err != nil {
		return s, errors.New("le dossier de cette session n’existe plus sur cette machine")
	}
	for _, existing := range workspaceSessions.list() {
		if existing.RuntimeID == agent.ID && existing.NativeSessionID == info.SessionID {
			full, _ := workspaceSessions.get(existing.ID)
			return full, nil
		}
	}
	b := &replayBuilder{binding: &acpBinding{tools: map[string]map[string]any{}}}
	processDir := cwd
	if agent.Remote {
		processDir, _ = os.UserHomeDir()
	}
	c, _, err := startACPReader(ctx, agent, processDir, b.notify(agent, info.SessionID))
	if err != nil {
		return s, err
	}
	defer c.close()
	var loaded acpSessionResponse
	if err := c.call(ctx, "session/load", map[string]any{"sessionId": info.SessionID, "cwd": cwd, "mcpServers": []any{}}, &loaded); err != nil {
		return s, errors.New("le harness n’a pas pu rouvrir cette session")
	}
	time.Sleep(300 * time.Millisecond) // trailing updates sent just after the response
	b.mu.Lock()
	b.flush(agent, info.SessionID)
	messages, turns, commands := b.messages, b.turns, b.commands
	b.mu.Unlock()
	if len(messages) == 0 {
		return s, errors.New("session vide : rien à importer")
	}
	now := time.Now().UnixMilli()
	title := strings.TrimSpace(info.Title)
	if title == "" {
		if t, _ := messages[0].Content.(string); t != "" {
			r := []rune(strings.TrimSpace(t))
			title = string(r[:min(len(r), 70)])
		}
	}
	if len([]rune(title)) > 100 {
		title = string([]rune(title)[:100])
	}
	s = RuntimeSession{ID: newSessionID(), ProjectID: projectID, RuntimeID: agent.ID, ProviderName: agent.Name, Model: "default", Title: title, CreatedAt: now, UpdatedAt: now, Status: "complete", Messages: messages, Turns: turns}
	s.Workdir = cwd
	s.Permission = "ask"
	s.NativeSessionID, s.NativeRuntimeID = info.SessionID, agent.ID
	s.Commands = commands
	// Same hash as the next send computes, so it resumes the native session.
	prepared := prepareDiscussion(s, "x")
	if n := len(prepared.Messages); n > 0 {
		s.NativeContext = acpContextHash(prepared.Messages[:n-1])
	}
	return s, putStoreJSON(bkRuntimeSessions, s.ID, s)
}

// GET /api/runtimes/{id}/sessions: native sessions; POST …/sessions/import.
func handleACPSessions(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	agent, ok := acpAgentFor(r.PathValue("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness ACP introuvable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	list, err := listACPSessions(ctx, agent)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sessions": list})
}

func handleACPImport(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	agent, ok := acpAgentFor(r.PathValue("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness ACP introuvable"})
		return
	}
	var req struct {
		acpSessionInfo
		ProjectID string `json:"project_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.SessionID == "" || len(req.SessionID) > 200 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "session requise"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	s, err := importACPSession(ctx, agent, req.acpSessionInfo, req.ProjectID)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": s})
}
