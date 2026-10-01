package loom

import (
	"context"
	"strings"
	"testing"
)

// Historical catalog fixtures are retained for the disposable browser surface.
type codexModel struct {
	Model         string `json:"model"`
	Name          string `json:"displayName"`
	Hidden        bool   `json:"hidden"`
	DefaultEffort string `json:"defaultReasoningEffort"`
	Efforts       []struct {
		Effort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}
type codexConnection struct {
	Models []codexModel `json:"models"`
}

func TestCodexUsesACPRegistry(t *testing.T) {
	adapter, ok := registeredRuntimes.lookup("codex")
	acp, yes := adapter.(*acpAdapter)
	if !ok || !yes {
		t.Fatal("Codex did not migrate to ACP")
	}
	if acp.agent.Command != "npx" || len(acp.agent.Args) != 2 || !strings.HasPrefix(acp.agent.Args[1], "@agentclientprotocol/codex-acp@") {
		t.Fatal("unversioned Codex ACP command")
	}
	if !hasRuntimeCapability(acp.Descriptor(), "quota") {
		t.Fatal("quota reader lost")
	}
	if _, err := acp.Run(context.Background(), RuntimeTurn{}, func(StreamEvent) bool { return true }); err == nil {
		t.Fatal("turn without chosen session/workdir")
	}
}
func TestCodexACPConsentAndPortableContext(t *testing.T) {
	testHome(t)
	fixture := fakeACPAdapter(t)
	fixture.agent.ID = "codex"
	fixture.agent.Name = "Codex"
	isolateRuntimeRegistry(t, fixture, llamaRuntimeAdapter{})
	m := newRuntimeSessions()
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.selectModel(s.ID, "codex:default", false); err == nil {
		t.Fatal("external route without consent")
	}
	selected, err := m.selectModel(s.ID, "codex:default", true)
	if err != nil {
		t.Fatal(err)
	}
	selected.Turns = []RuntimeTurnRecord{{ACPEvents: []DiscussionEvent{{"type": "tool_start", "tool": map[string]any{"output": "private runtime fixture"}}}}}
	selected.Messages = []Message{{Role: "user", Content: "portable"}}
	if strings.Contains(acpCompact(prepareDiscussion(selected, "").Messages, 4096), "private runtime fixture") {
		t.Fatal("private tool event in portable prompt")
	}
}
