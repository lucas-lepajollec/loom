package loom

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// RuntimeAdapter is the execution seam, not an inference engine. Adapters own
// protocol translation; their upstream owns inference, tools and cancellation.
// Harness adapters will use native sessions rather than the local tool loop.
type RuntimeAdapter interface {
	Descriptor() RuntimeDescriptor
	Run(context.Context, RuntimeTurn, ChatCallback) ([]Message, error)
}

// Connectable discovers and saves a native catalog with explicit consent. The
// result preserves each adapter's existing models response for legacy clients;
// /api/workspace continues to expose normalized ModelChoice records.
type Connectable interface {
	Connect(context.Context, bool) (any, error)
}

// QuotaReader reads an account snapshot without generation or reset consumption.
// Throttling and caching belong to the HTTP layer, not to an adapter.
type QuotaReader interface {
	Quota(context.Context) (QuotaSnapshot, error)
}

type RuntimeDescriptor struct {
	Logo         string   `json:"logo,omitempty"`
	Available    bool     `json:"available"`
	InstallHint  string   `json:"install_hint,omitempty"`
	Docs         string   `json:"docs,omitempty"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Description  string   `json:"description"`
	CLI          string   `json:"cli"`
	Consent      string   `json:"consent"`
	Implemented  bool     `json:"implemented"`
	Capabilities []string `json:"capabilities"`
}

type RuntimeTurn struct {
	Messages    []Message
	Temperature float64
	Caps        Caps
	MaxTokens   int // Optional explicit output budget, used by benchmarks.
}

type llamaRuntimeAdapter struct{}

func (llamaRuntimeAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "llama.cpp", Name: "llama.cpp", Kind: "local", Description: "Moteur local llama.cpp, piloté par Loom.", Implemented: true, Capabilities: []string{"chat", "stream", "tools", "attachments", "cancel"}}
}

func (llamaRuntimeAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	return runChat(ctx, turn.Messages, turn.Temperature, turn.Caps, emit)
}

type runtimeRegistry struct {
	mu       sync.RWMutex
	adapters map[string]RuntimeAdapter
	order    []string
}

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{adapters: make(map[string]RuntimeAdapter)}
}

func (reg *runtimeRegistry) register(adapter RuntimeAdapter) error {
	if adapter == nil {
		return errors.New("adaptateur runtime requis")
	}
	d := adapter.Descriptor()
	if d.ID == "" || strings.TrimSpace(d.ID) != d.ID || strings.ContainsAny(d.ID, "/{}") {
		return errors.New("identifiant runtime invalide")
	}
	if !d.Implemented && len(d.Capabilities) != 0 {
		return errors.New("un runtime en préparation ne peut pas déclarer de capacités")
	}
	if hasRuntimeCapability(d, "connect") {
		if _, ok := adapter.(Connectable); !ok {
			return errors.New("capacité connect sans interface Connectable")
		}
	}
	if hasRuntimeCapability(d, "quota") {
		if _, ok := adapter.(QuotaReader); !ok {
			return errors.New("capacité quota sans interface QuotaReader")
		}
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.adapters[d.ID]; exists {
		return errors.New("runtime déjà enregistré")
	}
	reg.adapters[d.ID] = adapter
	reg.order = append(reg.order, d.ID)
	return nil
}

func (reg *runtimeRegistry) lookup(id string) (RuntimeAdapter, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	adapter, ok := reg.adapters[id]
	return adapter, ok
}

func (reg *runtimeRegistry) catalog() []RuntimeDescriptor {
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

func hasRuntimeCapability(d RuntimeDescriptor, capability string) bool {
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
