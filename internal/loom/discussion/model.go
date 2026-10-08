package discussion

import (
	"encoding/json"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type HarnessEvent = runtime.HarnessEvent

// Message is one entry in the chat history sent to llama.cpp.
// `Content` may be nil when an assistant message only contains tool_calls.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// DiscussionEvent is the display vocabulary. Replay controls have no type.
// Runtime-private state never enters the portable transcript.
type DiscussionEvent map[string]any

type ACPChangedFile struct {
	Path string `json:"path"`
	Op   string `json:"op"`
	Add  int    `json:"add"`
	Del  int    `json:"del"`
	At   int64  `json:"at"`
}

// ACPState is runtime-private display state, never part of prepared messages.
// MCP definitions/env values are deliberately absent from this persisted state.
type ACPState struct {
	NativeSessionFile      string                 `json:"native_session_file,omitempty"`
	PendingRequests        []runtime.AgentRequest `json:"pending_requests,omitempty"`
	FilesystemPolicy       string                 `json:"filesystem_policy,omitempty"`
	WorkspaceID            string                 `json:"workspace_id,omitempty"`
	WorkspaceTarget        string                 `json:"workspace_target,omitempty"`
	MCPServers             *[]string              `json:"mcp_servers,omitempty"`
	NativeSessionID        string                 `json:"native_session_id,omitempty"`
	NativeRuntimeID        string                 `json:"native_runtime_id,omitempty"`
	NativeContext          string                 `json:"native_context,omitempty"`
	Workdir                string                 `json:"workdir,omitempty"`
	AdditionalDirs         []string               `json:"additional_dirs,omitempty"`
	Permission             string                 `json:"permission,omitempty"`
	Mode                   string                 `json:"mode,omitempty"`
	ConfigOptions          map[string]any         `json:"config_options,omitempty"`
	AvailableModes         []map[string]any       `json:"available_modes,omitempty"`
	AvailableConfigOptions []map[string]any       `json:"available_config_options,omitempty"`
	FileBaselines          map[string]string      `json:"file_baselines,omitempty"`
	Files                  []ACPChangedFile       `json:"changed_files,omitempty"`
	ACPUsage               map[string]any         `json:"harness_usage,omitempty"`
	Commands               []map[string]any       `json:"commands,omitempty"`
	AgentCapabilities      map[string]any         `json:"agent_capabilities,omitempty"`
}

func CloneACPState(s ACPState) ACPState {
	b, _ := json.Marshal(s)
	var out ACPState
	_ = json.Unmarshal(b, &out)
	return out
}

type NativeImport struct {
	TargetChoice string `json:"target_choice,omitempty"`
	RuntimeID    string `json:"runtime_id"`
	MachineID    string `json:"machine_id,omitempty"`
	SessionID    string `json:"session_id"`
	ImportedAt   int64  `json:"imported_at"`
}

type ContextState struct {
	Used   int    `json:"used"`
	Size   int    `json:"size"`
	Source string `json:"source"`
}
type CompactionRecord struct {
	At         int64  `json:"at"`
	RuntimeID  string `json:"runtime_id"`
	ProviderID string `json:"provider_id,omitempty"`
	Model      string `json:"model"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
}

// RuntimeSession is a Loom-owned conversation. Its portable transcript outlives
// any execution route. A route change never replaces or forks this history.
type RuntimeSession[Usage, Stats any] struct {
	FrozenSnapshot
	ContextExtras    string             `json:"context_extras,omitempty"`
	Context          *ContextState      `json:"context,omitempty"`
	ContextWarning   bool               `json:"context_warning,omitempty"`
	ContinuedFrom    string             `json:"continued_from,omitempty"`
	PortableMessages []Message          `json:"portable_messages,omitempty"`
	Compactions      []CompactionRecord `json:"compactions,omitempty"`

	ACPState
	ImportSource    *NativeImport `json:"import_source,omitempty"`
	ID              string        `json:"id"`
	ProjectID       string        `json:"project_id"`
	RuntimeID       string        `json:"runtime_id"`
	ProviderID      string        `json:"provider_id"`
	ProviderName    string        `json:"provider_name"`
	Endpoint        string        `json:"endpoint"`
	Model           string        `json:"model"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Title           string        `json:"title"`
	CreatedAt       int64         `json:"created_at"`
	UpdatedAt       int64         `json:"updated_at"`
	MessageCount    int           `json:"message_count,omitempty"` // filled in lists, where messages are left out
	Status          string        `json:"status"`
	Messages        []Message     `json:"messages"`
	Usage           *Usage        `json:"usage,omitempty"`
	Error           string        `json:"error,omitempty"`
	Instructions    string        `json:"instructions,omitempty"`
	CustomTitle     bool          `json:"custom_title,omitempty"`
	// Last request identity prevents retries from charging the same turn twice.
	LastRequestID string                            `json:"last_request_id,omitempty"`
	RequestIDs    []string                          `json:"request_ids,omitempty"`
	Turns         []RuntimeTurnRecord[Usage, Stats] `json:"turns,omitempty"`
	SourceArchive string                            `json:"source_archive,omitempty"`
	NativeArchive string                            `json:"native_archive,omitempty"`
}

type RuntimeTurnRecord[Usage, Stats any] struct {
	ContextItems     []ContextItem     `json:"context_items,omitempty"`
	ContextBudget    *ContextBudget    `json:"context_budget,omitempty"`
	FrozenRevision   string            `json:"frozen_revision,omitempty"`
	ToolSummaries    []string          `json:"tool_summaries,omitempty"`
	ACPEvents        []DiscussionEvent `json:"acp_events,omitempty"`
	MessageIndex     int               `json:"message_index"`
	RuntimeID        string            `json:"runtime_id"`
	ProviderName     string            `json:"provider_name"`
	ProviderID       string            `json:"provider_id,omitempty"`
	Endpoint         string            `json:"endpoint,omitempty"`
	Model            string            `json:"model"`
	ContextBytes     int               `json:"context_bytes"`
	ContextRevision  string            `json:"context_revision,omitempty"`
	InputBytes       int               `json:"input_bytes,omitempty"`
	Usage            *Usage            `json:"usage,omitempty"`
	NativeSessionID  string            `json:"native_session_id,omitempty"`
	Events           []HarnessEvent    `json:"events,omitempty"`
	DurationSeconds  float64           `json:"duration_seconds,omitempty"`
	StartedAt        int64             `json:"started_at,omitempty"`
	Stats            *Stats            `json:"stats,omitempty"`
	ReasoningSummary string            `json:"reasoning_summary,omitempty"`
	ReasoningEffort  string            `json:"reasoning_effort,omitempty"`
}

func CloneRuntimeSession[Usage, Stats any](s RuntimeSession[Usage, Stats]) RuntimeSession[Usage, Stats] {
	s.FrozenSnapshot = CloneFrozenSnapshot(s.FrozenSnapshot)
	s.ACPState = CloneACPState(s.ACPState)
	if s.ImportSource != nil {
		source := *s.ImportSource
		s.ImportSource = &source
	}
	s.Messages = append([]Message{}, s.Messages...)
	if s.PortableMessages != nil {
		s.PortableMessages = append([]Message{}, s.PortableMessages...)
	}
	s.Compactions = append([]CompactionRecord(nil), s.Compactions...)
	if s.Context != nil {
		c := *s.Context
		s.Context = &c
	}
	s.RequestIDs = append([]string{}, s.RequestIDs...)
	s.Turns = append([]RuntimeTurnRecord[Usage, Stats]{}, s.Turns...)
	for i := range s.Turns {
		s.Turns[i] = CloneRuntimeTurn(s.Turns[i])
	}
	if s.Usage != nil {
		u := *s.Usage
		s.Usage = &u
	}
	return s
}

func CloneRuntimeTurn[Usage, Stats any](turn RuntimeTurnRecord[Usage, Stats]) RuntimeTurnRecord[Usage, Stats] {
	turn.ToolSummaries = append([]string(nil), turn.ToolSummaries...)
	snapshot := CloneFrozenSnapshot(FrozenSnapshot{FrozenItems: turn.ContextItems, FrozenBudget: turn.ContextBudget})
	turn.ContextItems, turn.ContextBudget = snapshot.FrozenItems, snapshot.FrozenBudget

	if turn.ACPEvents != nil {
		b, _ := json.Marshal(turn.ACPEvents)
		turn.ACPEvents = nil
		_ = json.Unmarshal(b, &turn.ACPEvents)
	}
	if turn.Stats != nil {
		v := *turn.Stats
		turn.Stats = &v
	}
	if turn.Events != nil {
		turn.Events = append([]HarnessEvent{}, turn.Events...)
	}
	if turn.Usage != nil {
		v := *turn.Usage
		turn.Usage = &v
	}
	return turn
}

// FrozenSnapshot travels with the discussion, including native archives. Its
// metadata describes the captured text even when the source memory is updated.
type FrozenSnapshot struct {
	FrozenContext           string         `json:"frozen_context,omitempty"`
	FrozenRevision          string         `json:"frozen_revision,omitempty"`
	FrozenItems             []ContextItem  `json:"frozen_items,omitempty"`
	FrozenBudget            *ContextBudget `json:"frozen_budget,omitempty"`
	FrozenPortableCount     int            `json:"frozen_portable_count,omitempty"`
	FrozenPortableHash      string         `json:"frozen_portable_hash,omitempty"`
	FrozenPortableRevision  string         `json:"frozen_portable_revision,omitempty"`
	FrozenGlobalPreferences string         `json:"frozen_global_preferences,omitempty"`
	FrozenMinimum           string         `json:"frozen_minimum,omitempty"`
	FrozenReferenceIDs      []string       `json:"frozen_reference_ids,omitempty"`
}

func CloneFrozenSnapshot(s FrozenSnapshot) FrozenSnapshot {
	b, _ := json.Marshal(s)
	var out FrozenSnapshot
	_ = json.Unmarshal(b, &out)
	return out
}
