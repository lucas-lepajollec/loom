package discussion

import (
	"strings"
	"testing"
)

func prepareTestDiscussion(s RuntimeSession[testUsage, testStats], draft string) DiscussionPreview[string] {
	c := DiscussionContext[string]{Skills: []string{}}
	if s.Instructions != "" {
		c.System = "Discussion instructions:\n" + s.Instructions
	}
	c.Revision = ContextRevision(s, c)
	return PrepareDiscussion(s, draft, c)
}

func TestDiscussionPromptLimitsAndNonportableData(t *testing.T) {
	s := RuntimeSession[testUsage, testStats]{ID: "test", Instructions: strings.Repeat("i", 12000), Messages: []Message{{Role: "user", Content: strings.Repeat("a", MaxPortableBytes-100)}}}
	p := prepareTestDiscussion(s, "draft")
	if p.Problem == "" || p.TextBytes <= MaxPortableBytes || len(p.Messages) != 3 || len(p.Messages[1].Content.(string)) != MaxPortableBytes-100 {
		t.Fatal("size limit missing or silent truncation")
	}
	s.Instructions = ""
	s.Messages = make([]Message, MaxPortableMessages)
	for i := range s.Messages {
		s.Messages[i] = Message{Role: "user", Content: "x"}
	}
	if p := prepareTestDiscussion(s, "draft"); p.Problem == "" {
		t.Fatal("message count not bounded")
	}
	s.Messages = []Message{{Role: "assistant", Content: ""}}
	if p := prepareTestDiscussion(s, "draft"); len(p.Messages) != 1 || p.HistoryCount != 0 || p.Problem != "" {
		t.Fatal("empty interrupted assistant not omitted")
	}
	for _, message := range []Message{{Role: "tool", Content: "private"}, {Role: "user", Content: []any{"image"}}, {Role: "assistant", Content: "text", ToolCalls: []ToolCall{{ID: "private"}}}} {
		s.Messages = []Message{message}
		if p := prepareTestDiscussion(s, "draft"); p.Problem == "" {
			t.Fatal("unsupported native data silently accepted")
		}
	}
	s.Messages = nil
	if p := prepareTestDiscussion(s, strings.Repeat("é", 12001)); p.Problem == "" {
		t.Fatal("draft byte limit missing")
	}
}
