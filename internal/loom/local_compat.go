package loom

import (
	"context"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/local"
)

// Keep the historical zero-value adapter and registration; inject the original
// chat pipeline on each call rather than creating another local executor.
type llamaRuntimeAdapter struct{}

func (llamaRuntimeAdapter) Descriptor() RuntimeDescriptor {
	return (local.Adapter[Message, Caps, ChatCallback]{}).Descriptor()
}
func (llamaRuntimeAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	return (local.Adapter[Message, Caps, ChatCallback]{RunChat: runChat}).Run(ctx, turn, emit)
}
