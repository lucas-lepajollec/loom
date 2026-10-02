package discussion

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type testUsage struct {
	Input, Output, Total, Thinking, Cached int64
	reported                               bool
}
type testStats struct{ GenTokens int }

func TestCloneRetainsHistoricalCopyAndEmptyShapes(t *testing.T) {
	empty := CloneRuntimeSession(RuntimeSession[testUsage, testStats]{})
	if empty.Messages == nil || empty.RequestIDs == nil || empty.Turns == nil || empty.AdditionalDirs != nil {
		t.Fatal("historical empty/null shapes changed")
	}
	servers := []string{"selected"}
	s := RuntimeSession[testUsage, testStats]{
		ACPState: ACPState{MCPServers: &servers, ConfigOptions: map[string]any{"choice": "old"}, Files: []ACPChangedFile{{Path: "old"}}},
		Messages: []Message{{Role: "user", Content: "old"}}, RequestIDs: []string{"old"}, Usage: &testUsage{Input: 1, reported: true},
		Turns: []RuntimeTurnRecord[testUsage, testStats]{{Usage: &testUsage{Output: 2, reported: true}, Stats: &testStats{GenTokens: 3}, Events: []HarnessEvent{{Name: "old"}}, ACPEvents: []DiscussionEvent{{"tool": map[string]any{"name": "old"}}}}},
	}
	c := CloneRuntimeSession(s)
	if !c.Usage.reported || !c.Turns[0].Usage.reported {
		t.Fatal("private usage flags lost")
	}
	c.Messages[0].Content = "new"
	c.RequestIDs[0] = "new"
	c.Usage.Input = 9
	(*c.MCPServers)[0] = "new"
	c.ConfigOptions["choice"] = "new"
	c.Files[0].Path = "new"
	c.Turns[0].Usage.Output = 9
	c.Turns[0].Stats.GenTokens = 9
	c.Turns[0].Events[0].Name = "new"
	c.Turns[0].ACPEvents[0]["tool"].(map[string]any)["name"] = "new"
	if s.Messages[0].Content != "old" || s.RequestIDs[0] != "old" || s.Usage.Input != 1 || servers[0] != "selected" || s.ConfigOptions["choice"] != "old" || s.Files[0].Path != "old" || s.Turns[0].Usage.Output != 2 || s.Turns[0].Stats.GenTokens != 3 || s.Turns[0].Events[0].Name != "old" || s.Turns[0].ACPEvents[0]["tool"].(map[string]any)["name"] != "old" {
		t.Fatal("clone shares historically detached metadata")
	}
}

func TestTurnTitleAndPortablePrefix(t *testing.T) {
	text := strings.Repeat("é", 71)
	if got := TitleFromText(text); got != strings.Repeat("é", 70) {
		t.Fatal("title no longer uses 70 runes")
	}
	if TitleFromText(" a\nb ") != " a\nb " {
		t.Fatal("title helper changed whitespace")
	}
	s := RuntimeSession[testUsage, testStats]{RuntimeID: "codex", ProviderID: "p", ProviderName: "P", Endpoint: "e", Model: "m"}
	p := DiscussionPreview[string]{Context: DiscussionContext[string]{System: "é", Revision: "revision"}, TextBytes: 9}
	turn := NewTurnRecord(s, p, 3)
	if turn.MessageIndex != 3 || turn.Model != "m" || turn.ProviderID != "p" || turn.ContextBytes != 2 || turn.InputBytes != 9 || turn.ContextRevision != "revision" || turn.Usage != nil || turn.Stats != nil {
		t.Fatalf("provenance changed: %+v", turn)
	}
	prefix := []Message{{Role: "user", Content: "q"}}
	for _, tc := range []struct {
		messages []Message
		want     bool
	}{
		{append(append([]Message{}, prefix...), Message{Role: "assistant", Content: "a"}), true},
		{nil, false}, {[]Message{{Role: "assistant", Content: "q"}}, false}, {[]Message{{Role: "user", Content: []any{"q"}}}, false},
	} {
		if PortablePrefix(prefix, tc.messages) != tc.want {
			t.Fatal("prefix changed")
		}
	}
}

func TestRevisionMatchesHistoricalOrderedHashAndIgnoresDisplayMetadata(t *testing.T) {
	s := RuntimeSession[testUsage, testStats]{ID: "s", Title: "t", RuntimeID: "codex", Model: "m", Messages: []Message{{Role: "user", Content: "q"}}}
	c := DiscussionContext[string]{System: "context"}
	// SHA-256 of the original ordered JSON tuple, including its null fields.
	const expected = "d409c14c41ede1d1ac432d30f3690065f496a59f33f31d3904d49ad938de8968"
	if got := ContextRevision(s, c); got != expected {
		t.Fatalf("revision %s", got)
	}
	baseline := ContextRevision(s, c)
	s.Turns = []RuntimeTurnRecord[testUsage, testStats]{{ReasoningSummary: "display-only", Usage: &testUsage{Input: 12}}}
	s.ACPUsage = map[string]any{"cost": 2}
	s.NativeContext = "private"
	s.Files = []ACPChangedFile{{Path: "private"}}
	if ContextRevision(s, c) != baseline {
		t.Fatal("display/private state entered revision")
	}
	s.Messages = append(s.Messages, Message{Role: "assistant", Content: "answer"})
	if ContextRevision(s, c) == baseline {
		t.Fatal("history missing from revision")
	}
}

func TestEmptyModelWireShapes(t *testing.T) {
	got, err := json.Marshal(RuntimeSession[testUsage, testStats]{})
	const want = `{"id":"","project_id":"","runtime_id":"","provider_id":"","provider_name":"","endpoint":"","model":"","title":"","created_at":0,"updated_at":0,"status":"","messages":null}`
	if err != nil || string(got) != want {
		t.Fatalf("session JSON %s %v", got, err)
	}
	got, err = json.Marshal(RuntimeTurnRecord[testUsage, testStats]{})
	if err != nil || string(got) != `{"message_index":0,"runtime_id":"","provider_name":"","model":"","context_bytes":0}` {
		t.Fatalf("turn JSON %s %v", got, err)
	}
	empty := UsageSummaries(map[string]*ModelUsageSummary[testUsage]{}, func(string) bool { return false }, func(testUsage, UsagePrice) float64 { return 0 })
	if !reflect.DeepEqual(empty, []ModelUsageSummary[testUsage]{}) {
		t.Fatal("empty summaries became null")
	}
}
