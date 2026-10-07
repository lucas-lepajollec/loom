package loom

import (
	"encoding/json"
	"net/http"

	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

// Both routes use the existing SSE envelope; subscriptions never send a turn.
func handleDiscussionEvents(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID   string `json:"id"`
		From int    `json:"from"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	native := req.ID == ""
	if !native {
		s, ok := workspaceSessions.get(req.ID)
		if !ok {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion not found or locked"})
			return
		}
		native = s.RuntimeID == "llama.cpp" && s.NativeArchive != ""
		if native {
			conv.mu.Lock()
			active := conv.ID == s.NativeArchive
			conv.mu.Unlock()
			if !active {
				sendJSON(w, 409, map[string]any{"ok": false, "error": "open this local discussion before following its journal"})
				return
			}
		}
	}
	if err := runtimeVaultError(); err != nil {
		sendJSON(w, 423, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	web.SSEHeaders(w, "no-store, no-transform")
	flusher, _ := w.(http.Flusher)
	mu, stop := sseHeartbeat(w, flusher)
	defer stop()
	emit := func(event DiscussionEvent) bool {
		if runtimeVaultError() != nil {
			return false
		}
		b, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": event}}})
		if err != nil {
			return false
		}
		return web.WriteSSE(w, flusher, mu, b) == nil
	}
	if native {
		var stats any
		conv.Subscribe(r.Context(), req.From, func(delta map[string]any) bool {
			for _, event := range nativeDiscussionEvents(delta) {
				if event["reset"] != nil || event["type"] == "turn_start" {
					stats = nil
				}
				if event["type"] == "usage" {
					stats = event["metrics"]
				}
				if event["type"] == "turn_done" && stats != nil {
					event["metrics"].(DiscussionEvent)["stats"] = stats
				}
				if !emit(event) {
					return false
				}
			}
			if delta["caught_up"] == true {
				state := DiscussionEvent{"state": conv.state()}
				if req.ID != "" {
					if s, ok := workspaceSessions.get(req.ID); ok {
						state["session"] = clientSession(s)
						state["context"] = discussionContext(s)
					}
				}
				return emit(state)
			}
			return true
		})
		return
	}
	workspaceSessions.subscribeDiscussion(r.Context(), req.ID, emit)
}
