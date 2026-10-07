package acp

import "strings"

type SessionResponse struct {
	SessionID string `json:"sessionId"`
	Modes     *struct {
		Current   string           `json:"currentModeId"`
		Available []map[string]any `json:"availableModes"`
	} `json:"modes"`
	Config []map[string]any `json:"configOptions"`
	// Older "session model" API (still used by Hermes and others): a model
	// list outside configOptions, changed with session/set_model.
	Models *struct {
		Current   string `json:"currentModelId"`
		Available []struct {
			ID          string `json:"modelId"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"availableModels"`
	} `json:"models"`
	// pi-acp: the startup notice it then sends as an agent message.
	Meta *struct {
		Pi struct {
			StartupInfo string `json:"startupInfo"`
		} `json:"piAcp"`
	} `json:"_meta,omitempty"`
}

// LegacyModelKey marks the model option Loom built from the older API.
const LegacyModelKey = "loomLegacyModel"

// Options returns configOptions plus, when the agent only uses the older
// model API, an equivalent "model" option so the rest of Loom sees one shape.
func (r SessionResponse) Options() []map[string]any {
	out := r.Config
	if r.Models == nil || len(r.Models.Available) == 0 || ModelOption(out) != nil {
		return out
	}
	values := []any{}
	for _, m := range r.Models.Available {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		values = append(values, map[string]any{"value": m.ID, "name": name, "description": m.Description})
	}
	return append(append([]map[string]any{}, out...), map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select",
		"currentValue": r.Models.Current, "options": values, LegacyModelKey: true})
}

func ConfigValueAllowed(option map[string]any, value any) bool {
	if option["type"] == "boolean" {
		_, ok := value.(bool)
		return ok
	}
	v, ok := value.(string)
	if !ok || option["type"] != "select" {
		return false
	}
	var matches func(any) bool
	matches = func(raw any) bool {
		choices, ok := raw.([]any)
		if !ok {
			return false
		}
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			if choice["value"] == v {
				return true
			}
			if matches(choice["options"]) {
				return true
			}
		}
		return false
	}
	return matches(option["options"])
}

// ModelOption returns the agent's model selector, if it announced one.
func ModelOption(config []map[string]any) map[string]any {
	for _, o := range config {
		if o["category"] == "model" {
			return o
		}
	}
	for _, o := range config {
		if id, _ := o["id"].(string); strings.EqualFold(id, "model") {
			return o
		}
	}
	return nil
}

type OptionValue struct{ Value, Name, Description string }

func OptionValues(option map[string]any) []OptionValue {
	out := []OptionValue{}
	var walk func(any)
	walk = func(raw any) {
		list, _ := raw.([]any)
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if inner, ok := m["options"]; ok {
				walk(inner)
				continue
			}
			v, _ := m["value"].(string)
			n, _ := m["name"].(string)
			d, _ := m["description"].(string)
			if v != "" {
				out = append(out, OptionValue{v, n, d})
			}
		}
	}
	if option != nil {
		walk(option["options"])
	}
	return out
}
