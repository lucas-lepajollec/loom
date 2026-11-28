package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
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
	// This machine's own engine management keeps working.
	called := false
	nodeAware("/api/llamacpp", func(http.ResponseWriter, *http.Request) { called = true })(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/llamacpp", nil))
	if !called {
		t.Fatal("la gestion de llama.cpp de cette machine doit rester locale")
	}
	rec = httptest.NewRecorder()
	nodeAware("/api/load-model", nil)(rec, httptest.NewRequest("POST", "/api/load-model", strings.NewReader(`{"model":"a.gguf"}`)))
	if rec.Code != 200 || currentEngineNode().Model != "a.gguf" {
		t.Fatalf("choix du modèle: %d %s", rec.Code, rec.Body.String())
	}
	if got := engineModelValues(); len(got) != 2 {
		t.Fatalf("modèles pour les harnesses: %v", got)
	}
}

func TestDirectEngineNativeIdentificationAndMetadata(t *testing.T) {
	cases := []struct {
		kind, modelList, expectedModel string
		native                         map[string]string
		context                        int
	}{
		{"ollama", `{"data":[{"id":"qwen3:8b"}]}`, "qwen3:8b", map[string]string{
			"/api/version": `{"version":"0.20.0"}`,
			"/api/tags":    `{"models":[{"name":"qwen3:8b","size":5000000,"details":{"family":"qwen3","quantization_level":"Q4_K_M"}},{"name":"qwen3:4b","size":2000000}]}`,
			"/api/ps":      `{"models":[{"name":"qwen3:8b","context_length":8192}]}`}, 8192},
		{"lmstudio", `{"data":[{"id":"model"}]}`, "model", map[string]string{
			"/api/v0/models": `{"data":[{"id":"model","type":"llm","state":"not-loaded","quantization":"Q4_K_M","max_context_length":32768,"arch":"qwen3"},{"id":"embedding","type":"embeddings"}]}`}, 32768},
		{"sglang", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/get_model_info": `{"model_path":"model","context_length":16384,"version":"0.5"}`}, 16384},
		{"sglang", `{"data":[{"id":"model","owned_by":"sglang","max_model_len":4096}]}`, "model", nil, 4096},
		{"mlx", `{"data":[{"id":"mlx-community/model"}]}`, "mlx-community/model", nil, 0},
		{"mlx", `{"data":[{"id":"model","mlx":{"quantization":4}}]}`, "model", nil, 0},
		{"tabbyapi", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/v1/model/list": `{"data":[{"id":"model"}]}`}, 0},
		{"koboldcpp", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/api/extra/version": `{"result":"KoboldCpp","version":"1.111"}`}, 0},
		{"lemonade", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/api/v0/health": `{"status":"ok","model_loaded":"model","max_context_tokens":4096}`}, 4096},
		{"llama.cpp", `{"data":[{"id":"model","status":{"value":"unloaded"}}]}`, "model", map[string]string{"/props": `{"role":"router","n_ctx":4096}`}, 4096},
		{"ik_llama.cpp", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/props": `{"build_info":"ik_llama.cpp abc123","n_ctx":4096}`}, 4096},
		{"strata", `{"data":[{"id":"model","owned_by":"strata","max_model_len":131072}]}`, "model", nil, 131072},
		{"strata", `{"data":[{"id":"model"}]}`, "model", map[string]string{
			"/health": `{"status":"ok","service":"strata","max_context":32768}`,
			"/props":  `{"n_ctx":32768}`,
		}, 32768},
		{"basalt", `{"data":[{"id":"model","owned_by":"basalt","status":{"value":"loaded"},"meta":{"n_ctx":32768}}]}`, "model", nil, 32768},
		{"basalt", `{"data":[{"id":"mlx-community/model"}]}`, "mlx-community/model", map[string]string{
			"/health": `{"status":"ok","service":"basalt","max_context":65536}`,
			"/props":  `{"n_ctx":65536}`,
		}, 65536},
		{"mistral.rs", `{"data":[{"id":"model","owned_by":"local","status":"loaded","max_model_len":131072}]}`, "model", map[string]string{
			"/v1/system/info": `{"devices":[{"kind":"CPU"}],"build":{"version":"0.9.5","cuda":false,"metal":false}}`,
		}, 131072},
		{"mistral.rs", `{"data":[{"id":"model","owned_by":"mistralrs","status":"loaded","max_model_len":32768}]}`, "model", nil, 32768},
		{"ktransformers", `{"object":"list","data":[{"id":"model","name":"model"}]}`, "model", map[string]string{
			"/openapi.json": `{"openapi":"3.1.0","info":{"title":"ktransformers server","version":"1.0.0"}}`,
		}, 0},
		{"exo", `{"data":[{"id":"mlx-community/model","owned_by":"exo","context_length":65536}]}`, "mlx-community/model", nil, 65536},
		{"exo", `{"data":[{"id":"mlx-community/model","context_length":32768}]}`, "mlx-community/model", map[string]string{
			"/state": `{"topology":{"nodes":[]},"instances":{},"runners":{}}`,
		}, 32768},
		{"openai", `{"data":[{"id":"mistralai/model","owned_by":"mistralai","status":"loaded","meta":"fixture metadata"}]}`, "mistralai/model", map[string]string{
			"/health":         `{"status":"ok","service":"other"}`,
			"/v1/system/info": `{"devices":[],"build":{"version":"0.9.5"}}`,
			"/openapi.json":   `{"info":{"title":"FastAPI"}}`,
			"/state":          `{"topology":{},"instances":{}}`,
		}, 0},
		{"openai", `{"data":[{"id":"model"}]}`, "model", map[string]string{"/props": `{"status":"ok"}`, "/api/v0/health": `{"status":"ok"}`}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing key")
				}
				if r.URL.Path == "/v1/models" {
					io.WriteString(w, tc.modelList)
					return
				}
				if response, ok := tc.native[r.URL.Path]; ok {
					io.WriteString(w, response)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			got, err := probeDirectEngine(context.Background(), server.URL, "fixture")
			if err != nil || got.Kind != tc.kind || len(got.Models) == 0 || got.Models[0] != tc.expectedModel || got.contextFor(tc.expectedModel) != tc.context {
				t.Fatalf("probe = %+v, %v", got, err)
			}
			detail := got.Details[tc.expectedModel]
			switch tc.kind {
			case "ollama":
				if got.Version != "0.20.0" || detail.Loaded == nil || !*detail.Loaded || detail.Quantization != "Q4_K_M" || detail.Size != 5000000 ||
					got.Details["qwen3:4b"].Loaded == nil || *got.Details["qwen3:4b"].Loaded {
					t.Fatalf("Ollama detail = %+v", got)
				}
			case "lmstudio":
				if len(got.Models) != 1 || detail.Loaded == nil || *detail.Loaded || detail.Architecture != "qwen3" || detail.Quantization != "Q4_K_M" {
					t.Fatalf("LM Studio detail = %+v", got)
				}
			case "mistral.rs":
				if got.Router || (tc.native != nil && got.Version != "0.9.5") {
					t.Fatalf("mistral.rs metadata = %+v", got)
				}
			case "llama.cpp":
				if !got.Router {
					t.Fatal("router not detected")
				}
			}
		})
	}
}

func TestDirectOllamaManagementUsesLinkedOriginAndPolicy(t *testing.T) {
	testHome(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer native-fixture" {
			t.Errorf("wrong native request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "qwen3:8b" {
			t.Errorf("invalid model: %v", body)
		}
		calls.Add(1)
		switch r.URL.Path {
		case "/api/pull":
			io.WriteString(w, "{\"status\":\"pulling\"}\n{\"status\":\"success\"}\n")
		case "/api/generate":
			if body["keep_alive"] != float64(0) || body["stream"] != false {
				t.Errorf("bad unload: %v", body)
			}
			io.WriteString(w, `{"done":true,"done_reason":"unload"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	call := func(h http.HandlerFunc, body string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, vllmJSONPost(strings.NewReader(body)))
		if rec.Code != want {
			t.Fatalf("%d want %d: %s", rec.Code, want, rec.Body)
		}
	}
	call(handleOllamaPull, `{"model":"qwen3:8b"}`, 409)
	if err := setEngineNode(&engineNode{Direct: true, Kind: "ollama", V1: server.URL, APIKey: "native-fixture"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"model":"qwen3:8b","url":"http://127.0.0.1:2"}`, `{"model":"../qwen3"}`, `{"model":"example.invalid/ns/model"}`, `{"model":"https://example.invalid/model"}`} {
		call(handleOllamaPull, body, 400)
	}
	call(handleOllamaPull, `{"model":"qwen3:8b"}`, 200)
	call(handleOllamaUnload, `{"model":"qwen3:8b"}`, 200)
	savePolicyRules(t, policy.Rule{ID: "deny", Scope: "machine", ScopeID: "local", Subject: "node.install", Decision: policy.Deny})
	call(handleOllamaPull, `{"model":"qwen3:8b"}`, 403)
	if calls.Load() != 2 {
		t.Fatalf("called native engine %d times", calls.Load())
	}
}
