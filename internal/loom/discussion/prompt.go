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

// DiscussionContext is a fresh read model, not a second memory store. Revision
// binds the route, portable history and instructions seen by the client.
type DiscussionContext[Capability any] struct {
	GlobalPreferences string    `json:"global_preferences,omitempty"`
	Minimum           string    `json:"minimum,omitempty"`
	EstimatedTokens   int       `json:"estimated_tokens"`
	ReferenceIDs      []string  `json:"reference_ids,omitempty"`
	MCPServers        *[]string `json:"mcp_servers,omitempty"`
	ProjectID         string    `json:"project_id"`
	ProjectName       string    `json:"project_name"`
	Instructions      string    `json:"project_instructions"`
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
}

// PrepareDiscussion is shared by the read-only preview and execution. It never
// truncates, summarizes, calls a model or mutates the stored history.
func PrepareDiscussion[Usage, Stats, Capability any](s RuntimeSession[Usage, Stats], draft string, c DiscussionContext[Capability]) DiscussionPreview[Capability] {
	p := DiscussionPreview[Capability]{Context: c, Messages: []Message{}, MaxBytes: MaxPortableBytes, MaxMessages: MaxPortableMessages, Problem: c.Problem}
	if c.System != "" {
		p.Messages = append(p.Messages, Message{Role: "system", Content: c.System})
	}
	for _, msg := range s.Messages {
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
	for _, msg := range p.Messages {
		p.TextBytes += len(msg.Content.(string))
	}
	if len(draft) > 24000 {
		p.Problem = "Message too long (maximum 24000 bytes)."
	} else if p.TextBytes > MaxPortableBytes || len(p.Messages) > MaxPortableMessages {
		p.Problem = "Context too long: maximum 128 KiB of text and 200 messages, including instructions. No text was truncated."
	}
	return p
}

// ContextRevision binds the same ordered route, history and assembled context.
func ContextRevision[Usage, Stats, Capability any](s RuntimeSession[Usage, Stats], c DiscussionContext[Capability]) string {
	// No credentials, unchosen folder contents, global/local-only prompt or hidden state.
	encoded, _ := json.Marshal([]any{s.ID, s.Title, s.ProjectID, s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.ReasoningEffort, s.Workdir, s.AdditionalDirs, s.Permission, s.Mode, s.ConfigOptions, c.MCPServers, s.Messages, c.System, c.Problem, c.Warning})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
