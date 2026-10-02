package loom

import (
	"encoding/json"
	"testing"
)

func TestResourceCompatibilityWireShapes(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{Capability{ID: "7", Name: "Skill", Description: "d", Instructions: "Body"}, `{"id":"7","name":"Skill","description":"d","instructions":"Body"}`},
		{SkillSource{ID: "loom", Path: "skills", Label: "Loom", Builtin: true, Writable: true}, `{"id":"loom","path":"skills","label":"Loom","builtin":true,"writable":true,"count":0}`},
		{MCPSourceStatus{MCPSource: MCPSource{Path: "source", Label: "External"}, Servers: []MCPSourceServer{}}, `{"path":"source","label":"External","servers":[]}`},
		{MCPFileStatus{Path: "mcp.json"}, `{"path":"mcp.json","mtime":null}`},
		{skillSinkTarget{ID: "claude", Name: "Claude", Dir: "skills"}, `{"id":"claude","name":"Claude","harnesses":null,"dir":"skills","enabled":false,"written":null}`},
		{MCPServerStatus{Name: "remote", Transport: "http", Tools: []string{}, Disabled: []string{}}, `{"name":"remote","transport":"http","enabled":false,"connected":false,"tools":[],"disabled":[]}`},
		{Tool{Type: "function", Function: ToolFunction{Name: "test", Description: "desc"}}, `{"type":"function","function":{"name":"test","description":"desc","parameters":null}}`},
		{MemHit{File: "notes.md", Title: "Notes", Snippet: "Body"}, `{"File":"notes.md","Title":"Notes","Snippet":"Body"}`},
	} {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%T: %s %v; want %s", tc.value, got, err, tc.want)
		}
	}
}
