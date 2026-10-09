package loom

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatDefaultChoiceSettingsAndPrecedence(t *testing.T) {
	jarvisTestSetup(t)
	request := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/chat/settings", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handleChatSettings(w, r)
		return w
	}
	for _, body := range []string{`{"default_choice":"local:fixture.gguf"}`, `{}`} {
		w := request("POST", body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"default_choice":"local:fixture.gguf"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if ReadConfig()[chatDefaultChoiceKey] != "local:fixture.gguf" {
		t.Fatal("not persisted")
	}
	for _, id := range []string{"local:missing", "codex:missing", "cloud:missing"} {
		if w := request("POST", `{"default_choice":"`+id+`"}`); w.Code != 400 {
			t.Fatal(w.Body.String())
		}
	}
	if w := request("GET", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "local:fixture.gguf") {
		t.Fatal(w.Body.String())
	}
	p := ChatProject{ID: "project", DefaultChoice: "pi:loom-provider/model"}
	if err := putStoreJSON(bkProjects, p.ID, p); err != nil {
		t.Fatal(err)
	}
	choices := []ModelChoice{{ID: "local:fixture.gguf", Kind: "local", Enabled: true}, {ID: p.DefaultChoice, Kind: "harness", Enabled: true, Ready: true}, {ID: "cloud:connected", Kind: "cloud", Enabled: true, Ready: true}, {ID: "cloud:offline", Kind: "cloud", Enabled: true}}
	for _, tc := range []struct{ project, global, want string }{
		{p.ID, "local:fixture.gguf", p.DefaultChoice},
		{"", "local:fixture.gguf", "local:fixture.gguf"},
		{"", "cloud:connected", "cloud:connected"},
		{"", "cloud:offline", ""},
		{"", "cloud:gone", ""},
		{"", "", ""},
	} {
		c := initialDiscussionChoice(tc.project, tc.global, choices)
		got := ""
		if c != nil {
			got = c.ID
		}
		if got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
	choices[1].Enabled = false
	if initialDiscussionChoice(p.ID, "local:fixture.gguf", choices) != nil {
		t.Fatal("unavailable project silently rerouted")
	}
	s := RuntimeSession{ID: "existing", RuntimeID: "llama.cpp", Model: "fixture.gguf"}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", `{"default_choice":""}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, _ := workspaceSessions.get(s.ID)
	if saved.Model != s.Model {
		t.Fatal("existing discussion changed")
	}
	if err := applyLiveConfig("MODEL=fixture.gguf\n", ""); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()[chatDefaultChoiceKey] != "" {
		t.Fatal("clear failed")
	}
}

func TestChatDefaultSurvivesPresetAndResetReturnsChoice(t *testing.T) {
	jarvisTestSetup(t)
	if err := SetConfigKey(chatDefaultChoiceKey, "local:fixture.gguf"); err != nil {
		t.Fatal(err)
	}
	if err := applyLiveConfig("MODEL=fixture.gguf\n", ""); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()[chatDefaultChoiceKey] != "local:fixture.gguf" {
		t.Fatal("preset lost default")
	}
	w := httptest.NewRecorder()
	handleChatReset(w, httptest.NewRequest("POST", "/api/chat/reset", strings.NewReader(`{}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"initial_choice":{"id":"local:fixture.gguf"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
