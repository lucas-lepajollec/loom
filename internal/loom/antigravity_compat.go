package loom

import (
	"context"
	"io"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/antigravity"
)

// Historical names preserve the registry/session boundary and JSON shapes.
type agyUsage = antigravity.Usage
type agyResult = antigravity.Result
type HarnessEvent = antigravity.HarnessEvent

type antigravityAdapter struct {
	model      string
	executable string
}

func (antigravityAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "antigravity", Name: "Antigravity", Kind: "harness", Description: "Agent de Google. Loom utilise le CLI déjà connecté à ton compte.", CLI: "agy", Consent: "Loom lit la liste des modèles du CLI agy déjà connecté à ton compte. Aucun message n’est envoyé. Tes permissions natives restent actives.", Implemented: true, Capabilities: []string{"chat", "stream", "cancel", "usage", "native-events", "fresh-text-handoff", "connect", "quota"}}
}

func agyRead(ctx context.Context, args ...string) ([]byte, error) {
	return antigravity.Read(ctx, args...)
}
func discoverAgyModels(ctx context.Context) ([]string, error) { return antigravity.DiscoverModels(ctx) }
func validAgyUsage(u *agyUsage) bool                          { return antigravity.ValidUsage(u) }
func agyInstalled() bool                                      { return antigravity.Installed() }

func emitAntigravity(emit ChatCallback) func(antigravity.Event) bool {
	return func(e antigravity.Event) bool {
		var usage *RuntimeUsage
		if e.Usage != nil {
			u := e.Usage
			usage = &RuntimeUsage{Input: u.Input, Output: u.Output, Total: u.Total, Thinking: u.Thinking, Cached: u.Cached}
		}
		return emit(StreamEvent{Content: e.Content, Usage: usage, HarnessEvent: e.HarnessEvent, NativeSessionID: e.NativeSessionID, DurationSeconds: e.DurationSeconds})
	}
}
func consumeAgyStream(ctx context.Context, reader io.Reader, emit ChatCallback) (string, error) {
	return antigravity.ConsumeStream(ctx, reader, emitAntigravity(emit))
}
func (a antigravityAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	answer, err := (antigravity.Adapter{Model: a.model, Executable: a.executable}).Run(ctx, turn.Messages, emitAntigravity(emit))
	if err != nil {
		return nil, err
	}
	return []Message{{Role: "assistant", Content: answer}}, nil
}

func readAgyQuota(ctx context.Context) (QuotaSnapshot, error) {
	q := QuotaSnapshot{RuntimeID: "antigravity", Name: "Antigravity", Source: "agy /usage · compte natif", Windows: []QuotaWindow{}}
	native, err := antigravity.ReadQuota(ctx, agyRead)
	for _, w := range native.Windows {
		q.Windows = append(q.Windows, QuotaWindow{Group: w.Group, Name: w.Name, Remaining: w.Remaining, ResetAt: w.ResetAt})
	}
	q.Credits, q.FetchedAt = native.Credits, native.FetchedAt
	return q, err
}
