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
