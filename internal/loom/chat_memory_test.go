package loom

import (
	"testing"
)

func TestMemModeDropsAlwaysAndDefault(t *testing.T) {
	testHome(t)
	if got := memMode(); got != MemOff {
		t.Fatalf("défaut: got %q", got)
	}
	if err := SetConfigKey("MEM_MODE", "always"); err != nil {
		t.Fatal(err)
	}
	if got := memMode(); got != MemOff {
		t.Fatalf("always doit retomber sur off, got %q", got)
	}
	if err := SetConfigKey("MEM_MODE", "ondemand"); err != nil {
		t.Fatal(err)
	}
	if got := memMode(); got != MemOnDemand {
		t.Fatalf("ondemand: got %q", got)
	}
}
