package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceContextIsExplicitAndDoesNotAlterHistory(t *testing.T) {
	testHome(t)
	p, err := createProject("Migration")
	if err != nil {
		t.Fatal(err)
	}
	// Existing projects remain usable without migration or injected boilerplate.
	if got := projectContext(p.ID); got != "" {
		t.Fatalf("legacy context = %q", got)
	}
	selected, err := saveCapability(Capability{Name: "Review", Instructions: "Check edge cases."})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saveCapability(Capability{Name: "Unselected", Instructions: "Never inject this."})
	if err != nil {
		t.Fatal(err)
	}
	p.Instructions = "Preserve existing behavior."
	p.Directory = t.TempDir()
	p.CapabilityIDs = []string{selected.ID, selected.ID}
	p, err = saveProjectContext(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.CapabilityIDs) != 1 {
		t.Fatal("duplicate skill persisted")
	}
	if err := renameProject(p.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := getProject(p.ID)
	if got.Instructions != p.Instructions || got.Directory != p.Directory {
		t.Fatal("rename lost context")
	}
	context := projectContext(p.ID)
	if !strings.Contains(context, selected.Instructions) || strings.Contains(context, "Never inject") || strings.Contains(context, p.Directory) {
		t.Fatalf("wrong context: %q", context)
	}
	history := []Message{{Role: "system", Content: "Global settings."}, {Role: "user", Content: "Hello"}}
	prepared := withProjectContext(history, context)
	if len(prepared) != 2 || prepared[0].Role != "system" || !strings.Contains(prepared[0].Content.(string), "Global settings.") {
		t.Fatalf("wrong prepared messages: %+v", prepared)
	}
	if history[0].Content != "Global settings." {
		t.Fatal("persisted history mutated")
	}
	if got := projectContext("missing"); got != "" {
		t.Fatalf("unknown project = %q", got)
	}
	// A deleted skill must not remain in the next turn's prompt.
	if err := putBytes(bkCapabilities, selected.ID, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(projectContext(p.ID), selected.Instructions) {
		t.Fatal("deleted skill still injected")
	}
}

func TestWorkspaceRejectsInvalidContextWithoutOverwriting(t *testing.T) {
	testHome(t)
	if _, err := saveProjectContext(ChatProject{Name: "Invalid", Directory: "relative"}); err == nil {
		t.Fatal("invalid project accepted")
	}
	if len(listProjects()) != 0 {
		t.Fatal("failed creation left an empty project")
	}
	created, err := saveProjectContext(ChatProject{Name: "New", Instructions: "New context"})
	if err != nil || created.ID == "" || created.CreatedAt == 0 {
		t.Fatalf("atomic creation failed: %+v %v", created, err)
	}
	p, _ := createProject("Keep")
	for _, tc := range []struct {
		name   string
		change func(*ChatProject)
	}{
		{"relative directory", func(p *ChatProject) { p.Directory = "relative/path" }},
		{"missing directory", func(p *ChatProject) { p.Directory = t.TempDir() + "/absent" }},
		{"unknown skill", func(p *ChatProject) { p.CapabilityIDs = []string{"absent"} }},
		{"large prompt", func(p *ChatProject) { p.Instructions = strings.Repeat("x", maxProjectInstructions+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := p
			tc.change(&changed)
			if _, err := saveProjectContext(changed); err == nil {
				t.Fatal("invalid context accepted")
			}
			got, _ := getProject(p.ID)
			if got.Name != p.Name || got.Directory != "" || got.Instructions != "" || len(got.CapabilityIDs) > 0 {
				t.Fatal("invalid update modified project")
			}
		})
	}
}

func TestWorkspaceHTTPBoundary(t *testing.T) {
	testHome(t)
	if err := storeWebKey("test-workspace-key"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct {
		path, method, body, contentType, auth string
		want                                  int
	}{
		{"/api/workspace", "GET", "", "", "", 401},
		{"/api/workspace", "GET", "", "", "Bearer test-workspace-key", 200},
		{"/api/workspace", "POST", "{}", "application/json", "Bearer test-workspace-key", 405},
		{"/api/capabilities/save", "GET", "", "", "Bearer test-workspace-key", 405},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "application/json", "", 401},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "text/plain", "Bearer test-workspace-key", 415},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check.","unexpected":true}`, "application/json", "Bearer test-workspace-key", 400},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."} {}`, "application/json", "Bearer test-workspace-key", 400},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "application/json", "Bearer test-workspace-key", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s: got %d want %d: %s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(listCapabilities()) != 1 {
		t.Fatal("invalid requests wrote capabilities")
	}
}

func TestLocalRuntimeDelegatesStreamingAndProjectContext(t *testing.T) {
	testHome(t)
	requests := make(chan []Message, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests <- req.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseChunk("Local response") + "data: [DONE]\n\n"))
	}))
	defer srv.Close()
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	_, err := localChatRuntime().Run(context.Background(), RuntimeTurn{Messages: withProjectContext([]Message{{Role: "user", Content: "Hello"}}, "Project context."), Temperature: 0.7}, func(ev StreamEvent) bool { answer.WriteString(ev.Content); return true })
	if err != nil {
		t.Fatal(err)
	}
	received := <-requests
	if answer.String() != "Local response" || len(received) != 2 || received[0].Content != "Project context." {
		t.Fatalf("delegation failed: %q %+v", answer.String(), received)
	}
}
