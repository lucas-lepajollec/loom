package loom

import (
	"context"
	"strings"
)

// DiscussionEvent is the display vocabulary. Replay controls have no type.
// Runtime-private state never enters the portable transcript.
type DiscussionEvent map[string]any

func nativeDiscussionEvents(d map[string]any) []DiscussionEvent {
	base := DiscussionEvent{}
	for _, key := range []string{"seq", "ts", "ts0", "replace", "toks", "portable_text"} {
		if v, ok := d[key]; ok {
			base[key] = v
		}
	}
	out := []DiscussionEvent{}
	add := func(kind string, fields DiscussionEvent) {
		e := DiscussionEvent{"type": kind}
		for k, v := range base {
			e[k] = v
		}
		for k, v := range fields {
			e[k] = v
		}
		out = append(out, e)
	}
	if v, ok := d["user"]; ok {
		add("turn_start", DiscussionEvent{"text": v, "files": d["files"]})
	}
	if v, ok := d["content"]; ok {
		add("text_delta", DiscussionEvent{"text": v})
	}
	if v, ok := d["reasoning_content"]; ok {
		add("reasoning_delta", DiscussionEvent{"text": v})
	}
	if d["drop_reasoning"] == true {
		add("reasoning_delta", DiscussionEvent{"drop": true})
	}
	if tool, ok := d["tool_used"].(map[string]any); ok {
		kind := "tool_start"
		if tool["typing"] == true {
			kind = "tool_delta"
		}
		if tool["done"] == true {
			kind = "tool_end"
		}
		add(kind, DiscussionEvent{"tool": tool})
	}
	if stats, ok := d["stats"]; ok {
		add("usage", DiscussionEvent{"metrics": stats})
	}
	if v, ok := d["error"]; ok {
		add("error", DiscussionEvent{"error": v})
	}
	if d["turn_done"] == true {
		add("turn_done", DiscussionEvent{"provenance": d["runtime_turn"], "elapsed_ms": d["elapsed_ms"], "metrics": DiscussionEvent{"elapsed_ms": d["elapsed_ms"]}})
	}
	control := DiscussionEvent{}
	for _, key := range []string{"reset", "replay", "caught_up", "pad", "ctx_used", "compact_count", "compacting", "compacted", "compact_noop"} {
		if v, ok := d[key]; ok {
			control[key] = v
		}
	}
	if len(control) > 0 {
		out = append(out, control)
	}
	return out
}

func runtimeDiscussionEvents(event StreamEvent, turn RuntimeTurnRecord) []DiscussionEvent {
	if event.ACPEvent != nil {
		return []DiscussionEvent{event.ACPEvent}
	}
	if event.ACPState != nil {
		return nil
	}
	out := []DiscussionEvent{}
	if event.Content != "" {
		out = append(out, DiscussionEvent{"type": "text_delta", "text": event.Content})
	}
	if event.Reasoning != "" && turn.RuntimeID == "codex" {
		out = append(out, DiscussionEvent{"type": "reasoning_delta", "text": event.Reasoning, "summary": true})
	}
	if event.Usage != nil || event.Stats != nil {
		out = append(out, DiscussionEvent{"type": "usage", "usage": event.Usage, "metrics": event.Stats, "provenance": turn})
	}
	if event.HarnessEvent != nil {
		kind := "tool_delta"
		if event.HarnessEvent.State == "ACTIVE" {
			kind = "tool_start"
		}
		if event.HarnessEvent.State == "DONE" {
			kind = "tool_end"
		}
		out = append(out, DiscussionEvent{"type": kind, "native_tool": *event.HarnessEvent, "provenance": turn})
	}
	return out
}

func runtimeReplay(s RuntimeSession) []DiscussionEvent {
	out := []DiscussionEvent{{"reset": true, "replay": true, "session": s, "context": discussionContext(s)}}
	for i, message := range s.Messages {
		text, _ := message.Content.(string)
		if message.Role == "user" {
			out = append(out, DiscussionEvent{"type": "turn_start", "text": text, "portable_text": true})
			continue
		}
		if message.Role != "assistant" {
			continue
		}
		var turn *RuntimeTurnRecord
		for j := range s.Turns {
			if s.Turns[j].MessageIndex == i {
				turn = &s.Turns[j]
				break
			}
		}
		if turn != nil && len(turn.ACPEvents) > 0 {
			out = append(out, turn.ACPEvents...)
		} else {
			if turn != nil && turn.ReasoningSummary != "" {
				out = append(out, DiscussionEvent{"type": "reasoning_delta", "text": turn.ReasoningSummary, "summary": true})
			}
			out = append(out, DiscussionEvent{"type": "text_delta", "text": text})
		}
		running := s.Status == "running" && i == len(s.Messages)-1
		if running {
			out = append(out, DiscussionEvent{"provenance": turn, "running": true})
		} else {
			out = append(out, DiscussionEvent{"type": "turn_done", "provenance": turn, "metrics": discussionMetrics(turn)})
		}
	}
	if s.Error != "" {
		out = append(out, DiscussionEvent{"type": "error", "error": s.Error})
	}
	return append(out, DiscussionEvent{"caught_up": true, "session": s, "context": discussionContext(s)})
}

func discussionMetrics(turn *RuntimeTurnRecord) DiscussionEvent {
	metrics := DiscussionEvent{}
	if turn == nil {
		return metrics
	}
	if turn.Usage != nil {
		metrics["usage"] = turn.Usage
	}
	if turn.Stats != nil {
		metrics["stats"] = turn.Stats
	}
	if turn.DurationSeconds > 0 {
		metrics["duration_seconds"] = turn.DurationSeconds
	}
	return metrics
}

type discussionSubscriber struct{ events chan DiscussionEvent }

// A slow reader reconnects from a snapshot. It never blocks generation.
func (m *runtimeSessions) publishLocked(id string, events ...DiscussionEvent) {
	for sub := range m.subscribers[id] {
		for _, event := range events {
			select {
			case sub.events <- event:
			default:
				close(sub.events)
				delete(m.subscribers[id], sub)
				break
			}
			if _, ok := m.subscribers[id][sub]; !ok {
				break
			}
		}
	}
}

func (m *runtimeSessions) subscribeDiscussion(ctx context.Context, id string, emit func(DiscussionEvent) bool) bool {
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return false
	}
	sub := &discussionSubscriber{events: make(chan DiscussionEvent, 256)}
	if m.subscribers[id] == nil {
		m.subscribers[id] = map[*discussionSubscriber]bool{}
	}
	m.subscribers[id][sub] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.subscribers[id], sub)
		if len(m.subscribers[id]) == 0 {
			delete(m.subscribers, id)
		}
		m.mu.Unlock()
	}()
	if !emit(DiscussionEvent{"pad": strings.Repeat("·", 2048)}) {
		return true
	}
	for _, e := range runtimeReplay(s) {
		if ctx.Err() != nil || !emit(e) {
			return true
		}
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case event, open := <-sub.events:
			if !open || !emit(event) {
				return true
			}
		}
	}
}
