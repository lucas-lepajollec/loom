package loom

import (
	"encoding/json"
	"testing"
)

func TestRuntimeContractJSONShapes(t *testing.T) {
	available := false
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"descriptor-required", RuntimeDescriptor{ID: "planned", Capabilities: []string{}}, `{"id":"planned","name":"","kind":"","description":"","cli":"","consent":"","implemented":false,"capabilities":[]}`},
		{"descriptor-null", RuntimeDescriptor{}, `{"id":"","name":"","kind":"","description":"","cli":"","consent":"","implemented":false,"capabilities":null}`},
		{"descriptor-optional", RuntimeDescriptor{Logo: "icon", Available: &available, InstallHint: "install", Docs: "docs", Custom: true, Machine: "remote", ID: "fixture", Name: "Fixture", Kind: "harness", Description: "description", CLI: "cli", Consent: "consent", Implemented: true, Capabilities: []string{"chat"}}, `{"logo":"icon","available":false,"install_hint":"install","docs":"docs","custom":true,"machine":"remote","id":"fixture","name":"Fixture","kind":"harness","description":"description","cli":"cli","consent":"consent","implemented":true,"capabilities":["chat"]}`},
		{"turn", RuntimeTurn{Messages: []Message{{Role: "user", Content: "hello"}}, Temperature: 0.7, Caps: Caps{Internet: true, Mem: "off"}, MaxTokens: 64}, `{"Messages":[{"role":"user","content":"hello"}],"Temperature":0.7,"Caps":{"Agent":false,"Internet":true,"Mem":"off","MCP":false},"MaxTokens":64}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.value)
			if err != nil || string(got) != tc.want {
				t.Fatalf("JSON = %s, error = %v; want %s", got, err, tc.want)
			}
		})
	}
}
