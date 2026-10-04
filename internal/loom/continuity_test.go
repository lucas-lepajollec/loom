package loom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/project"
)

func TestProjectContinuityHydratesNewExecutorsAndScopesReferences(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	brainSvc = nil
	brainSvcMu.Unlock()
	p, err := saveProjectContext(ChatProject{Name: "Portable", BrainSources: []string{"conversations"}, BrainBudget: 1500})
	if err != nil {
		t.Fatal(err)
	}
	selected := RuntimeSession{ID: "chosen", ProjectID: p.ID, Messages: []Message{{Role: "user", Content: "quartz chosen rationale"}}}
	other := RuntimeSession{ID: "excluded", Messages: []Message{{Role: "user", Content: "quartz excluded rationale"}}}
	for _, s := range []RuntimeSession{selected, other} {
		if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	p, err = saveProjectContext(ChatProject{ID: p.ID, Name: "Portable", BrainSources: []string{"conversations"}, BrainBudget: 1500, Continuity: &project.Continuity{Core: project.Core{Purpose: "Build a shared workspace", Rationale: "Keep context across executors", Decisions: "Keep source truth"}, WorkingState: "Verify mobile interactions"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{"llama.cpp", "openai-compatible", "fixture-harness"} {
		preview := prepareDiscussion(RuntimeSession{ID: "fresh-" + runtime, RuntimeID: runtime, ProjectID: p.ID}, "quartz")
		for _, part := range []string{p.Continuity.Core.Purpose, p.Continuity.Core.Rationale, p.Continuity.Core.Decisions, p.Continuity.WorkingState, "chosen rationale"} {
			if !strings.Contains(preview.Context.System, part) {
				t.Fatalf("%s lost %s", runtime, part)
			}
		}
		if strings.Contains(preview.Context.System, "excluded rationale") || preview.Context.EstimatedTokens <= 0 {
			t.Fatal("unselected reference leaked or missing inspector estimate")
		}
	}
	hits, err := theBrain().Search(brain.SearchRequest{Query: "quartz", ProjectID: p.ID})
	if err != nil || len(hits) != 1 {
		t.Fatalf("project MCP scope: %v %v", hits, err)
	}
	for _, prefixes := range [][]string{{}, {"discussion/excluded"}} {
		limited, err := theBrain().Search(brain.SearchRequest{Query: "quartz", ProjectID: p.ID, PathPrefixes: map[string][]string{"conversations": prefixes}})
		if err != nil || len(limited) != 0 {
			t.Fatalf("project scope expanded a narrower caller scope: %v %v", limited, err)
		}
	}
	native, err := nativePreparedContext("unbound-archive", p.ID, "quartz")
	if err != nil || strings.Count(native, "chosen rationale") != 1 || strings.Contains(native, "excluded rationale") {
		t.Fatalf("native local context must retrieve once for the current draft: %q %v", native, err)
	}
	all, _ := theBrain().Search(brain.SearchRequest{Query: "quartz", Sources: []string{"conversations"}})
	for _, hit := range all {
		if strings.Contains(hit.Path, "excluded") {
			if _, err := theBrain().Read(brain.ReadRequest{ProjectID: p.ID, ChunkID: hit.ChunkID}); err == nil {
				t.Fatal("known excluded chunk bypassed project scope")
			}
		}
	}
	partial, err := saveProjectContext(ChatProject{ID: p.ID, Name: p.Name})
	if err != nil || partial.Continuity == nil || partial.Continuity.Core.Rationale != p.Continuity.Core.Rationale || !partial.Continuity.StateUpdatedAt.Equal(p.Continuity.StateUpdatedAt) {
		t.Fatal("older client erased continuity")
	}
}

func TestGlobalPreferencesRequireExplicitExternalSelection(t *testing.T) {
	testHome(t)
	if err := MemSave("preferences", "", "Keep clear explanations."); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, sharedPreferencesKey, sharedPreferences{Page: "preferences"}); err != nil {
		t.Fatal(err)
	}
	local := discussionContext(RuntimeSession{RuntimeID: "llama.cpp"})
	external := discussionContext(RuntimeSession{RuntimeID: "openai-compatible"})
	if !strings.Contains(local.System, "Keep clear explanations.") || strings.Contains(external.System, "Keep clear explanations.") {
		t.Fatal("external sharing policy bypassed")
	}
	if err := putStoreJSON(bkState, sharedPreferencesKey, sharedPreferences{Page: "preferences", External: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(discussionContext(RuntimeSession{RuntimeID: "fixture-harness"}).System, "Keep clear explanations.") {
		t.Fatal("explicit shared preferences missing")
	}
	if err := MemDelete("preferences"); err != nil {
		t.Fatal(err)
	}
	if discussionContext(RuntimeSession{RuntimeID: "fixture-harness"}).Problem == "" {
		t.Fatal("missing selected preference silently discarded")
	}
	if _, err := nativePreparedContext("unbound", "", "new message"); err == nil {
		t.Fatal("native local send silently discarded missing selected preferences")
	}
}

func TestGlobalPreferencesHTTPAuthAndPageBounds(t *testing.T) {
	testHome(t)
	if err := storeWebKey("fixture-control"); err != nil {
		t.Fatal(err)
	}
	if err := MemSave("preferences", "", "Use clear answers."); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	webAPI(mux)("/api/context/preferences", handleSharedPreferences)
	call := func(method, body, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/context/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	for _, method := range []string{"GET", "POST"} {
		if w := call(method, `{}`, ""); w.Code != 401 {
			t.Fatalf("unprotected preference policy: %d", w.Code)
		}
	}
	for _, body := range []string{`{"page":"../outside"}`, `{"page":"missing"}`, `{"unknown":true}`} {
		if w := call("POST", body, "fixture-control"); w.Code != 400 {
			t.Fatalf("invalid selection accepted: %s %d", body, w.Code)
		}
	}
	if w := call("POST", `{"page":"preferences","external":false}`, "fixture-control"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "", "fixture-control"); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"page":"preferences"`) {
		t.Fatal("selection missing or cacheable", w.Body.String())
	}
}
