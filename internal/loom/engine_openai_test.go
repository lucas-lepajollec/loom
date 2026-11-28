package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEngineOpenAIDescribesTheAPIWithoutSecrets(t *testing.T) {
	testHome(t)
	if !engineRoutes["/api/engine/openai"] {
		t.Fatal("the API description must follow a linked engine")
	}
	w := httptest.NewRecorder()
	handleEngineOpenAI(w, httptest.NewRequest(http.MethodGet, "/api/engine/openai", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["url_local"] != "http://127.0.0.1:8081/v1" || out["exposed"] != false {
		t.Fatalf("local address: %v", out)
	}
	if _, ok := out["models"].([]any); !ok {
		t.Fatalf("models must be a list: %v", out["models"])
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "sk-loom") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("no secret in the description: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	handleEngineOpenAI(w, httptest.NewRequest(http.MethodPost, "/api/engine/openai", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST answered %d", w.Code)
	}
}

func TestOpenAIHarnessSnippetsUseLoomEndpointAndPlaceholder(t *testing.T) {
	testHome(t)
	if err := writeAPIKey("loom-inference-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := setEngineNode(&engineNode{Direct: true, Kind: "sglang", Model: "Qwen/Qwen3-8B", V1: "http://127.0.0.1:39289", APIKey: "native-inference-fixture"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handleHarnessSnippets(rec, httptest.NewRequest("GET", "http://127.0.0.1:2510/api/engines/harness-snippets", nil))
	var out struct {
		Model       string
		Local       string `json:"url_local"`
		Required    bool   `json:"key_required"`
		Placeholder string `json:"key_placeholder"`
		Snippets    map[string]harnessSnippet
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Model != "Qwen/Qwen3-8B" ||
		out.Local != "http://127.0.0.1:2510/v1" || !out.Required || out.Placeholder != loomKeyPlaceholder || len(out.Snippets) != 6 {
		t.Fatalf("snippets = %d %s", rec.Code, rec.Body)
	}
	for id, snippet := range out.Snippets {
		if !strings.Contains(snippet.Content, out.Local) || !strings.Contains(snippet.Content, out.Model) ||
			strings.Contains(snippet.Content, "inference-fixture") || strings.Contains(snippet.Content, "39289") {
			t.Errorf("%s snippet uses an invalid address, model or credential", id)
		}
	}
	if !json.Valid([]byte(out.Snippets["opencode"].Content)) || !json.Valid([]byte(out.Snippets["pi"].Content)) ||
		!strings.Contains(out.Snippets["codex"].Content, `wire_api = "responses"`) ||
		!strings.Contains(out.Snippets["claude"].Content, `export ANTHROPIC_BASE_URL='http://127.0.0.1:2510'`) {
		t.Fatalf("invalid client configurations: %+v", out.Snippets)
	}
}

func TestOpenAIHarnessEndpointAuthenticatesAndForwardsNativeWire(t *testing.T) {
	testHome(t)
	if err := writeAPIKey("loom-inference-fixture"); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer native-inference-fixture" ||
			r.Header.Get("Cookie") != "" || r.Header.Get("X-API-Key") != "" {
			t.Errorf("invalid upstream request: %s %v", r.URL.Path, r.Header)
		}
		var body struct{ Model, Input string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "Qwen/Qwen3-8B" || body.Input != "fixture" {
			t.Errorf("upstream request changed: %+v", body)
		}
		w.Write([]byte(`{"id":"fixture"}`))
	}))
	defer backend.Close()
	if err := setEngineNode(&engineNode{Direct: true, Kind: "sglang", Model: "Qwen/Qwen3-8B", V1: backend.URL, APIKey: "native-inference-fixture"}); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct {
		path, key, origin string
		code              int
	}{
		{"/v1/responses", "", "", 401},
		{"/v1/responses", "loom-inference-fixture", "https://fixture.invalid", 403},
		{"/v1/model/load", "loom-inference-fixture", "", 404},
		{"/v1/responses", "loom-inference-fixture", "", 200},
	} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:2510"+tc.path, strings.NewReader(`{"model":"loom","input":"fixture"}`))
		req.RemoteAddr = "127.0.0.1:42157"
		req.Header.Set("Authorization", "Bearer "+tc.key)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Cookie", "synthetic-cookie")
		req.Header.Set("X-API-Key", "synthetic-unused")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%s = %d, want %d: %s", tc.path, rec.Code, tc.code, rec.Body)
		}
	}
}
