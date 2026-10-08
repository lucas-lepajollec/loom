package acp

import (
	"encoding/json"
	"fmt"
	"strings"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

// Elicitation modes are capability objects, not booleans (ACP SDK 1.7.0).
// terminal_output is display metadata; Loom does not offer terminal/create.
func ClientCapabilities(files, interactive bool) map[string]any {
	caps := map[string]any{"fs": map[string]bool{"readTextFile": files, "writeTextFile": files}, "terminal": false,
		"_meta": map[string]any{"terminal_output": true}}
	if interactive {
		caps["elicitation"] = map[string]any{"form": map[string]any{}, "url": map[string]any{}}
		caps["_meta"].(map[string]any)["jetbrains"] = map[string]any{"air": map[string]any{"version": 1, "capabilities": []string{"sessionFailure"}}}
	}
	return caps
}

// Claude 0.88 emits question_N plus optional question_N_custom fields. Match
// only that complete schema and a tool reference; all other forms stay forms.
func AskUserQuestions(raw json.RawMessage) []agent.InputQuestion {
	var p struct {
		Mode    string `json:"mode"`
		Tool    string `json:"toolCallId"`
		Message string `json:"message"`
		Schema  struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Mode != "form" || p.Tool == "" || p.Schema.Type != "object" || len(p.Schema.Properties) == 0 || len(p.Schema.Properties)%2 != 0 {
		return nil
	}
	n := len(p.Schema.Properties) / 2
	qs := make([]agent.InputQuestion, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("question_%d", i)
		var field struct {
			Type        string       `json:"type"`
			Title       string       `json:"title"`
			Description string       `json:"description"`
			OneOf       []enumOption `json:"oneOf"`
			Items       struct {
				AnyOf []enumOption `json:"anyOf"`
			} `json:"items"`
		}
		var custom struct {
			Type  string `json:"type"`
			Title string `json:"title"`
		}
		if json.Unmarshal(p.Schema.Properties[key], &field) != nil || json.Unmarshal(p.Schema.Properties[key+"_custom"], &custom) != nil || custom.Type != "string" || custom.Title != "Other" {
			return nil
		}
		opts := field.OneOf
		multi := field.Type == "array"
		if multi {
			opts = field.Items.AnyOf
		} else if field.Type != "string" {
			return nil
		}
		if len(opts) == 0 {
			return nil
		}
		q := agent.InputQuestion{ID: key, Header: field.Title, Question: field.Description, FreeText: true, MultiSelect: multi, Optional: true}
		if n == 1 {
			q.Question = p.Message
		}
		for _, o := range opts {
			if o.Const == "" {
				return nil
			}
			q.Options = append(q.Options, agent.RequestOption{ID: o.Const, Label: o.Title, Description: o.Description})
		}
		qs = append(qs, q)
	}
	return qs
}

type enumOption struct {
	Const       string `json:"const"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func QuestionContent(qs []agent.InputQuestion, a agent.RequestAnswer) json.RawMessage {
	content := map[string]any{}
	for _, q := range qs {
		picks, other := []string{}, []string{}
		for _, v := range a.Answers[q.ID] {
			known := false
			for _, o := range q.Options {
				if v == o.ID || v == o.Label {
					picks = append(picks, o.ID)
					known = true
					break
				}
			}
			if !known {
				other = append(other, v)
			}
		}
		if len(picks) > 0 {
			if q.MultiSelect {
				content[q.ID] = picks
			} else {
				content[q.ID] = picks[0]
			}
		}
		if len(other) > 0 {
			content[q.ID+"_custom"] = strings.Join(other, "\n")
		}
	}
	return agent.JSON(content)
}

// AIR sessionFailure titles/details are provider-authored text. Cleared entries
// have no title and are not failures. Never infer an error from assistant prose.
func FailureMeta(raw json.RawMessage) (message, severity string) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "", ""
	}
	j, _ := m["jetbrains"].(map[string]any)
	air, _ := j["air"].(map[string]any)
	f, _ := air["sessionFailure"].(map[string]any)
	title, _ := f["title"].(string)
	severity, _ = f["severity"].(string)
	if title == "" {
		return "", ""
	}
	return title, severity
}
