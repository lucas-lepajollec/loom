package runtime

import (
	"errors"
	"strings"
	"sync"
)

// Registry owns an ordered set of adapters and its lock. Construct it with
// NewRegistry; callers own the instance and startup registration policy.
type Registry[Message, Caps, Callback, Snapshot any] struct {
	mu       sync.RWMutex
	adapters map[string]RuntimeAdapter[Message, Caps, Callback]
	order    []string
}

func NewRegistry[Message, Caps, Callback, Snapshot any]() *Registry[Message, Caps, Callback, Snapshot] {
	return &Registry[Message, Caps, Callback, Snapshot]{adapters: make(map[string]RuntimeAdapter[Message, Caps, Callback])}
}

func (reg *Registry[Message, Caps, Callback, Snapshot]) Register(adapter RuntimeAdapter[Message, Caps, Callback]) error {
	if adapter == nil {
		return errors.New("runtime adapter required")
	}
	d := adapter.Descriptor()
	if d.ID == "" || strings.TrimSpace(d.ID) != d.ID || strings.ContainsAny(d.ID, "/{}") {
		return errors.New("invalid runtime ID")
	}
	if !d.Implemented && len(d.Capabilities) != 0 {
		return errors.New("a planned runtime cannot declare capabilities")
	}
	if HasRuntimeCapability(d, "connect") {
		if _, ok := adapter.(Connectable); !ok {
			return errors.New("connect capability without Connectable interface")
		}
	}
	if HasRuntimeCapability(d, "quota") {
		if _, ok := adapter.(QuotaReader[Snapshot]); !ok {
			return errors.New("quota capability without QuotaReader interface")
		}
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.adapters[d.ID]; exists {
		return errors.New("runtime already registered")
	}
	reg.adapters[d.ID] = adapter
	reg.order = append(reg.order, d.ID)
	return nil
}

// Upsert adds or replaces a runtime registered after startup (user-defined
// harnesses). Built-in entries are never replaced by this path.
func (reg *Registry[Message, Caps, Callback, Snapshot]) Upsert(adapter RuntimeAdapter[Message, Caps, Callback]) {
	d := adapter.Descriptor()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.adapters[d.ID]; !exists {
		reg.order = append(reg.order, d.ID)
	}
	reg.adapters[d.ID] = adapter
}

func (reg *Registry[Message, Caps, Callback, Snapshot]) Remove(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, ok := reg.adapters[id]; !ok {
		return
	}
	delete(reg.adapters, id)
	for i, x := range reg.order {
		if x == id {
			reg.order = append(reg.order[:i], reg.order[i+1:]...)
			break
		}
	}
}

func (reg *Registry[Message, Caps, Callback, Snapshot]) Lookup(id string) (RuntimeAdapter[Message, Caps, Callback], bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	adapter, ok := reg.adapters[id]
	return adapter, ok
}

func (reg *Registry[Message, Caps, Callback, Snapshot]) List() []RuntimeDescriptor {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	catalog := make([]RuntimeDescriptor, 0, len(reg.order))
	for _, id := range reg.order {
		d := reg.adapters[id].Descriptor()
		// Never expose a shared capability slice to callers. Empty is [], not null.
		d.Capabilities = append([]string{}, d.Capabilities...)
		catalog = append(catalog, d)
	}
	return catalog
}

func HasRuntimeCapability(d RuntimeDescriptor, capability string) bool {
	if !d.Implemented {
		return false
	}
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}
