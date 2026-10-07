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
	// Too long: the oldest whole messages are left out, never truncated.
	p := prepareTestDiscussion(s, "draft")
	if p.Problem != "" || p.Omitted != 1 || p.HistoryCount != 0 || len(p.Messages) != 2 || p.TextBytes > MaxPortableBytes || len(s.Messages[0].Content.(string)) != MaxPortableBytes-100 {
		t.Fatalf("window not applied: %+v", p.Omitted)
	}
	s.Instructions = ""
	s.Messages = make([]Message, MaxPortableMessages)
	for i := range s.Messages {
		s.Messages[i] = Message{Role: "user", Content: "x"}
	}
	if p := prepareTestDiscussion(s, "draft"); p.Problem != "" || len(p.Messages) != MaxPortableMessages || p.Omitted != 1 {
		t.Fatal("message count not bounded by the window")
	}
	// The window starts on a user turn and keeps the newest exchanges.
	s.Messages = []Message{{Role: "user", Content: strings.Repeat("u", MaxPortableBytes/2)}, {Role: "assistant", Content: strings.Repeat("a", MaxPortableBytes/2)}, {Role: "user", Content: "recent"}, {Role: "assistant", Content: "reply"}}
	if p := prepareTestDiscussion(s, "draft"); p.Problem != "" || p.Omitted != 2 || p.Messages[0].Content != "recent" {
		t.Fatalf("window must start on a user turn: %+v", p.Messages)
	}
	// Instructions and draft alone over budget are still refused.
	s.Messages = nil
	s.Instructions = strings.Repeat("i", MaxPortableBytes)
	if p := prepareTestDiscussion(s, "draft"); p.Problem == "" {
		t.Fatal("oversized instructions accepted")
	}
	s.Instructions = ""
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
	if p := prepareTestDiscussion(s, strings.Repeat("é", MaxMessageBytes/2+1)); p.Problem == "" {
		t.Fatal("draft byte limit missing")
	}
}

func TestTitleIgnoresPastedAttachments(t *testing.T) {
	msg := "Résume ce document :\n\n--- Loom text attachments ---\npasted-text-1.txt (3 UTF-16 units)\nabc\n--- End Loom text attachments ---"
	if got := TitleFromText(msg); got != "Résume ce document :" {
		t.Fatalf("%q", got)
	}
	only := "--- Loom text attachments ---\npasted-text-1.txt (3 UTF-16 units)\nabc\n--- End Loom text attachments ---"
	if got := TitleFromText(only); got != "pasted-text-1.txt" {
		t.Fatalf("%q", got)
	}
}
