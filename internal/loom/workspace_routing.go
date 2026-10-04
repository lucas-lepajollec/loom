package loom

import (
	"context"
	"errors"
	"time"
)

func (m *runtimeSessions) selectModel(id, choiceID string, consent bool, effort ...string) (RuntimeSession, error) {
	choices := modelCatalog(m.providers())
	var choice ModelChoice
	found := false
	for _, c := range choices {
		if c.ID == choiceID {
			choice = c
			found = true
			break
		}
	}
	if !found || !choice.Enabled {
		return RuntimeSession{}, errors.New("model missing from selector; enable it in Models")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.getLocked(id)
	if !ok {
		return s, errors.New("discussion not found")
	}
	if m.runs[id] != nil || m.nativeRunning(s) {
		return s, errors.New("wait for or stop the response before changing models")
	}
	if (choice.Kind == "cloud" || choice.Kind == "harness") && !consent {
		return s, errors.New("confirm sending the thread and context to this destination")
	}
	if choice.Kind == "cloud" && m.keys[choice.ProviderID] == "" {
		return s, errors.New("connect this provider in Models → Providers")
	}
	previousRuntime, previousModel := s.RuntimeID, s.Model
	s.RuntimeID = "llama.cpp"
	if choice.Kind == "cloud" {
		s.RuntimeID = "openai-compatible"
	}
	if choice.Kind == "harness" {
		if !choice.Ready {
			return s, errors.New("harness CLI unavailable")
		}
		s.RuntimeID = choice.RuntimeID
		if agent, ok := acpAgentFor(s.RuntimeID); ok {
			target := workspaceTarget(agent)
			priorTarget := s.WorkspaceTarget
			if priorTarget == "" {
				if previous, found := acpAgentFor(previousRuntime); found {
					priorTarget = workspaceTarget(previous)
				}
			}
			if s.Workdir == "" || (priorTarget != "" && priorTarget != target) {
				s.Workdir, s.WorkspaceID = "", ""
				s.AdditionalDirs = nil
				if dir, extra := projectFolders(s.ProjectID, agent); dir != "" {
					s.Workdir, s.AdditionalDirs = dir, extra
				} else if target != "" {
					workspace, err := defaultWorkspace(agent)
					if err != nil {
						return s, err
					}
					dir, err := prepareWorkspace(context.Background(), workspace, workspace.Managed)
					if err != nil {
						return s, err
					}
					s.Workdir, s.WorkspaceID = dir, workspace.ID
				}
			}
			attachPrimarySecondBrain(&s, agent)
			s.WorkspaceTarget = target
		}
	}
	s.ReasoningEffort = ""
	if choice.RuntimeID == "codex" && len(choice.ReasoningEfforts) > 0 {
		s.ReasoningEffort = choice.DefaultEffort
		if len(effort) > 0 && effort[0] != "" {
			s.ReasoningEffort = effort[0]
		}
		valid := false
		for _, e := range choice.ReasoningEfforts {
			if e == s.ReasoningEffort {
				valid = true
			}
		}
		if !valid {
			return s, errors.New("reasoning level not supported by this Codex model")
		}
	}
	s.ProviderID = choice.ProviderID
	s.ProviderName = choice.ProviderName
	s.Endpoint = choice.Endpoint
	s.Model = choice.Model
	if previousRuntime != s.RuntimeID || previousModel != s.Model {
		m.closeACP(id)
		if previousRuntime != s.RuntimeID {
			s.FilesystemPolicy = ""
		}
		s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
		s.Mode = ""
		s.Commands = nil
		s.ACPUsage = nil
		s.AgentCapabilities = nil
		s.ConfigOptions = nil
		s.AvailableModes = nil
		s.AvailableConfigOptions = nil
	}
	s.UpdatedAt = time.Now().UnixMilli()
	return s, putStoreJSON(bkRuntimeSessions, id, s)
}

// Import is explicit and non-destructive. Only portable user/assistant text is
// copied; tool protocol state, hidden reasoning and attachments stay in the
// original archive. Reopening the same archive resumes the same Loom thread.
func (m *runtimeSessions) importArchive(id string) (RuntimeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newID := "legacy-" + id
	if existing, ok := m.getLocked(newID); ok {
		return existing, nil
	}
	a, ok := loadArchive(id)
	if !ok {
		return RuntimeSession{}, errors.New("archive not found or locked")
	}
	messages := archivePortableText(a)
	s := RuntimeSession{ID: newID, SourceArchive: id, ProjectID: a.ProjectID, Title: a.Title, CreatedAt: a.SavedAt, UpdatedAt: time.Now().UnixMilli(), RuntimeID: "llama.cpp", ProviderName: "llama.cpp", Model: ReadConfig()["MODEL"], Messages: messages, Status: "idle"}
	s.Turns = nativeTurnRecords(a)
	return s, putStoreJSON(bkRuntimeSessions, newID, s)
}
