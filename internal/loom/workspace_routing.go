package loom

import (
	"context"
	"errors"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"reflect"
	"time"
)

func (m *runtimeSessions) selectModel(id, choiceID string, consent bool, effort ...string) (RuntimeSession, error) {
	return m.selectModelContext(context.Background(), id, choiceID, consent, effort...)
}

func (m *runtimeSessions) selectModelContext(ctx context.Context, id, choiceID string, consent bool, effort ...string) (RuntimeSession, error) {
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
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return s, errors.New("discussion not found")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(s) {
		m.mu.Unlock()
		return s, errors.New("wait for or stop the response before changing models")
	}

	if choice.Kind == "cloud" && m.keys[choice.ProviderID] == "" {
		m.mu.Unlock()
		return s, errors.New("connect this provider in Models → Providers")
	}
	original := s
	s = cloneRuntimeSession(s)
	m.mu.Unlock()
	if choice.Kind == "cloud" {
		in := sessionPolicyInput(s, "data.send_provider", policy.Confirm)
		in.ProviderID, in.Endpoint, in.Model, in.AgentID = choice.ProviderID, choice.Endpoint, choice.Model, choice.RuntimeID
		in.MachineID = "local"
		// Existing grants match only this discussion and destination.
		in.DiscussionID = s.ID
		if err := m.authorizePolicy(ctx, in, consent); err != nil {
			return s, err
		}
	}
	previousRuntime, previousModel := s.RuntimeID, s.Model
	s.RuntimeID = "llama.cpp"
	if choice.Kind == "cloud" {
		s.RuntimeID = "openai-compatible"
	}
	s.RuntimeID = "llama.cpp"
	if choice.Kind == "cloud" {
		s.RuntimeID = "openai-compatible"
	}
	s.ReasoningEffort = ""
	s.ProviderID = choice.ProviderID
	s.ProviderName = choice.ProviderName
	s.Endpoint = choice.Endpoint
	s.Model = choice.Model
	if err := ctx.Err(); err != nil {
		return s, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, exists := m.getLocked(id)
	if !exists || !reflect.DeepEqual(current, original) || m.preparing[id] || m.runs[id] != nil || m.nativeRunning(current) {
		return s, errors.New("discussion changed while preparing the executor; try again")
	}
	if choice.Kind == "cloud" && m.keys[choice.ProviderID] == "" {
		return s, errors.New("provider disconnected while preparing the executor")
	}
	if previousRuntime != s.RuntimeID || previousModel != s.Model {
		m.cancelPolicyRequests(id)
		if previousRuntime != s.RuntimeID {
			s.FilesystemPolicy = ""
		}
		s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
		s.NativeSessionFile = ""
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
