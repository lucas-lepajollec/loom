// Package brain provides Loom's local lexical context index. It has no Loom
// state, model calls, network access, or dependency on a particular harness.
package brain

import (
	"context"
	"time"
)

const (
	MaxFileBytes  = 1 << 20
	MaxFiles      = 20000
	MaxSources    = 100
	MaxIndexBytes = 64 << 20
	MaxChunks     = 60000
)

type Source struct {
	Connector  string `json:"connector,omitempty"`
	Remote     string `json:"remote,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Permission string `json:"permission,omitempty"`
	Primary    bool   `json:"primary,omitempty"`
	// Secondary brains are never injected into context automatically: the
	// model is told they exist and searches them only when a request needs
	// them (Loom's brain search). Read-only unless their permission says so.
	Secondary   bool      `json:"secondary,omitempty"`
	Exclude     []string  `json:"exclude,omitempty"`
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Include     []string  `json:"include,omitempty"`
	ReadOnly    bool      `json:"read_only"`
	Files       int       `json:"files"`
	Chunks      int       `json:"chunks"`
	LastChecked time.Time `json:"last_checked"`
	LastIndexed time.Time `json:"last_indexed"`
	Error       string    `json:"error,omitempty"`
}

type Chunk struct {
	ID      string   `json:"chunk_id"`
	Source  string   `json:"source"`
	Path    string   `json:"path"`
	Heading []string `json:"heading"`
	Text    string   `json:"text"`
}

// Ranges are Unicode code point offsets, [start,end), in the snippet.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type Hit struct {
	Source     string   `json:"source"`
	Path       string   `json:"path"`
	Heading    []string `json:"heading"`
	Snippet    string   `json:"snippet"`
	Highlights []Range  `json:"highlights"`
	Score      float64  `json:"score"`
	ChunkID    string   `json:"chunk_id"`
}
type SearchRequest struct {
	ProjectID    string              `json:"project_id,omitempty"`
	PathPrefixes map[string][]string `json:"path_prefixes,omitempty"`
	Query        string              `json:"query"`
	Sources      []string            `json:"sources,omitempty"`
	Personal     bool                `json:"personal,omitempty"`
	Limit        int                 `json:"limit,omitempty"`
}
type ReadRequest struct {
	ProjectID    string              `json:"project_id,omitempty"`
	PathPrefixes map[string][]string `json:"path_prefixes,omitempty"`
	ChunkID      string              `json:"chunk_id,omitempty"`
	Source       string              `json:"source,omitempty"`
	Path         string              `json:"path,omitempty"`
	Heading      []string            `json:"heading,omitempty"`
	Sources      []string            `json:"sources,omitempty"`
	Personal     bool                `json:"personal,omitempty"`
}
type PackRequest struct {
	ProjectID    string              `json:"project_id,omitempty"`
	PathPrefixes map[string][]string `json:"path_prefixes,omitempty"`
	Query        string              `json:"query"`
	BudgetTokens int                 `json:"budget_tokens,omitempty"`
	Sources      []string            `json:"sources,omitempty"`
	Personal     bool                `json:"personal,omitempty"`
}
type Citation struct {
	ChunkID  string `json:"chunk_id"`
	Source   string `json:"source"`
	Citation string `json:"citation"`
}
type Pack struct {
	Text       string     `json:"text"`
	Citations  []Citation `json:"citations"`
	TokensUsed int        `json:"tokens_used"`
	Chunks     []Chunk    `json:"chunks"`
}

type File struct {
	Source string
	Path   string
	Mtime  int64
	Size   int64
	Chunks []Chunk
}
type Snapshot struct {
	Version   int
	Files     []File
	IndexedAt time.Time
	Scopes    map[string]string
}

// Storage keeps source definitions and the rebuildable file cache separately.
// Built-in documents never enter the persisted snapshot.
type Storage interface {
	LoadSources() ([]Source, error)
	SaveSources([]Source) error
	LoadIndex() (Snapshot, error)
	SaveIndex(Snapshot) error
}
type Document struct {
	Path string
	Text string
}

// Provider emits documents one at a time and stops when emit returns false.
type Provider func(context.Context, func(Document) bool) error
type Options struct {
	Storage       Storage
	Conversations Provider
	Memory        Provider
	Distilled     Provider
	// Available is checked on every read and refresh (e.g. vault locking).
	Available func() error
}
