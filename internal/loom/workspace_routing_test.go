package loom

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestUnifiedDiscussionSwitchesLocalCloudLocalWithoutForking(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	requests := make(chan []Message, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		requests <- body.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Portable answer")+"data: [DONE]\n\n")
	}))
	defer srv.Close()
	SetConfigKey("PORT", strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	SetConfigKey("MODEL", "fixture.gguf")
	p, err := m.saveProvider(CloudProvider{Name: "Fixture", Endpoint: srv.URL + "/v1", Model: "model-a", Models: []string{"model-a", "model-b"}}, "fake-test-key")
	if err != nil {
		t.Fatal(err)
	}
	project, _ := saveProjectContext(ChatProject{Name: "Shared", Instructions: "Same shared instructions"})
	s, err := m.create(project.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if s.RuntimeID != "llama.cpp" {
		t.Fatal("new discussion is not local-first")
	}
	localPreview := prepareDiscussion(s, "Hello local")
	if err := m.start(s.ID, "local-turn-one", "Hello local", localPreview.Context.Revision); err != nil {
		t.Fatal(err)
	}
	first := awaitCloudFinished(t, m, s.ID)
	if first.Status != "complete" {
		t.Fatalf("local run failed: %+v", first)
	}
	if !reflect.DeepEqual(<-requests, localPreview.Messages) {
		t.Fatal("local wire differs from preview")
	}
	choiceID := cloudChoiceID(p.ID, "model-b")
	if _, err := m.selectModel(s.ID, choiceID, false); err == nil {
		t.Fatal("cloud transfer without consent")
	}
	selected, err := m.selectModel(s.ID, choiceID, true)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != s.ID || len(selected.Messages) != 2 || selected.Model != "model-b" {
		t.Fatal("selection forked or erased the discussion")
	}
	if err := m.start(s.ID, "route-changed", "Reject stale route", discussionContext(first).Revision); err == nil {
		t.Fatal("stale route accepted")
	}
	if err := m.start(s.ID, "cloud-turn-two", "Continue cloud"); err != nil {
		t.Fatal(err)
	}
	second := awaitCloudFinished(t, m, s.ID)
	if second.Status != "complete" {
		t.Fatalf("cloud run failed: %+v", second)
	}
	cloudMessages := <-requests
	if len(cloudMessages) != 4 || !strings.Contains(fmt.Sprint(cloudMessages), "Hello local") || !strings.Contains(fmt.Sprint(cloudMessages), "Same shared instructions") {
		t.Fatalf("cloud lost shared context: %+v", cloudMessages)
	}
	localID := ""
	for _, c := range modelCatalog(m.providers()) {
		if c.Kind == "local" {
			localID = c.ID
			break
		}
	}
	if _, err := m.selectModel(s.ID, localID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.start(s.ID, "local-turn-three", "Back to local"); err != nil {
		t.Fatal(err)
	}
	final := awaitCloudFinished(t, m, s.ID)
	localMessages := <-requests
	if len(final.Messages) != 6 || len(localMessages) != 6 || len(m.list()) != 1 || len(final.Turns) != 3 {
		t.Fatalf("lost history: %+v", final)
	}
	if final.Turns[0].RuntimeID != "llama.cpp" || final.Turns[1].Model != "model-b" || final.Turns[2].RuntimeID != "llama.cpp" {
		t.Fatal("turn attribution lost")
	}
	putStr(bkModelChoices, choiceID, "hidden")
	if _, err := m.selectModel(s.ID, choiceID, true); err == nil {
		t.Fatal("hidden model still selectable")
	}
	if len(m.list()) != 1 {
		t.Fatal("hiding model removed discussion")
	}
}

func TestUnifiedImportPreservesArchiveAndDropsNonportableState(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	a := &convArchive{ID: "old-discussion", Title: "Keep", Messages: []Message{{Role: "system", Content: "Private native prompt"}, {Role: "user", Content: "Portable question"}, {Role: "tool", Content: "Native tool state"}, {Role: "assistant", Content: "Portable answer"}}}
	if err := saveArchive(a); err != nil {
		t.Fatal(err)
	}
	s, err := m.importArchive(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Messages) != 2 || strings.Contains(fmt.Sprint(s.Messages), "Native") {
		t.Fatal("nonportable state leaked")
	}
	if original, ok := loadArchive(a.ID); !ok || len(original.Messages) != 4 {
		t.Fatal("archive destroyed")
	}
	again, err := m.importArchive(a.ID)
	if err != nil || again.ID != s.ID || len(m.list()) != 1 {
		t.Fatal("reimport forked discussion")
	}
}

func TestHarnessProfilesDoNotPretendToExecute(t *testing.T) {
	testHome(t)
	p, err := saveHarnessProfile(HarnessProfile{Name: "Code review", RuntimeID: "codex", ModelIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || len(harnessProfiles()) != 1 {
		t.Fatal("profile missing")
	}
	for _, r := range runtimeCatalog() {
		if r.ID == "hermes" && hasRuntimeCapability(r, "chat") {
			t.Fatal("unimplemented harness advertised as executable")
		}
	}
	if _, err := saveHarnessProfile(HarnessProfile{Name: "Bad", RuntimeID: "codex", ModelIDs: []string{"unknown"}}); err == nil {
		t.Fatal("unknown model accepted")
	}
}
