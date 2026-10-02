package tools

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

func MemSearchTool() Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "mem_search",
			Description: "Search your memory for pages matching a query.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Keywords"},
					"limit": map[string]any{"type": "integer", "description": "Default 8, max 30"},
				},
				"required": []string{"query"},
			},
		},
	}
}

func MemReadTool() Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "mem_read",
			Description: "Read the content of a memory page.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":   map[string]any{"type": "string", "description": "Page name"},
					"offset": map[string]any{"type": "integer", "description": "Start line (default 1)"},
					"limit":  map[string]any{"type": "integer", "description": "Lines (max 500)"},
				},
				"required": []string{"file"},
			},
		},
	}
}

func MemAddTool() Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "mem_add",
			Description: "Create a new memory page.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":    map[string]any{"type": "string", "description": "Kebab-case page name"},
					"content": map[string]any{"type": "string", "description": "Markdown, first line = title #"},
				},
				"required": []string{"file", "content"},
			},
		},
	}
}

func MemEditTool() Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "mem_edit",
			Description: "Replace an exact, unique snippet of text in a memory page.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{"type": "string", "description": "Page name"},
					"old":  map[string]any{"type": "string", "description": "Exact text to replace (unique)"},
					"new":  map[string]any{"type": "string", "description": "Replacement"},
				},
				"required": []string{"file", "old", "new"},
			},
		},
	}
}

// TailRunes returns the last n runes of s (used to cap tool output).
func TailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
