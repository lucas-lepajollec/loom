package loom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Harness history: list the sessions a harness already has (session/list),
// and import one into Loom by replaying it (session/load). The Loom discussion
// can retain a native binding for compatibility, or start fresh from the
// portable replay and chosen project. The original session is never rewritten.

var nativeImportMu sync.Mutex

func nativeImportMatches(s RuntimeSession, agent acpAgent, nativeID string) bool {
	if s.ImportSource != nil {
		return s.ImportSource.TargetChoice == "" && s.ImportSource.RuntimeID == agent.ID && s.ImportSource.MachineID == agent.Machine && s.ImportSource.SessionID == nativeID
	}
	return s.RuntimeID == agent.ID && s.NativeSessionID == nativeID
}

type acpSessionInfo struct {
	SessionFile string `json:"sessionFile,omitempty"`
	SessionID   string `json:"sessionId"`
	Cwd         string `json:"cwd"`
	Title       string `json:"title,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	Imported    string `json:"imported,omitempty"` // Loom discussion already bound to it
}

func startACPReader(ctx context.Context, agent acpAgent, cwd string, notify func(acpFrame)) (*acpClient, map[string]any, error) {
	if !agent.available() {
		return nil, nil, errors.New("harness CLI or ACP launcher unavailable")
	}
	c, err := startACPClient(agent.Command, agent.Args, cwd, acpLaunchEnv(agent.ID, "")...)
	if err != nil {
		return nil, nil, err
	}
	c.handler = func(*acpFrame) (any, error) { return nil, errors.New("Loom reading") }
	c.notify = notify
	if err := c.start(); err != nil {
		c.close()
		return nil, nil, err
	}
	var init struct {
		ProtocolVersion   int            `json:"protocolVersion"`
		AgentCapabilities map[string]any `json:"agentCapabilities"`
		AgentInfo         map[string]any `json:"agentInfo"`
	}
	if err := c.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": acpClientCapabilities(false, false), "clientInfo": map[string]string{"name": "loom", "version": Version}}, &init); err != nil || init.ProtocolVersion != 1 {
		c.close()
		return nil, nil, errors.New("the agent does not respond to the ACP protocol")
	}
	recordACPCompatibility(agent, init.AgentInfo, nil)
	return c, init.AgentCapabilities, nil
}

func listACPSessions(ctx context.Context, agent acpAgent) ([]acpSessionInfo, error) {
	if nativeAgentProtocol(agent) != "" {
		rows, err := listNativeAgentSessions(ctx, agent)
		return markNativeImports(agent, rows), err
	}
	home, _ := os.UserHomeDir()
	c, caps, err := startACPReader(ctx, agent, home, func(acpFrame) {})
	if err != nil {
		return nil, err
	}
	defer c.close()
	sc, _ := caps["sessionCapabilities"].(map[string]any)
	if _, ok := sc["list"]; !ok {
		return nil, errors.New("this harness does not list its sessions")
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
			return nil, errors.New("session list rejected by the harness")
		}
		out = append(out, r.Sessions...)
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	bound := map[string]string{}
	for _, s := range workspaceSessions.list() {
		if s.ImportSource != nil && s.ImportSource.TargetChoice == "" && s.ImportSource.RuntimeID == agent.ID && s.ImportSource.MachineID == agent.Machine {
			bound[s.ImportSource.SessionID] = s.ID
		} else if s.RuntimeID == agent.ID && s.NativeSessionID != "" {
			bound[s.NativeSessionID] = s.ID
		}
	}
	for i := range out {
		out[i].Imported = bound[out[i].SessionID]
	}
	return out, nil
}

func importACPSession(ctx context.Context, agent acpAgent, info acpSessionInfo, projectID string, fresh ...bool) (RuntimeSession, error) {
	// Serializing explicit imports prevents duplicate retained records while
	// keeping ordinary registry reads independent of a slow native replay.
	nativeImportMu.Lock()
	defer nativeImportMu.Unlock()
	var s RuntimeSession
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("project not found or locked")
		}
	}
	for _, existing := range workspaceSessions.list() {
		if nativeImportMatches(existing, agent, info.SessionID) {
			full, ok := workspaceSessions.get(existing.ID)
			if !ok {
				return s, errors.New("discussion not found or locked")
			}
			return full, nil
		}
	}
	s, err := readNativeACPSession(ctx, agent, info, projectID, len(fresh) > 0 && fresh[0])
	if err != nil {
		return s, err
	}
	return s, putStoreJSON(bkRuntimeSessions, s.ID, s)
}

// Reading is separate from retaining/deduplicating a Loom import. Transfers
// replay the live native source rather than a previously imported Loom copy.
func readNativeACPSession(ctx context.Context, agent acpAgent, info acpSessionInfo, projectID string, fresh bool) (RuntimeSession, error) {
	var s RuntimeSession
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("project not found or locked")
		}
	}
	check := acpDirectory
	if agent.Remote {
		check = remoteWorkdir
	}
	cwd, err := check(info.Cwd)
	if err != nil {
		return s, errors.New("this session's directory no longer exists on this machine")
	}
	messages := []Message{}
	turns := []RuntimeTurnRecord{}
	commands := []map[string]any{}
	sessionFile := ""
	if nativeAgentProtocol(agent) != "" {
		messages, turns, sessionFile, err = readNativeAgentHistory(ctx, agent, info, cwd)
		if err != nil {
			return s, err
		}
	} else {
		b := newReplayBuilder()
		processDir := cwd
		if agent.Remote {
			processDir, _ = os.UserHomeDir()
		}
		c, _, err := startACPReader(ctx, agent, processDir, b.Notify(agent.ID, agent.Name, info.SessionID))
		if err != nil {
			return s, err
		}
		defer c.close()
		var loaded acpSessionResponse
		if err := c.call(ctx, "session/load", map[string]any{"sessionId": info.SessionID, "cwd": cwd, "mcpServers": []any{}}, &loaded); err != nil {
			return s, nativeLoadError(err)
		}
		time.Sleep(300 * time.Millisecond) // trailing updates sent just after the response
		messages, turns, commands = b.Finish(agent.ID, agent.Name, info.SessionID)

	}
	if len(messages) == 0 {
		return s, errors.New("empty session: nothing to import")
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
	s.ImportSource = &NativeImport{RuntimeID: agent.ID, MachineID: agent.Machine, SessionID: info.SessionID, ImportedAt: now}
	s.Workdir = cwd
	s.Permission = "ask"
	s.NativeSessionID, s.NativeRuntimeID = info.SessionID, agent.ID
	s.NativeSessionFile = sessionFile
	s.Commands = commands
	// Same hash as the next send computes, so it resumes the native session.
	prepared := prepareDiscussion(s, "x")
	if n := len(prepared.Messages); n > 0 {
		s.NativeContext = acpContextHash(prepared.Messages[:n-1])
	}
	if fresh {
		s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
		s.NativeSessionFile = ""
		s.Commands = nil
		if projectID != "" {
			s.Workdir = ""
		}
	}
	return s, nil
}

// GET /api/runtimes/{id}/sessions: native sessions; POST …/sessions/import.
func handleACPSessions(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	agent, ok := acpAgentFor(r.PathValue("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "ACP harness not found"})
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
		sendJSON(w, 404, map[string]any{"ok": false, "error": "ACP harness not found"})
		return
	}
	var req struct {
		acpSessionInfo
		ProjectID string `json:"project_id"`
		Fresh     bool   `json:"fresh"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.SessionID == "" || len(req.SessionID) > 200 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "session required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	s, err := importACPSession(ctx, agent, req.acpSessionInfo, req.ProjectID, req.Fresh)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s)})
}

// nativeLoadError explains known, harmless reasons a harness refuses to reopen
// a session; other upstream messages stay hidden (they may contain secrets).
func nativeLoadError(err error) error {
	var re *acpRPCError
	if errors.As(err, &re) && strings.Contains(strings.ToLower(re.Message), "in use by another") {
		return errors.New("this session is open in another client of the harness (its desktop app, CLI or IDE extension): close it there, then import again")
	}
	return errors.New("the harness could not reopen this session")
}
