package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

const continuityJSON = `{"summary":"Implemented local backups.","state":{"objective":"Reliable backups","done":["Local backup"],"next":["Test restore"],"open":[]},"facts":[{"class":"procedural","text":"Run restore checks before release."}]}`

func continuityFixture(t *testing.T) *brainService {
	return continuityEnabled(t, continuityBase(t))
}
func continuityBase(t *testing.T) *brainService {
	t.Helper()
	testHome(t)
	oldSessions, oldConv := workspaceSessions, conv
	workspaceSessions = newRuntimeSessions()
	conv = &Conversation{ID: "empty"}
	conv.cond = sync.NewCond(&conv.mu)
	// Fixtures are short and run without a local engine: stub both economies.
	oldLoaded, oldMin := continuityLoadedModel, continuityMinChars
	continuityLoadedModel, continuityMinChars = func() string { return engineRequestModel() }, 0
	t.Cleanup(func() {
		workspaceSessions, conv = oldSessions, oldConv
		continuityLoadedModel, continuityMinChars = oldLoaded, oldMin
	})
	return newBrainService(LoomHome())
}
func continuityEnabled(t *testing.T, s *brainService) *brainService {
	t.Helper()
	if err := s.storage.write("continuity.json", []byte(`{"enabled":true,"idle_minutes":10,"provider_id":"","model":"","consent":false}`)); err != nil {
		t.Fatal(err)
	}
	return s
}
func continuitySession(t *testing.T, id, project string, at time.Time) RuntimeSession {
	t.Helper()
	d := RuntimeSession{ID: id, ProjectID: project, RuntimeID: "fixture", UpdatedAt: at.UnixMilli(), Status: "idle", Messages: []Message{{Role: "system", Content: "PRIVATE SYSTEM"}, {Role: "user", Content: "Implement local backups"}, {Role: "tool", Content: "PRIVATE TOOL"}, {Role: "assistant", Content: "Implemented local backups"}}}
	if err := putStoreJSON(bkRuntimeSessions, id, d); err != nil {
		t.Fatal(err)
	}
	return d
}
func continuityReply(w http.ResponseWriter, raw string) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}}}})
}
func TestBrainContinuityIdleRetryUpdateAndFacts(t *testing.T) {
	s := continuityFixture(t)
	now := time.Now()
	d := continuitySession(t, "idle", "p", now.Add(-11*time.Minute))
	continuitySession(t, "recent", "p", now.Add(-9*time.Minute))
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/chat/completions" || strings.Contains(string(b), "PRIVATE") {
			t.Fatal(string(b), r.URL)
		}
		if calls == 1 {
			continuityReply(w, "bad JSON")
			return
		}
		raw := continuityJSON
		if calls == 3 {
			if strings.Contains(string(b), "Implement local backups") || !strings.Contains(string(b), "previous_summary") || !strings.Contains(string(b), "Reliable backups") {
				t.Fatal("incremental input", string(b))
			}
			raw = strings.ReplaceAll(raw, "Implemented local backups.", "Backups and restore complete.")
			raw = strings.ReplaceAll(raw, "Reliable backups", "Verified backups")
		}
		continuityReply(w, raw)
	})
	entries, err := s.runContinuity(context.Background(), "", now)
	if err != nil || len(entries) != 1 || calls != 2 || entries[0].SummaryID == "" || entries[0].StateID == "" {
		t.Fatalf("%+v %v calls=%d", entries, err, calls)
	}
	first := entries[0]
	if entries, err = s.runContinuity(context.Background(), "", now); err != nil || len(entries) != 0 || calls != 2 {
		t.Fatal("double run", entries, err, calls)
	}
	restarted := newBrainService(LoomHome())
	if entries, err = restarted.runContinuity(context.Background(), "", now); err != nil || len(entries) != 0 || calls != 2 {
		t.Fatal("checkpoint restart", entries, err, calls)
	}
	d.Messages = append(d.Messages, Message{Role: "user", Content: "Restore verified"}, Message{Role: "assistant", Content: "Restore complete"})
	if err = putStoreJSON(bkRuntimeSessions, d.ID, d); err != nil {
		t.Fatal(err)
	}
	entries, err = s.runContinuity(context.Background(), d.ID, now)
	if err != nil || calls != 3 || entries[0].SummaryID == first.SummaryID || entries[0].StateID == first.StateID {
		t.Fatal(entries, err, calls)
	}
	list, err := s.ListMemory(brain.MemoryFilter{Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	active, superseded, candidates := 0, 0, 0
	for _, item := range list.Items {
		if item.Scope != "project:p" || item.Provenance.DiscussionID != d.ID {
			t.Fatal(item)
		}
		switch item.Status {
		case "active":
			active++
			if len(item.Supersedes) != 1 {
				t.Fatal("history", item)
			}
		case "superseded":
			superseded++
		case "candidate":
			candidates++
		}
	}
	if active != 2 || superseded != 2 || candidates != 1 {
		t.Fatal(active, superseded, candidates, list)
	}
}
func TestBrainContinuityConsentProviderAndRunNow(t *testing.T) {
	s := continuityFixture(t)
	continuitySession(t, "recent", "", time.Now())
	if err := putStoreJSON(bkProviders, "cloud", CloudProvider{ID: "cloud", Endpoint: "https://chat.example/v1", Model: "default"}); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.keys["cloud"] = "provider-secret"
	cfg := `{"enabled":true,"idle_minutes":10,"provider_id":"cloud","model":"chosen","consent":false}`
	w := consolidationRequest(t, s.continuityHTTP, "POST", "/api/brain/continuity", cfg)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.URL.String() != "https://chat.example/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer provider-secret" || !strings.Contains(string(b), `"model":"chosen"`) {
			t.Fatal(r.URL, r.Header, string(b))
		}
		continuityReply(w, continuityJSON)
	})
	w = consolidationRequest(t, s.continuityRunHTTP, "POST", "/api/brain/continuity/run", `{"discussion_id":"recent"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "consent required") || calls != 0 {
		t.Fatal(w.Code, w.Body, calls)
	}
	if len(getBytes(bkBrainContinuity, "recent")) != 0 {
		t.Fatal("skipped checkpoint")
	}
	cfg = strings.Replace(cfg, `"consent":false`, `"consent":true`, 1)
	consolidationRequest(t, s.continuityHTTP, "POST", "/api/brain/continuity", cfg)
	w = consolidationRequest(t, s.continuityRunHTTP, "POST", "/api/brain/continuity/run", `{"discussion_id":"recent"}`)
	if w.Code != 200 || calls != 1 {
		t.Fatal(w.Code, w.Body, calls)
	}
	list, _ := s.ListMemory(brain.MemoryFilter{})
	for _, item := range list.Items {
		if item.Class == "working" && (item.Scope != "task:recent" || !hasName(item.Tags, "discussion-state")) || item.Class == "episodic" && item.Scope != "global" {
			t.Fatal(item)
		}
	}
	for i := 0; i < 25; i++ {
		_, _ = s.runContinuity(context.Background(), "recent", time.Now())
	}
	w = consolidationRequest(t, s.continuityStatusHTTP, "GET", "/api/brain/continuity/status", "")
	var status brainContinuityStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.Running || status.LastRun == 0 || len(status.Recent) != 20 || status.Recent[0].SkippedReason != "no new user turn" {
		t.Fatal(w.Body, err)
	}
	// Linked engines obey the same consent rule.
	oldNode, oldRead := engineNodeCache, engineNodeRead
	engineNodeCache, engineNodeRead = &engineNode{V1: "https://engine.example", APIKey: "engine-secret", Direct: true, Model: "native"}, true
	t.Cleanup(func() { engineNodeCache, engineNodeRead = oldNode, oldRead })
	if _, _, _, err := continuityDestination(brainContinuityConfig{}); err == nil {
		t.Fatal("linked engine lacks consent")
	}
	endpoint, key, model, err := continuityDestination(brainContinuityConfig{Consent: true})
	if err != nil || endpoint != "https://engine.example/v1/chat/completions" || key != "engine-secret" || model != "native" {
		t.Fatal(endpoint, key, model, err)
	}
}
func TestBrainContinuityConcurrentGenerationAndCancellation(t *testing.T) {
	s := continuityFixture(t)
	continuitySession(t, "discussion", "p", time.Now().Add(-time.Hour))
	entered, release := make(chan struct{}), make(chan struct{})
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			return
		case <-release:
			continuityReply(w, continuityJSON)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.runContinuity(ctx, "discussion", time.Now()); done <- err }()
	<-entered
	if _, err := s.runContinuity(ctx, "discussion", time.Now()); err == nil {
		t.Fatal("concurrent run accepted")
	}
	w := consolidationRequest(t, s.continuityStatusHTTP, "GET", "/api/brain/continuity/status", "")
	if !strings.Contains(w.Body.String(), `"running":true`) {
		t.Fatal(w.Body)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancellation ignored")
	}
	conv.Generating = true
	entries, err := s.runContinuity(context.Background(), "discussion", time.Now())
	if err != nil || entries[0].SkippedReason != "turn generating" {
		t.Fatal(entries, err)
	}
	conv.Generating = false
	// A new turn during summarization invalidates the result.
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		conv.mu.Lock()
		conv.Generating = true
		conv.mu.Unlock()
		continuityReply(w, continuityJSON)
	})
	entries, err = s.runContinuity(context.Background(), "discussion", time.Now())
	if err != nil || entries[0].SummaryID != "" || entries[0].SkippedReason == "" {
		t.Fatal(entries, err)
	}
	list, _ := s.ListMemory(brain.MemoryFilter{Status: "all"})
	if len(list.Items) != 0 {
		t.Fatal("stale writes", list)
	}
}
func TestBrainContinuityNativeBindingProjectStateAndIdentity(t *testing.T) {
	s := continuityFixture(t)
	at := time.Now().Add(-time.Hour).UnixMilli()
	conv.ID, conv.ActiveProject = "native", "p"
	conv.Log = []LogEvent{{TS: at, Delta: map[string]any{"user": "Implement backups"}}, {TS: at + 1, Delta: map[string]any{"content": "Done"}}}
	bound := RuntimeSession{ID: "bound", NativeArchive: "native", RuntimeID: "llama.cpp", ProjectID: "p", UpdatedAt: time.Now().UnixMilli()}
	if err := putStoreJSON(bkRuntimeSessions, "bound", bound); err != nil {
		t.Fatal(err)
	}
	continuitySession(t, "second", "p", time.Now().Add(-time.Hour))
	archive := convArchive{ID: "unbound", SavedAt: at, Messages: []Message{{Role: "user", Content: "Another task"}}}
	if err := saveArchive(&archive); err != nil {
		t.Fatal(err)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; continuityReply(w, continuityJSON) })
	entries, err := s.runContinuity(context.Background(), "", time.Now())
	if err != nil || len(entries) != 3 || calls != 3 {
		t.Fatal(entries, err, calls)
	}
	if entries[0].DiscussionID == "native" || entries[1].DiscussionID == "native" {
		t.Fatal("bound archive duplicated", entries)
	}
	list, _ := s.ListMemory(brain.MemoryFilter{})
	projectStates, summaries, taskStates := 0, 0, 0
	for _, item := range list.Items {
		if hasName(item.Tags, "project-state") {
			projectStates++
		}
		if hasName(item.Tags, "session-summary") {
			summaries++
		}
		if hasName(item.Tags, "discussion-state") {
			taskStates++
		}
	}
	if projectStates != 1 || summaries != 3 || taskStates != 1 {
		t.Fatal("identity dedupe", projectStates, summaries, taskStates)
	}
}
func TestBrainContinuitySettingsParsingTailAndAuth(t *testing.T) {
	s := continuityBase(t)
	w := consolidationRequest(t, s.continuityHTTP, "GET", "/api/brain/continuity", "")
	if strings.TrimSpace(w.Body.String()) != `{"enabled":false,"idle_minutes":10,"provider_id":"","model":"","consent":false,"loaded_only":true}` {
		t.Fatal(w.Body)
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":true,"idle_minutes":0,"provider_id":"","model":"","consent":false}`, `{"enabled":true,"idle_minutes":10,"provider_id":"","model":"","consent":false,"extra":1}`} {
		if w = consolidationRequest(t, s.continuityHTTP, "POST", "/api/brain/continuity", body); w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	for _, raw := range []string{`{}`, `{"summary":"x","state":{"objective":"x","done":[],"next":[],"open":[]},"facts":null}`, strings.Replace(continuityJSON, `"procedural"`, `"working"`, 1), continuityJSON + ` {}`, strings.Replace(continuityJSON, `"facts":`, `"extra":true,"facts":`, 1)} {
		if _, err := brain.ParseContinuity(raw); err == nil {
			t.Fatal("invalid JSON accepted", raw)
		}
	}
	tail := continuityTextTail([]Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: strings.Repeat("é", 20000)}, {Role: "tool", Content: "hidden"}}, 1)
	if len(tail) != 1 || len(tail[0].Text) > 24<<10 || !utf8.ValidString(tail[0].Text) {
		t.Fatal("bounded tail")
	}
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	for _, route := range []string{"continuity", "continuity/run", "continuity/status"} {
		w = consolidationRequest(t, mux.ServeHTTP, "POST", "/api/brain/"+route, `{}`)
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(route, w.Code)
		}
	}
}

