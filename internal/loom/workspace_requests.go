package loom

import (
	"errors"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"net/http"
)

func (m *runtimeSessions) cancelPolicyRequests(id string) {
	m.interactionMu.Lock()
	b := m.requests[id]
	delete(m.requests, id)
	m.interactionMu.Unlock()
	// Callers may hold the discussion mutex; resolution emits a journal event.
	if b != nil {
		go b.Cancel()
	}
}
func (m *runtimeSessions) shutdownRuntimes() {
	c := &m.policyConsents
	c.mu.Lock()
	c.closed = true
	pending := make([]*policyConsentRequest, 0, len(c.pending))
	for _, request := range c.pending {
		pending = append(pending, request)
	}
	c.mu.Unlock()
	for _, request := range pending {
		request.cancel()
	}
	m.mu.Lock()
	for _, run := range m.runs {
		if run.cancel != nil {
			run.cancel()
		}
	}
	m.mu.Unlock()
	m.interactionMu.Lock()
	brokers := m.requests
	m.requests = map[string]*agent.RequestBroker{}
	m.interactionMu.Unlock()
	for _, b := range brokers {
		b.Cancel()
	}
	// No legacy worker may still write its policy journal after shutdown returns.
	m.policyWorkers.Wait()
}
func (m *runtimeSessions) answerRequest(id, requestID string, answer agent.RequestAnswer) error {
	if _, ok := m.get(id); !ok {
		return errors.New("discussion not found or locked")
	}
	m.interactionMu.Lock()
	b := m.requests[id]
	m.interactionMu.Unlock()
	if b == nil {
		return errors.New("request no longer pending")
	}
	return b.Resolve(requestID, answer)
}
func handleDiscussionRequest(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var answer agent.RequestAnswer
	if !workspaceDecode(w, r, &answer) {
		return
	}
	if err := workspaceSessions.answerRequest(r.PathValue("id"), r.PathValue("request_id"), answer); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
