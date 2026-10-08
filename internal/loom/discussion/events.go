package discussion

// StreamEvent is the display projection supplied by the application. Live
// execution, cancellation, persistence and subscription state remain in Loom.
type StreamEvent[Usage, Stats any] struct {
	Content      string
	Reasoning    string
	Usage        *Usage
	Stats        *Stats
	HarnessEvent *HarnessEvent
	ACPEvent     DiscussionEvent
	ACPState     *ACPState
}

func NativeDiscussionEvents(d map[string]any) []DiscussionEvent {
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
	if d["compacted"] == true {
		add("compacted", DiscussionEvent{})
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

func RuntimeDiscussionEvents[Usage, Stats any](event StreamEvent[Usage, Stats], turn RuntimeTurnRecord[Usage, Stats]) []DiscussionEvent {
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

func RuntimeReplay[Usage, Stats any](s RuntimeSession[Usage, Stats], context func() any) []DiscussionEvent {
	out := []DiscussionEvent{{"reset": true, "replay": true, "session": s, "context": context()}}
	for i, message := range s.Messages {
		text, _ := message.Content.(string)
		if message.Role == "user" {
			out = append(out, DiscussionEvent{"type": "turn_start", "text": text, "portable_text": true})
			continue
		}
		if message.Role != "assistant" {
			continue
		}
		var turn *RuntimeTurnRecord[Usage, Stats]
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
			out = append(out, DiscussionEvent{"type": "turn_done", "provenance": turn, "metrics": DiscussionMetrics(turn)})
		}
	}
	if s.Error != "" {
		out = append(out, DiscussionEvent{"type": "error", "error": s.Error})
	}
	return append(out, DiscussionEvent{"caught_up": true, "session": s, "context": context()})
}

func DiscussionMetrics[Usage, Stats any](turn *RuntimeTurnRecord[Usage, Stats]) DiscussionEvent {
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
