package loom

import (
	"context"
	"errors"
)

type llamaRuntimeAdapter struct{}

func (llamaRuntimeAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "llama.cpp", Name: "llama.cpp", Kind: "local", Description: "Moteur local llama.cpp, piloté par Loom.", Implemented: true, Capabilities: []string{"chat", "stream", "tools", "attachments", "cancel"}}
}

func (llamaRuntimeAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	return runChat(ctx, turn.Messages, turn.Temperature, turn.Caps, emit)
}

var registeredRuntimes = newRuntimeRegistry()

// registerRuntime is the single startup registration point. A duplicate or
// dishonest descriptor is a programming error, not a recoverable user setting.
func registerRuntime(adapter RuntimeAdapter) {
	if err := registeredRuntimes.register(adapter); err != nil {
		panic(err)
	}
}

func init() {
	registerRuntime(llamaRuntimeAdapter{})
	registerRuntime(cloudRuntimeAdapter{})
	registerRuntime(antigravityAdapter{})
	registerACPAgents()
	registerRuntime(plannedRuntimeAdapter{RuntimeDescriptor{ID: "hermes", Name: "Hermes", Kind: "harness", Description: "Agent toujours actif, souvent sur une autre machine.", CLI: "hermes", Capabilities: []string{}}})
}

// Planned entries describe direction and can never execute or connect.
type plannedRuntimeAdapter struct{ descriptor RuntimeDescriptor }

func (a plannedRuntimeAdapter) Descriptor() RuntimeDescriptor { return a.descriptor }
func (plannedRuntimeAdapter) Run(context.Context, RuntimeTurn, ChatCallback) ([]Message, error) {
	return nil, errors.New("adaptateur runtime en préparation")
}

// The registry replaces the former standalone local adapter global. Existing
// native execution still calls the same registered llama.cpp adapter.
func localChatRuntime() RuntimeAdapter {
	adapter, ok := registeredRuntimes.lookup("llama.cpp")
	if !ok {
		panic("runtime local non enregistré")
	}
	return adapter
}

func runtimeCatalog() []RuntimeDescriptor { return registeredRuntimes.catalog() }
