package discussion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const MaxDiscussionInstructions = 12000
const MaxPortableBytes = 128 << 10
const MaxPortableMessages = 200
const MaxMessageBytes = 112 << 10

// MaxTypedBytes bounds what the user types; attached files fill the rest of
// MaxMessageBytes, which stays under the portable window.
const MaxTypedBytes = 64 << 10

// ContextItem explains one ordered part of the outgoing Loom context.
type ContextItem struct {
	Frozen bool   `json:"frozen,omitempty"`
	Text   string `json:"-"` // assembled segment, used only for outgoing retrieval
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Source string `json:"source"`
	Class  string `json:"class,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Reason string `json:"reason"`
	Tokens int    `json:"tokens"`
}
type TokenBudget struct {
	Used      int `json:"used"`
	Available int `json:"available"`
}
type MemoryBudget struct {
	TokenBudget
	Classes map[string]TokenBudget `json:"classes"`
}
type ContextBudget struct {
	ByKind map[string]int `json:"by_kind"`
	Memory MemoryBudget   `json:"memory"`
}

// DiscussionContext is a fresh read model, not a second memory store. Revision
// binds the route, portable history and instructions seen by the client.
type DiscussionContext[Capability any] struct {
	Snapshot          FrozenSnapshot `json:"-"`
	Extras            string         `json:"extras,omitempty"`
	Items             []ContextItem  `json:"items"`
	Budget            ContextBudget  `json:"budget"`
	GlobalPreferences string         `json:"global_preferences,omitempty"`
	Minimum           string         `json:"minimum,omitempty"`
	EstimatedTokens   int            `json:"estimated_tokens"`
	ReferenceIDs      []string       `json:"reference_ids,omitempty"`
	MCPServers        *[]string      `json:"mcp_servers,omitempty"`
	ProjectID         string         `json:"project_id"`
	ProjectName       string         `json:"project_name"`
	Instructions      string         `json:"project_instructions"`
	// BrainCitations: where the Brain passages of this context come from.
	BrainCitations []string     `json:"brain_citations,omitempty"`
	Skills         []Capability `json:"skills"`
	Discussion     string       `json:"discussion_instructions"`
	System         string       `json:"system"`
	Revision       string       `json:"revision"`
	Warning        string       `json:"warning,omitempty"`
	Problem        string       `json:"problem,omitempty"`
}

type DiscussionPreview[Capability any] struct {
	Context      DiscussionContext[Capability] `json:"context"`
	Messages     []Message                     `json:"messages"`
	TextBytes    int                           `json:"text_bytes"`
	HistoryCount int                           `json:"history_count"`
	DraftAdded   bool                          `json:"draft_added"`
	MaxBytes     int                           `json:"max_bytes"`
	MaxMessages  int                           `json:"max_messages"`
	Problem      string                        `json:"problem,omitempty"`
	// Omitted counts the oldest history messages left out so the rest fits the
	// portable budget. Stored history is never changed.
	Omitted int `json:"omitted,omitempty"`
}

// PrepareDiscussion is shared by the read-only preview and execution. It never
// truncates, summarizes, calls a model or mutates the stored history.
func PrepareDiscussion[Usage, Stats, Capability any](s RuntimeSession[Usage, Stats], draft string, c DiscussionContext[Capability]) DiscussionPreview[Capability] {
	p := DiscussionPreview[Capability]{Context: c, Messages: []Message{}, MaxBytes: MaxPortableBytes, MaxMessages: MaxPortableMessages, Problem: c.Problem}
	if c.System != "" {
		p.Messages = append(p.Messages, Message{Role: "system", Content: c.System})
	}
	history := s.Messages
	if s.PortableMessages != nil {
		history = s.PortableMessages
	}
	for _, msg := range history {
		content, ok := msg.Content.(string)
		if !ok || (msg.Role != "user" && msg.Role != "assistant") || len(msg.ToolCalls) > 0 || msg.ToolCallID != "" {
			p.Problem = "This thread contains a non-portable format; no automatic sending."
			continue
		}
		if content != "" {
			p.Messages = append(p.Messages, Message{Role: msg.Role, Content: content})
			p.HistoryCount++
		}
	}
	draft = strings.TrimSpace(draft)
	if draft != "" {
		p.Messages = append(p.Messages, Message{Role: "user", Content: draft})
		p.DraftAdded = true
	}
	p.Messages = WithContextExtras(p.Messages, c.Extras)
	for _, msg := range p.Messages {
		p.TextBytes += len(msg.Content.(string))
	}
	if len(draft) > MaxMessageBytes {
		p.Problem = "Message too long (maximum 112 KiB with attached files)."
		return p
	}
	if p.TextBytes > MaxPortableBytes || len(p.Messages) > MaxPortableMessages {
		p.fitWindow()
	}
	return p
}

// fitWindow keeps the instructions, the draft and the most recent history that
// fits the portable budget: a long discussion stays usable instead of being
// refused forever. Whole messages are dropped, oldest first, and the window
// starts on a user turn; nothing is truncated or summarized.
func (p *DiscussionPreview[Capability]) fitWindow() {
	first := 0
	if len(p.Messages) > 0 && p.Messages[0].Role == "system" {
		first = 1
	}
	last := len(p.Messages)
	if p.DraftAdded {
		last--
	}
	drop := 0
	for drop < last-first && (p.TextBytes > MaxPortableBytes || len(p.Messages)-drop > MaxPortableMessages ||
		p.Messages[first+drop].Role != "user") {
		p.TextBytes -= len(p.Messages[first+drop].Content.(string))
		drop++
	}
	if p.TextBytes > MaxPortableBytes || len(p.Messages)-drop > MaxPortableMessages {
		p.Problem = "Context too long: the instructions and this message alone exceed 128 KiB of text."
		return
	}
	if drop > 0 {
		p.Messages = append(p.Messages[:first], p.Messages[first+drop:]...)
		p.HistoryCount -= drop
		p.Omitted = drop
	}
}

// ContextRevision binds the same ordered route, history and assembled context.
func ContextRevision[Usage, Stats, Capability any](s RuntimeSession[Usage, Stats], c DiscussionContext[Capability]) string {
	// No credentials, unchosen folder contents, global/local-only prompt or hidden state.
	tuple := []any{s.ID, s.Title, s.ProjectID, s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.ReasoningEffort, s.Workdir, s.AdditionalDirs, s.Permission, s.Mode, s.ConfigOptions, c.MCPServers, s.Messages, c.System, c.Problem, c.Warning}
	if c.Extras != "" || c.Snapshot.FrozenRevision != "" {
		tuple = append(tuple, c.Extras, c.Snapshot.FrozenRevision)
	}
	if s.PortableMessages != nil || s.ContinuedFrom != "" {
		tuple = append(tuple, s.PortableMessages, s.ContinuedFrom)
	}
	encoded, _ := json.Marshal(tuple)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// WithContextExtras copies the outgoing last user message. Neither the display
// journal nor the model-facing stored history receives retrieval scaffolding.
func WithContextExtras(messages []Message, extras string) []Message {
	if extras == "" {
		return messages
	}
	out := append([]Message(nil), messages...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "user" {
			continue
		}
		prefix := "<loom-context>\n" + extras + "\n</loom-context>\n\n"
		switch content := out[i].Content.(type) {
		case string:
			out[i].Content = prefix + content
		case []map[string]any:
			parts := append([]map[string]any{{"type": "text", "text": prefix}}, content...)
			out[i].Content = parts
		case []any:
			out[i].Content = append([]any{map[string]any{"type": "text", "text": prefix}}, content...)
		}
		break
	}
	return out
}

// WithoutContextExtras removes only the exact scaffolding added for this turn
// when a local engine returns a rewritten model history after compaction.
func WithoutContextExtras(messages []Message, extras string) []Message {
	if extras == "" {
		return messages
	}
	out := append([]Message(nil), messages...)
	prefix := "<loom-context>\n" + extras + "\n</loom-context>\n\n"
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "user" {
			continue
		}
		switch content := out[i].Content.(type) {
		case string:
			if strings.HasPrefix(content, prefix) {
				out[i].Content = strings.TrimPrefix(content, prefix)
				return out
			}
		case []map[string]any:
			if len(content) > 0 && content[0]["type"] == "text" && content[0]["text"] == prefix {
				out[i].Content = append([]map[string]any(nil), content[1:]...)
				return out
			}
		case []any:
			if len(content) > 0 {
				if part, ok := content[0].(map[string]any); ok && part["type"] == "text" && part["text"] == prefix {
					out[i].Content = append([]any(nil), content[1:]...)
					return out
				}
			}
		}
	}
	return out
}

func PortableHash(messages []Message) string {
	encoded, _ := json.Marshal(messages)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// PortableRevision follows a rolling append guard, while the revision used by
// the frozen prompt remains unchanged. Any rewrite of retained history breaks
// the guard, including changes to messages appended since snapshot capture.
func PortableRevision(snapshot FrozenSnapshot, messages []Message) string {
	if snapshot.FrozenRevision != "" && snapshot.FrozenPortableCount >= 0 && snapshot.FrozenPortableCount <= len(messages) && snapshot.FrozenPortableHash == PortableHash(messages[:snapshot.FrozenPortableCount]) {
		return snapshot.FrozenPortableRevision
	}
	if snapshot.FrozenRevision == "" {
		return PortableHash(messages)
	}
	// A rewrite back to the original history is still a new context boundary.
	encoded, _ := json.Marshal([]any{"portable rewrite", snapshot.FrozenPortableRevision, snapshot.FrozenPortableHash, messages})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func TrackFrozenPortable(snapshot FrozenSnapshot, messages []Message) FrozenSnapshot {
	if snapshot.FrozenRevision != "" && PortableRevision(snapshot, messages) == snapshot.FrozenPortableRevision {
		snapshot.FrozenPortableCount = len(messages)
		snapshot.FrozenPortableHash = PortableHash(messages)
	}
	return snapshot
}
