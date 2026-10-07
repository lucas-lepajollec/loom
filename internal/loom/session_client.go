package loom

import (
	"net/http"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

// Long discussions carry hundreds of tool calls whose inputs and outputs reach
// 64 KiB each (14 MB for one imported Codex thread). The interface receives a
// light copy: long tool texts keep their end (where results and errors are)
// and are marked truncated; the full call is fetched on demand.
const clientToolText = 4 << 10

// Older turns are usually collapsed: their tool texts are trimmed further.
const clientOldToolText, clientRecentTurns, clientToolTitle = 1 << 10, 3, 240

// clientSession is the discussion sent with REST responses and replay
// envelopes: turn metadata only (the interface replays turn events once,
// through the discussion event stream).
func clientSession(s RuntimeSession) RuntimeSession {
	if len(s.Turns) == 0 {
		return s
	}
	out := s
	out.Turns = make([]RuntimeTurnRecord, len(s.Turns))
	for i, turn := range s.Turns {
		turn.ACPEvents = nil
		out.Turns[i] = turn
	}
	return out
}

// lightTurnEvents trims long tool texts in replayed turn events.
func lightTurnEvents(s RuntimeSession) RuntimeSession {
	out := s
	out.Turns = make([]RuntimeTurnRecord, len(s.Turns))
	for i, turn := range s.Turns {
		limit := clientToolText
		if i < len(s.Turns)-clientRecentTurns {
			limit = clientOldToolText
		}
		events := make([]DiscussionEvent, len(turn.ACPEvents))
		for j, e := range turn.ACPEvents {
			events[j] = e
			tool, ok := e["tool"].(map[string]any)
			if !ok {
				continue
			}
			if light, cut := lightTool(tool, limit); cut {
				copied := DiscussionEvent{}
				for k, v := range e {
					copied[k] = v
				}
				copied["tool"] = light
				events[j] = copied
			}
		}
		turn.ACPEvents = events
		out.Turns[i] = turn
	}
	return out
}

// clientReplay is the replay sent to the interface: each turn's events once
// (long tool texts trimmed), with metadata-only sessions and provenance.
func clientReplay(s RuntimeSession) []DiscussionEvent {
	events := discussion.RuntimeReplay(lightTurnEvents(s), func() any { return discussionContext(s) })
	light := clientSession(s)
	for i, e := range events {
		if _, ok := e["session"]; ok {
			copied := DiscussionEvent{}
			for k, v := range e {
				copied[k] = v
			}
			copied["session"] = light
			e = copied
		}
		if turn, ok := e["provenance"].(*RuntimeTurnRecord); ok && turn != nil {
			meta := *turn
			meta.ACPEvents = nil
			copied := DiscussionEvent{}
			for k, v := range e {
				copied[k] = v
			}
			copied["provenance"] = &meta
			e = copied
		}
		events[i] = e
	}
	return events
}

func lightTool(tool map[string]any, limit int) (map[string]any, bool) {
	cut := false
	light := map[string]any{}
	for k, v := range tool {
		light[k] = v
	}
	for key, n := range map[string]int{"output": limit, "input": limit / 2} {
		if text, ok := tool[key].(string); ok && len(text) > n {
			light[key] = "…\n" + validUTF8Tail(text, n)
			cut = true
		}
	}
	// Some harnesses title a call with its whole script: keep the start.
	if title, ok := tool["title"].(string); ok && len(title) > clientToolTitle {
		head := title[:clientToolTitle]
		for len(head) > 0 && !utf8.ValidString(head) {
			head = head[:len(head)-1]
		}
		light["title"] = head + "…"
		cut = true
	}
	if cut {
		light["truncated"] = true
	}
	return light, cut
}

func validUTF8Tail(s string, n int) string {
	s = s[len(s)-n:]
	for len(s) > 0 && s[0]&0xC0 == 0x80 {
		s = s[1:]
	}
	return s
}

// GET /api/runtime/sessions/tool?id=&tool=: the full, latest state of one tool
// call of a discussion.
func handleRuntimeSessionTool(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	s, ok := workspaceSessions.get(r.URL.Query().Get("id"))
	toolID := r.URL.Query().Get("tool")
	if !ok || toolID == "" {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion or tool not found"})
		return
	}
	var found map[string]any
	for _, turn := range s.Turns {
		for _, e := range turn.ACPEvents {
			if tool, ok := e["tool"].(map[string]any); ok && tool["id"] == toolID {
				found = tool
			}
		}
	}
	if found == nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion or tool not found"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "tool": found})
}

// clientEvent strips turn events from the sessions and provenance a live
// discussion event carries: the interface already has them.
func clientEvent(e DiscussionEvent) DiscussionEvent {
	var copied DiscussionEvent
	set := func(k string, v any) {
		if copied == nil {
			copied = DiscussionEvent{}
			for key, value := range e {
				copied[key] = value
			}
		}
		copied[k] = v
	}
	switch v := e["session"].(type) {
	case RuntimeSession:
		set("session", clientSession(v))
	case *RuntimeSession:
		if v != nil {
			set("session", clientSession(*v))
		}
	}
	switch v := e["provenance"].(type) {
	case RuntimeTurnRecord:
		v.ACPEvents = nil
		set("provenance", v)
	case *RuntimeTurnRecord:
		if v != nil {
			meta := *v
			meta.ACPEvents = nil
			set("provenance", &meta)
		}
	}
	if copied == nil {
		return e
	}
	return copied
}
