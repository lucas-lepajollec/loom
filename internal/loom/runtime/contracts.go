package runtime

import (
	"context"
	"github.com/lucas-lepajollec/loom/internal/loom/capability"
)

// RuntimeAdapter is the execution seam, not an inference engine. Adapters own
// protocol translation; their upstream owns inference, tools and cancellation.
// Local and cloud execution use Loom's tool pipeline.
type RuntimeAdapter[Message, Caps, Event any] interface {
	Descriptor() RuntimeDescriptor
	Run(context.Context, RuntimeTurn[Message, Caps], EventSink[Event]) ([]Message, error)
}

type RuntimeDescriptor struct {
	Degraded     []capability.Degraded `json:"degraded,omitempty"`
	Logo         string                `json:"logo,omitempty"`
	Available    *bool                 `json:"available,omitempty"`
	Docs         string                `json:"docs,omitempty"`
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Kind         string                `json:"kind"`
	Description  string                `json:"description"`
	Consent      string                `json:"consent"`
	Implemented  bool                  `json:"implemented"`
	Capabilities []string              `json:"capabilities"`
}

type RuntimeTurn[Message, Caps any] struct {
	Messages    []Message
	Temperature float64
	Caps        Caps
	MaxTokens   int // Optional explicit output budget, used by benchmarks.
}
