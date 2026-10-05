package openai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ToolAccess is an explicit application-owned allowlist, not provider authority
// to run arbitrary shell/file/MCP commands.
type ToolAccess interface {
	Definitions() any
	Execute(context.Context, string, map[string]any) (string, error)
}
type ToolEvent struct {
	ID, Name, Arguments, Result string
	Done                        bool
}
type toolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}
type toolDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

func (a Adapter) runTools(ctx context.Context, turn Turn, emit func(Event) bool) (string, error) {
	encoded, err := json.Marshal(turn.Messages)
	if err != nil {
		return "", errors.New("invalid conversation")
	}
	var messages []map[string]any
	if string(encoded) != "null" && json.Unmarshal(encoded, &messages) != nil {
		return "", errors.New("invalid conversation")
	}
	var answer strings.Builder
	var accumulated, roundUsage Usage
	for round := 0; round < 5; round++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		turn.Messages = messages
		roundUsage = Usage{}
		pending := []toolCall{}
		part, err := a.runOnce(ctx, turn, func(e Event) bool {
			if e.Usage != nil {
				roundUsage = *e.Usage
				total := accumulated
				total.Input += roundUsage.Input
				total.Output += roundUsage.Output
				total.Total += roundUsage.Total
				total.Thinking += roundUsage.Thinking
				total.Cached += roundUsage.Cached
				total.inputReported = roundUsage.inputReported && (round == 0 || accumulated.inputReported)
				total.outputReported = roundUsage.outputReported && (round == 0 || accumulated.outputReported)
				e.Usage = &total
			}
			return emit(e)
		}, &pending)
		if err != nil {
			return "", err
		}
		answer.WriteString(part)
		if answer.Len() > 256<<10 {
			return "", errors.New("response too long (maximum 256 KiB)")
		}
		if len(pending) == 0 {
			if round > 0 && !roundUsage.inputReported && !roundUsage.outputReported {
				total := accumulated
				total.inputReported = false
				total.outputReported = false
				if !emit(Event{Usage: &total}) {
					return "", context.Canceled
				}
			}
			return answer.String(), nil
		}
		if round == 4 {
			return "", errors.New("web search call limit reached; ask a narrower question")
		}
		accumulated.Input += roundUsage.Input
		accumulated.Output += roundUsage.Output
		accumulated.Total += roundUsage.Total
		accumulated.Thinking += roundUsage.Thinking
		accumulated.Cached += roundUsage.Cached
		accumulated.inputReported = roundUsage.inputReported && (round == 0 || accumulated.inputReported)
		accumulated.outputReported = roundUsage.outputReported && (round == 0 || accumulated.outputReported)
		messages = append(messages, map[string]any{"role": "assistant", "content": part, "tool_calls": pending})
		for _, call := range pending {
			var args map[string]any
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
				return "", errors.New("invalid provider tool arguments")
			}
			event := &ToolEvent{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
			if !emit(Event{Tool: event}) {
				return "", context.Canceled
			}
			result, err := turn.Tools.Execute(ctx, call.Function.Name, args)
			if err != nil {
				return "", err
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if len(result) > 32<<10 {
				return "", errors.New("web result too large")
			}
			completed := *event
			completed.Result = result
			completed.Done = true
			if !emit(Event{Tool: &completed}) {
				return "", context.Canceled
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": result})
		}
	}
	return "", errors.New("web search call limit reached")
}
