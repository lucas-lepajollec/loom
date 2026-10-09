package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *harnessLifecycleService) nodeAction(ctx context.Context, target, id, action string, m *RemoteMachine) (harnessLifecycleState, error) {
	body, _ := json.Marshal(map[string]string{"id": id, "action": action})
	var reply struct {
		OK    bool                  `json:"ok"`
		State harnessLifecycleState `json:"state"`
		Error string                `json:"error"`
	}
	err := nodeMachineJSON(ctx, *m, http.MethodPost, "/api/node/harness/lifecycle", strings.NewReader(string(body)), &reply)
	state := reply.State
	setting := s.setting(target, id)
	state.Target, state.ID, state.Auto, state.LastAuto = target, id, setting.Auto, setting.LastAuto
	if reply.Error != "" {
		status := http.StatusBadRequest
		if e, ok := err.(runtimeActionError); ok {
			status = e.status
		}
		err = runtimeActionError{status, reply.Error}
	} else if err == nil && !reply.OK {
		err = runtimeActionError{http.StatusBadRequest, "node lifecycle response is unavailable"}
	}
	return state, err
}

func (s *nodeHarnessServer) lifecycleAction(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !nodeHarnessEnabled() {
		sendJSON(w, 409, map[string]any{"ok": false, "error": nodeHarnessDisabled})
		return
	}
	var req struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		cancel()
		sendJSON(w, 409, map[string]any{"ok": false, "error": "node stopping"})
		return
	}
	s.actions[ctx] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.actions, ctx)
		s.mu.Unlock()
	}()
	state, err := s.lifecycle.action(ctx, "local", req.ID, req.Action)
	sendHarnessLifecycle(w, state, err)
}
