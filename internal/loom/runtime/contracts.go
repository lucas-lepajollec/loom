package runtime

import "context"

// RuntimeAdapter is the execution seam, not an inference engine. Adapters own
// protocol translation; their upstream owns inference, tools and cancellation.
// Harness adapters will use native sessions rather than the local tool loop.
type RuntimeAdapter[Message, Caps, Event any] interface {
	Descriptor() RuntimeDescriptor
	Run(context.Context, RuntimeTurn[Message, Caps], EventSink[Event]) ([]Message, error)
}

// Connectable discovers and saves a native catalog with explicit consent. The
// result preserves each adapter's existing models response for legacy clients;
// /api/workspace continues to expose normalized ModelChoice records.
type Connectable interface {
	Connect(context.Context, bool) (any, error)
}

// QuotaReader reads an account snapshot without generation or reset consumption.
// Throttling and caching belong to the HTTP layer, not to an adapter.
type QuotaReader[Snapshot any] interface {
	Quota(context.Context) (Snapshot, error)
}

type RuntimeDescriptor struct {
	Compatibility      *CompatibilityRecord `json:"compatibility,omitempty"`
	FilesystemPolicies []string             `json:"filesystem_policies,omitempty"`
	MachineID          string               `json:"machine_id,omitempty"`
	Connected          *bool                `json:"connected,omitempty"`
	Logo               string               `json:"logo,omitempty"`
	Available          *bool                `json:"available,omitempty"`
	InstallHint        string               `json:"install_hint,omitempty"`
	Docs               string               `json:"docs,omitempty"`
	Custom             bool                 `json:"custom,omitempty"`
	Machine            string               `json:"machine,omitempty"` // remote machine name, for harnesses running elsewhere
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	Kind               string               `json:"kind"`
	Description        string               `json:"description"`
	CLI                string               `json:"cli"`
	Consent            string               `json:"consent"`
	Implemented        bool                 `json:"implemented"`
	Capabilities       []string             `json:"capabilities"`
}

type RuntimeTurn[Message, Caps any] struct {
	Messages    []Message
	Temperature float64
	Caps        Caps
	MaxTokens   int // Optional explicit output budget, used by benchmarks.
}
