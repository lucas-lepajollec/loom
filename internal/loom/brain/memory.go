package brain

import (
	"crypto/rand"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var memoryClasses = []string{"working", "session", "episodic", "semantic", "procedural", "reflex"}
var memoryID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// MemoryItem is durable knowledge, independent of runtime-private memory.
type MemoryItem struct {
	ID         string           `json:"id" yaml:"id"`
	Class      string           `json:"class" yaml:"class"`
	Scope      string           `json:"scope" yaml:"scope"`
	Text       string           `json:"text" yaml:"-"`
	Tags       []string         `json:"tags" yaml:"tags"`
	Importance float64          `json:"importance" yaml:"importance"`
	Confidence float64          `json:"confidence" yaml:"confidence"`
	CreatedAt  int64            `json:"created_at" yaml:"created_at"`
	UpdatedAt  int64            `json:"updated_at" yaml:"updated_at"`
	LastUsedAt int64            `json:"last_used_at" yaml:"last_used_at"`
	Provenance MemoryProvenance `json:"provenance" yaml:"provenance"`
	Supersedes []string         `json:"supersedes" yaml:"supersedes"`
	Status     string           `json:"status" yaml:"status"`
}
type MemoryProvenance struct {
	Kind         string `json:"kind" yaml:"kind" jsonschema:"source kind: user, agent, discussion, import or distilled"`
	DiscussionID string `json:"discussion_id,omitempty" yaml:"discussion_id,omitempty"`
	MessageIndex *int   `json:"message_index,omitempty" yaml:"message_index,omitempty" jsonschema:"nonnegative integer"`
	Agent        string `json:"agent,omitempty" yaml:"agent,omitempty"`
	Note         string `json:"note,omitempty" yaml:"note,omitempty"`
}

// Optional scores distinguish an explicit zero from the creation defaults.
type RememberRequest struct {
	ID         string           `json:"id,omitempty" jsonschema:"optional stable id; generated when omitted"`
	Class      string           `json:"class" jsonschema:"memory class: working, session, episodic, semantic, procedural or reflex"`
	Scope      string           `json:"scope" jsonschema:"global or project:, machine:, agent:, task: followed by a nonempty id"`
	Text       string           `json:"text" jsonschema:"plain text or Markdown; nonempty, at most 8192 UTF-8 bytes"`
	Tags       []string         `json:"tags,omitempty"`
	Importance *float64         `json:"importance,omitempty" jsonschema:"score from 0 to 1; default 0.5"`
	Confidence *float64         `json:"confidence,omitempty" jsonschema:"score from 0 to 1; default 0.7"`
	Provenance MemoryProvenance `json:"provenance"`
	Supersedes []string         `json:"supersedes,omitempty"`
	Status     string           `json:"status,omitempty" jsonschema:"status: active (default), candidate, superseded, uncertain or expired"`
}
type MemoryPatch struct {
	Text       *string   `json:"text,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
	Importance *float64  `json:"importance,omitempty" jsonschema:"score from 0 to 1"`
	Confidence *float64  `json:"confidence,omitempty" jsonschema:"score from 0 to 1"`
	Status     *string   `json:"status,omitempty" jsonschema:"status: active, candidate, superseded, uncertain or expired"`
	Scope      *string   `json:"scope,omitempty"`
}
type UpdateMemoryRequest struct {
	ID        string      `json:"id"`
	Patch     MemoryPatch `json:"patch"`
	Supersede bool        `json:"supersede,omitempty" jsonschema:"create a successor when normalized text changes; retain the old item as superseded"`
}
type ForgetMemoryRequest struct {
	ID string `json:"id"`
}
type MemoryFilter struct {
	Classes []string `json:"classes,omitempty" jsonschema:"memory classes to include; omit for all"`
	Scopes  []string `json:"scopes,omitempty" jsonschema:"scope ids to include; project scopes also include global"`
	Status  string   `json:"status,omitempty" jsonschema:"status: active (default), candidate, superseded, uncertain, expired or all"`
	Query   string   `json:"query,omitempty" jsonschema:"case-insensitive substring of item text"`
	Limit   int      `json:"limit,omitempty" jsonschema:"nonnegative integer"`
}
type MemoryResult struct {
	OK   bool       `json:"ok"`
	Item MemoryItem `json:"item"`
}
type MemoryList struct {
	OK        bool         `json:"ok"`
	Items     []MemoryItem `json:"items"`
	Malformed int          `json:"malformed"`
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func validScope(scope string) bool {
	if scope == "global" {
		return true
	}
	prefix, id, ok := strings.Cut(scope, ":")
	return ok && contains([]string{"project", "machine", "agent", "task"}, prefix) && strings.TrimSpace(id) == id && id != "" && !strings.ContainsAny(id, "\x00\r\n\t") && utf8.ValidString(id)
}
func normalizedMemoryText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
func validateMemory(item MemoryItem) error {
	if !memoryID.MatchString(item.ID) {
		return errors.New("invalid memory id")
	}
	if !contains(memoryClasses, item.Class) {
		return errors.New("invalid memory class")
	}
	if !validScope(item.Scope) {
		return errors.New("invalid memory scope")
	}
	if strings.TrimSpace(item.Text) == "" || len(item.Text) > 8<<10 || !utf8.ValidString(item.Text) || strings.ContainsRune(item.Text, 0) {
		return errors.New("memory text must be nonempty UTF-8 and at most 8 KiB")
	}
	for _, score := range []float64{item.Importance, item.Confidence} {
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return errors.New("importance and confidence must be between 0 and 1")
		}
	}
	if !contains([]string{"active", "candidate", "superseded", "uncertain", "expired"}, item.Status) {
		return errors.New("invalid memory status")
	}
	if !contains([]string{"user", "agent", "discussion", "import", "distilled"}, item.Provenance.Kind) || (item.Provenance.MessageIndex != nil && *item.Provenance.MessageIndex < 0) {
		return errors.New("invalid memory provenance")
	}
	if item.CreatedAt < 0 || item.UpdatedAt < 0 || item.LastUsedAt < 0 {
		return errors.New("invalid memory timestamp")
	}
	if len(item.Tags) > 128 || len(item.Supersedes) > 128 {
		return errors.New("too many memory tags or predecessors")
	}
	for _, tag := range item.Tags {
		if len(tag) > 256 || !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) {
			return errors.New("invalid memory tag")
		}
	}
	for _, id := range item.Supersedes {
		if !memoryID.MatchString(id) || id == item.ID {
			return errors.New("invalid supersedes id")
		}
	}
	if len(item.Provenance.Note)+len(item.Provenance.Agent)+len(item.Provenance.DiscussionID) > 8<<10 {
		return errors.New("memory provenance exceeds 8 KiB")
	}
	return nil
}
func newMemoryID() string { return "mem_" + rand.Text() }
func cloneMemory(item MemoryItem) MemoryItem {
	item.Tags = append([]string{}, item.Tags...)
	item.Supersedes = append([]string{}, item.Supersedes...)
	if item.Provenance.MessageIndex != nil {
		index := *item.Provenance.MessageIndex
		item.Provenance.MessageIndex = &index
	}
	return item
}

func sameContinuityIdentity(a, b MemoryItem) bool {
	for _, tag := range []string{"session-summary", "project-state", "discussion-state"} {
		if contains(a.Tags, tag) || contains(b.Tags, tag) {
			return contains(a.Tags, tag) && contains(b.Tags, tag) && (tag == "project-state" || a.Provenance.DiscussionID == b.Provenance.DiscussionID)
		}
	}
	return true
}
