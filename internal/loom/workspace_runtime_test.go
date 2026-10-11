package loom

import (
	"testing"

)

func isolateRuntimeRegistry(t *testing.T, adapters ...RuntimeAdapter) {
	t.Helper()
	old := registeredRuntimes
 t.Cleanup(func(){registeredRuntimes=old})
	registeredRuntimes = newRuntimeRegistry()
	for _, a := range adapters {
		if err := registeredRuntimes.register(a); err != nil {
			t.Fatal(err)
		}
	}
}
