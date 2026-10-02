package loom

import (
	"context"
	"errors"
	"os"
)

var registeredRuntimes = newRuntimeRegistry()

// registerRuntime is the single startup registration point. A duplicate or
// dishonest descriptor is a programming error, not a recoverable user setting.
func registerRuntime(adapter RuntimeAdapter) {
	if err := registeredRuntimes.register(adapter); err != nil {
		panic(err)
	}
}

func init() {
	// Registration inspects harness descriptors and saved machine names. Node
	// and privileged-update entry points must not open the main application DB
	// or probe harnesses before Main selects their isolated execution context.
	if len(os.Args) > 1 && (os.Args[1] == "node" || os.Args[1] == "system-update") {
		return
	}
	registerRuntime(llamaRuntimeAdapter{})
	registerRuntime(cloudRuntimeAdapter{})
	registerACPAgents()
}

// Planned entries describe direction and can never execute or connect.
type plannedRuntimeAdapter struct{ descriptor RuntimeDescriptor }

func (a plannedRuntimeAdapter) Descriptor() RuntimeDescriptor { return a.descriptor }
func (plannedRuntimeAdapter) Run(context.Context, RuntimeTurn, ChatCallback) ([]Message, error) {
	return nil, errors.New("runtime adapter planned")
}

// The registry replaces the former standalone local adapter global. Existing
// native execution still calls the same registered llama.cpp adapter.
func localChatRuntime() RuntimeAdapter {
	adapter, ok := registeredRuntimes.lookup("llama.cpp")
	if !ok {
		panic("local runtime not registered")
	}
	return adapter
}

func runtimeCatalog() []RuntimeDescriptor { return registeredRuntimes.catalog() }
