package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeInference(t *testing.T, kind string) (*httptest.Server, *[]string) {
	seen := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if kind == "vllm" {
				io.WriteString(w, `{"data":[{"id":"Qwen/Qwen3-8B","owned_by":"vllm","max_model_len":32768}]}`)
			} else {
				io.WriteString(w, `{"data":[{"id":"a.gguf","owned_by":"llamacpp"},{"id":"b.gguf","owned_by":"llamacpp"}]}`)
			}
		case "/props":
			if kind == "vllm" {
				w.WriteHeader(404)
				return
			}
			io.WriteString(w, `{"default_generation_settings":{"n_ctx":8192},"build_info":"b1"}`)
		case "/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*seen = append(*seen, body["model"].(string))
			io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func TestDirectEngineVLLMAndLlamaCpp(t *testing.T) {
	testHome(t)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	vllm, _ := fakeInference(t, "vllm")
	n, err := linkDirectEngine(context.Background(), vllm.URL+"/v1", "", "")
	if err != nil || !n.Direct || n.Kind != "vllm" || n.Model != "Qwen/Qwen3-8B" || n.Ctx != 32768 {
		t.Fatalf("%v %+v", err, n)
	}
	if engineRequestModel() != "Qwen/Qwen3-8B" || engineContextSize() != 32768 || engineBase() != vllm.URL {
		t.Fatalf("requêtes: %s %d %s", engineRequestModel(), engineContextSize(), engineBase())
	}
	llama, _ := fakeInference(t, "llama")
	n, err = linkDirectEngine(context.Background(), llama.URL, "", "b.gguf")
	if err != nil || n.Kind != "llama.cpp" || n.Ctx != 8192 || n.Model != "b.gguf" {
		t.Fatalf("%v %+v", err, n)
	}
	if _, err := linkDirectEngine(context.Background(), llama.URL, "", "absent"); err == nil {
		t.Fatal("modèle absent accepté")
	}
	// Engine routes are answered from the server, management is refused.
	rec := httptest.NewRecorder()
	nodeAware("/api/status", func(http.ResponseWriter, *http.Request) { t.Fatal("local") })(rec, httptest.NewRequest("GET", "/api/status", nil))
	if !strings.Contains(rec.Body.String(), `"health":true`) || !strings.Contains(rec.Body.String(), `"model_name":"b.gguf"`) {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	nodeAware("/api/models/delete", func(http.ResponseWriter, *http.Request) { t.Fatal("local") })(rec, httptest.NewRequest("POST", "/api/models/delete", nil))
	if rec.Code != 409 {
		t.Fatalf("gestion acceptée: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	nodeAware("/api/load-model", nil)(rec, httptest.NewRequest("POST", "/api/load-model", strings.NewReader(`{"model":"a.gguf"}`)))
	if rec.Code != 200 || currentEngineNode().Model != "a.gguf" {
		t.Fatalf("choix du modèle: %d %s", rec.Code, rec.Body.String())
	}
	if got := loomLocalModels(); len(got) != 2 {
		t.Fatalf("modèles pour les harnesses: %v", got)
	}
}