func TestBrainContinuityDisabledInvalidOutputAndProjectMove(t *testing.T) {
	s := continuityFixture(t)
	d := continuitySession(t, "moving", "", time.Now().Add(-time.Hour))
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; continuityReply(w, continuityJSON) })
	disabled := `{"enabled":false,"idle_minutes":15,"provider_id":"","model":"","consent":false}`
	w := consolidationRequest(t, s.continuityHTTP, "POST", "/api/brain/continuity", disabled)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	restarted := newBrainService(LoomHome())
	w = consolidationRequest(t, restarted.continuityHTTP, "GET", "/api/brain/continuity", "")
	if strings.TrimSpace(w.Body.String()) != strings.TrimSuffix(disabled, "}")+`,"loaded_only":true}` {
		t.Fatal("settings not persisted", w.Body)
	}
	entries, err := s.runContinuity(context.Background(), "", time.Now())
	if err != nil || len(entries) != 0 || calls != 0 {
		t.Fatal(entries, err, calls)
	}
	entries, err = s.runContinuity(context.Background(), d.ID, time.Now())
	if err != nil || entries[0].SkippedReason != "continuity disabled" || calls != 0 {
		t.Fatal(entries, err, calls)
	}
	consolidationRequest(t, s.continuityHTTP, "POST", "/api/brain/continuity", strings.Replace(disabled, `"enabled":false`, `"enabled":true`, 1))
	entries, err = s.runContinuity(context.Background(), d.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	summaryID := entries[0].SummaryID
	d.ProjectID = "p"
	d.Messages = append(d.Messages, Message{Role: "user", Content: "Move this work to the project"})
	putStoreJSON(bkRuntimeSessions, d.ID, d)
	entries, err = s.runContinuity(context.Background(), d.ID, time.Now())
	if err != nil || entries[0].SummaryID != summaryID {
		t.Fatal("summary moved", entries, err)
	}
	list, _ := s.ListMemory(brain.MemoryFilter{Classes: []string{"episodic"}})
	if len(list.Items) != 1 || list.Items[0].Scope != "project:p" {
		t.Fatal(list)
	}
	d.Messages = append(d.Messages, Message{Role: "user", Content: "More work"})
	putStoreJSON(bkRuntimeSessions, d.ID, d)
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; continuityReply(w, `{"summary":"invalid"}`) })
	before := calls
	_, err = s.runContinuity(context.Background(), d.ID, time.Now())
	if err == nil || calls != before+2 {
		t.Fatal("validation retry", err, calls)
	}
	if s.continuity.status.LastError == "" || s.continuity.running {
		t.Fatal("error status", s.continuity.status)
	}
	var checkpoint brainContinuityCheckpoint
	getStoreJSON(bkBrainContinuity, d.ID, &checkpoint)
	if checkpoint.MessageCount == len(d.Messages) {
		t.Fatal("failed run advanced checkpoint")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { s.continuityLoop(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop not cancellable")
	}
}

func TestBrainContinuityLockedVaultAndBadCheckpoint(t *testing.T) {
	s := continuityFixture(t)
	d := continuitySession(t, "locked", "", time.Now().Add(-time.Hour))
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("locked or malformed run sent text") })
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	w := consolidationRequest(t, s.continuityRunHTTP, "POST", "/api/brain/continuity/run", `{"discussion_id":"locked"}`)
	if w.Code != 423 || len(getBytes(bkBrainContinuity, d.ID)) != 0 {
		t.Fatal(w.Code, w.Body)
	}
	SetConfigKey("MEM_ENCRYPTED", "off")
	if err := putStoreJSON(bkBrainContinuity, d.ID, brainContinuityCheckpoint{-1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.runContinuity(context.Background(), d.ID, time.Now()); err == nil {
		t.Fatal("negative checkpoint accepted")
	}
}
func TestBrainContinuityEconomies(t *testing.T) {
	s := continuityFixture(t)
	continuitySession(t, "short", "p", time.Now().Add(-time.Hour))
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; continuityReply(w, continuityJSON) })
	continuityMinChars = 1500
	if entries, err := s.runContinuity(context.Background(), "", time.Now()); err != nil || len(entries) != 0 || calls != 0 {
		t.Fatalf("one short exchange summarised: %+v %v", entries, err)
	}
	continuityMinChars = 0
	continuityLoadedModel = func() string { return "" }
	entries, err := s.runContinuity(context.Background(), "", time.Now())
	if err != nil || len(entries) != 1 || entries[0].SkippedReason != "local engine has no model loaded" || calls != 0 {
		t.Fatalf("model loaded for a summary: %+v %v", entries, err)
	}
	continuityLoadedModel = func() string { return "already-loaded" }
	var model string
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		model = body.Model
		continuityReply(w, continuityJSON)
	})
	if _, err := s.runContinuity(context.Background(), "", time.Now()); err != nil || model != "already-loaded" {
		t.Fatalf("summary should use the loaded model, got %q %v", model, err)
	}
}
