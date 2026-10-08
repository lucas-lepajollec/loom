package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func primaryMemoryVault(t *testing.T, s *brainService) string {
	t.Helper()
	dir := t.TempDir()
	e, err := s.get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "primary", Label: "Vault", Path: dir, Kind: "context", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	return dir
}
func rememberContextItem(t *testing.T, class, scope, text string) brain.MemoryFile {
	t.Helper()
	kind := "reference"
	if class == "reflex" {
		kind = "user"
	}
	item, err := theBrain().MemoryWrite(brain.MemoryWrite{Scope: scope, Name: text, Description: "Context fixture", Type: kind, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

type memoryContextAdapter struct {
	id  string
	run func(RuntimeTurn, ChatCallback)
}

func (a memoryContextAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: a.id, Name: "Context fixture", Kind: "harness", Implemented: true, Capabilities: []string{"chat"}}
}
func (a memoryContextAdapter) Run(_ context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	a.run(turn, emit)
	return nil, nil
}
func continuityBase(t *testing.T) *brainService {
	t.Helper()
	testHome(t)
	oldSessions, oldConv := workspaceSessions, conv
	workspaceSessions = newRuntimeSessions()
	conv = &Conversation{ID: "empty"}
	conv.cond = sync.NewCond(&conv.mu)
	t.Cleanup(func() {
		theBrain().cancelMemoryConsolidation()
		theBrain().leaveJobs.Wait()
		transcriptJobs.Wait()
		theBrain().writeRefresh.Wait()
		workspaceSessions, conv = oldSessions, oldConv
	})
	return theBrain()
}
func continuityReply(w http.ResponseWriter, raw string) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}}}})
}
