package loom

import (
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
	// Worker and privileged-update entry points use an isolated context.
	if len(os.Args) > 1 && (os.Args[1] == "node" || os.Args[1] == "system-update") {
		return
	}
	registerRuntime(llamaRuntimeAdapter{})
	registerRuntime(cloudRuntimeAdapter{})
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

func runtimeCatalog() []RuntimeDescriptor {
	catalog := registeredRuntimes.catalog()
	for i, descriptor := range catalog {
		catalog[i] = degradedDescriptor(descriptor)
	}
	return catalog
}
