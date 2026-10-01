package loom

import "github.com/lucas-lepajollec/loom/internal/loom/runtime"

// Compatibility during the leaf-first migration: runtime owns the contracts and
// registry state; Loom supplies the existing chat and usage types and owns the
// adapter boundaries, startup registration and HTTP actions.
type RuntimeDescriptor = runtime.RuntimeDescriptor
type RuntimeTurn = runtime.RuntimeTurn[Message, Caps]
type RuntimeAdapter = runtime.RuntimeAdapter[Message, Caps, ChatCallback]
type Connectable = runtime.Connectable
type QuotaReader = runtime.QuotaReader[QuotaSnapshot]

// Historical unexported methods remain available to all callers, including ACP.
// The embedded owner contains the only registry map, insertion order and lock.
type runtimeRegistry struct {
	*runtime.Registry[Message, Caps, ChatCallback, QuotaSnapshot]
}

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{runtime.NewRegistry[Message, Caps, ChatCallback, QuotaSnapshot]()}
}
func (reg *runtimeRegistry) register(adapter RuntimeAdapter) error   { return reg.Register(adapter) }
func (reg *runtimeRegistry) upsert(adapter RuntimeAdapter)           { reg.Upsert(adapter) }
func (reg *runtimeRegistry) remove(id string)                        { reg.Remove(id) }
func (reg *runtimeRegistry) lookup(id string) (RuntimeAdapter, bool) { return reg.Lookup(id) }
func (reg *runtimeRegistry) catalog() []RuntimeDescriptor            { return reg.List() }
func hasRuntimeCapability(d RuntimeDescriptor, capability string) bool {
	return runtime.HasRuntimeCapability(d, capability)
}
