package loom

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

func (m *runtimeSessions) continueDiscussion(id, projectName string) (RuntimeSession, error) {
	m.mu.Lock()
	old, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return old, errors.New("discussion not found or locked")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(old) {
		m.mu.Unlock()
		return old, errors.New("a response is already in progress")
	}
	m.preparing[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.preparing, id); m.mu.Unlock() }()
	release, err := reserveNativeDiscussion(old)
	if err != nil {
		return old, err
	}
	defer release()
	original := cloneRuntimeSession(old)
	createdProject := ""
	if strings.TrimSpace(projectName) != "" {
		p, err := createProject(projectName)
		if err != nil {
			return old, err
		}
		createdProject, old.ProjectID = p.ID, p.ID
	} else if old.ProjectID != "" {
		if _, ok := getProject(old.ProjectID); !ok {
			return old, errors.New("project not found or locked")
		}
	}
	rollback := func() {
		if createdProject != "" {
			_ = putStoreJSON(bkRuntimeSessions, id, original)
			_ = putBytes(bkProjects, createdProject, nil)
		}
	}
	if createdProject != "" {
		old.UpdatedAt = time.Now().UnixMilli()
		if err := putStoreJSON(bkRuntimeSessions, id, old); err != nil {
			rollback()
			return old, err
		}
	}
	if err := saveDiscussionHandoff(old, ""); err != nil {
		rollback()
		return old, err
	}
	next := cloneRuntimeSession(old)
	now := time.Now().UnixMilli()
	next.FrozenSnapshot = discussion.FrozenSnapshot{}
	next.ContextExtras = ""
	next.ID, next.ContinuedFrom = newSessionID(), old.ID
	next.Title, next.CustomTitle = old.Title+" ›", true
	next.CreatedAt, next.UpdatedAt, next.Status, next.Error = now, now, "idle", ""
	next.Messages, next.PortableMessages, next.Turns, next.Compactions = []Message{}, nil, nil, nil
	next.Usage, next.Context, next.ContextWarning = nil, nil, false
	next.LastRequestID, next.RequestIDs = "", nil
	next.NativeArchive, next.SourceArchive, next.ImportSource = "", "", nil
	next.NativeSessionID, next.NativeRuntimeID, next.NativeContext = "", "", ""
	next.ACPUsage, next.Commands, next.Files, next.FileBaselines = nil, nil, nil, nil
	next.AvailableModes, next.AvailableConfigOptions, next.AgentCapabilities = nil, nil, nil
	if err := putStoreJSON(bkRuntimeSessions, next.ID, next); err != nil {
		rollback()
		return next, err
	}
	if createdProject != "" {
		if old.NativeArchive != "" {
			if err := setArchiveProject(old.NativeArchive, old.ProjectID); err != nil {
				_ = putBytes(bkRuntimeSessions, next.ID, nil)
				rollback()
				return next, err
			}
		}
		m.closeACP(id)
		old.NativeSessionID, old.NativeRuntimeID, old.NativeContext = "", "", ""
		if err := putStoreJSON(bkRuntimeSessions, id, old); err != nil {
			return next, err
		}
		m.mu.Lock()
		m.publishLocked(id, DiscussionEvent{"session": old, "context": discussionContext(old)})
		m.mu.Unlock()
		conv.mu.Lock()
		active := old.RuntimeID == "llama.cpp" && old.NativeArchive != "" && conv.ID == old.NativeArchive
		conv.mu.Unlock()
		if active {
			conv.persist()
		}
	}
	return next, nil
}
func handleRuntimeSessionContinue(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID          string `json:"id"`
		ProjectName string `json:"project_name"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.continueDiscussion(req.ID, req.ProjectName)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s)})
}
