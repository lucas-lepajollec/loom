package runtime

import (
	"sort"
	"strings"
)

// AnswerReplacement preserves the native protocol's scope of replacement.
// Transport, usage accounting and display projection remain with the adapter.
type AnswerReplacement uint8

const (
	ReplaceItem AnswerReplacement = iota
	ReplaceItemAndChildren
	ReplaceAnswer
)

// AnswerAccumulator collects only visible assistant text, in first-item order.
// Its owner serializes calls together with the display projection.
type AnswerAccumulator struct {
	replacement AnswerReplacement
	parts       map[string]string
	order       []string
	text        string
}

func NewAnswerAccumulator(replacement AnswerReplacement) *AnswerAccumulator {
	return &AnswerAccumulator{replacement: replacement, parts: map[string]string{}}
}

// Apply returns a copied snapshot only for an assistant text delta, including
// empty replacements. Reasoning and tool output never enter the answer.
func (a *AnswerAccumulator) Apply(e AgentEvent) *string {
	if e.Type != "content.delta" || e.Stream != "assistant_text" {
		return nil
	}
	id := e.ItemID
	if a.replacement == ReplaceAnswer {
		id = ""
	}
	if _, ok := a.parts[id]; !ok {
		a.order = append(a.order, id)
	}
	if e.Replace {
		if a.replacement == ReplaceItemAndChildren {
			for _, child := range a.order {
				if strings.HasPrefix(child, id+":") {
					a.parts[child] = ""
				}
			}
		}
		a.parts[id] = ""
	}
	a.parts[id] += e.Delta
	var text strings.Builder
	for _, id := range a.order {
		text.WriteString(a.parts[id])
	}
	a.text = text.String()
	snapshot := a.text
	return &snapshot
}

func (a *AnswerAccumulator) Text() string { return a.text }

// CloseItems closes unfinished display items in stable ID order before the
// terminal event. The projected rows supply tool kind/output; native frames
// and terminal status stay untouched. It does not mutate the input rows.
func CloseItems(tools map[string]map[string]any, end AgentEvent) []AgentEvent {
	ids := []string{}
	for id, tool := range tools {
		status, _ := tool["status"].(string)
		if status != "completed" && status != "failed" && status != "interrupted" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	events := []AgentEvent{}
	for _, id := range ids {
		tool := tools[id]
		kind := "tool_call"
		switch tool["kind"] {
		case "execute":
			kind = "command_execution"
		case "edit":
			kind = "file_change"
		case "search":
			kind = "web_search"
		}
		events = append(events, AgentEvent{Type: "item.completed", Runtime: end.Runtime, ThreadID: end.ThreadID, TurnID: end.TurnID, ItemID: id, ItemType: kind, Status: "interrupted", Error: "turn ended before the item completed", Payload: JSON(tool), Raw: end.Raw})
	}
	return events
}
