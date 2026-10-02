package local

import (
	"context"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

// Runner is Loom's existing conversation/tool pipeline. The adapter forwards the
// original messages, capabilities and callback without copying or transforming.
type Runner[Message, Caps, Callback any] func(context.Context, []Message, float64, Caps, Callback) ([]Message, error)

type Adapter[Message, Caps, Callback any] struct {
	RunChat Runner[Message, Caps, Callback]
}

func (Adapter[Message, Caps, Callback]) Descriptor() runtime.RuntimeDescriptor {
	return runtime.RuntimeDescriptor{ID: "llama.cpp", Name: "llama.cpp", Kind: "local", Description: "Local llama.cpp engine, managed by Loom.", Implemented: true, Capabilities: []string{"chat", "stream", "tools", "attachments", "cancel"}}
}

func (a Adapter[Message, Caps, Callback]) Run(ctx context.Context, turn runtime.RuntimeTurn[Message, Caps], emit Callback) ([]Message, error) {
	return a.RunChat(ctx, turn.Messages, turn.Temperature, turn.Caps, emit)
}
