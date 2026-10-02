package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func vllmJSONPost(body io.Reader) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestVLLMParamsArgs(t *testing.T) {
	values := map[string]string{
		"max-model-len": "8192", "gpu-memory-utilization": "0.85", "tensor-parallel-size": "2",
		"dtype": "bfloat16", "quantization": "awq", "kv-cache-dtype": "fp8", "enable-prefix-caching": "off",
		"max-num-seqs": "16", "enforce-eager": "on", "cpu-offload-gb": "2.5", "swap-space": "0",
		"reasoning-parser": "deepseek_r1", "enable-auto-tool-choice": "on", "tool-call-parser": "auto", "trust-remote-code": "off",
	}
	args, err := buildVLLMArgs("Qwen/Qwen3-8B", 8090, values, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"serve", "Qwen/Qwen3-8B", "--host", "127.0.0.1", "--port", "8090", "--max-model-len", "8192", "--gpu-memory-utilization", "0.85", "--tensor-parallel-size", "2", "--dtype", "bfloat16", "--quantization", "awq", "--kv-cache-dtype", "fp8", "--no-enable-prefix-caching", "--max-num-seqs", "16", "--enforce-eager", "--cpu-offload-gb", "2.5", "--swap-space", "0", "--enable-auto-tool-choice", "--reasoning-parser", "deepseek_r1", "--tool-call-parser", "hermes"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q", args)
	}
	args, err = buildVLLMArgs("unknown/model", 8000, nil, 1)
	if err != nil || strings.Contains(strings.Join(args, " "), "parser") || strings.Contains(strings.Join(args, " "), "trust") {
		t.Fatalf("unsafe defaults: %v %v", args, err)
	}
	values["trust-remote-code"] = "on"
	values["reasoning-parser"] = "none"
	args, err = buildVLLMArgs("Qwen/Qwen3-8B", 8090, values, 2)
	if err != nil || !strings.Contains(strings.Join(args, " "), "--trust-remote-code") || strings.Contains(strings.Join(args, " "), "--reasoning-parser") {
		t.Fatalf("explicit overrides: %v %v", args, err)
	}
	for _, n := range []int{0, 1, 2} {
		found, danger := false, false
		for _, p := range vllmParams(n) {
			found = found || p.ID == "tensor-parallel-size"
			if p.ID == "trust-remote-code" {
				danger = p.Dangerous && p.Default == "off"
			}
		}
		if found != (n > 1) || !danger {
			t.Fatalf("catalog for %d GPUs", n)
		}
	}
}

func TestVLLMValidation(t *testing.T) {
	for _, values := range []map[string]string{
		{"max-model-len": "0"}, {"max-model-len": "1.5"}, {"gpu-memory-utilization": "0"}, {"gpu-memory-utilization": "1.01"},
		{"gpu-memory-utilization": "NaN"}, {"swap-space": "Inf"}, {"cpu-offload-gb": "-1"}, {"max-num-seqs": "-1"},
		{"tensor-parallel-size": "3"}, {"dtype": "float64"}, {"quantization": "notreal"}, {"kv-cache-dtype": "bad"},
		{"trust-remote-code": "yes"}, {"reasoning-parser": "x;rm"}, {"tool-call-parser": "--plugin"}, {"extra-args": "--host 0.0.0.0"},
	} {
		if _, err := buildVLLMArgs("Qwen/Qwen3-8B", 8000, values, 2); err == nil {
			t.Errorf("accepted %v", values)
		}
	}
	if _, err := buildVLLMArgs("unknown/model", 8000, map[string]string{"enable-auto-tool-choice": "on"}, 1); err == nil {
		t.Fatal("guessed unknown tool parser")
	}
	if _, err := buildVLLMArgs("x", 8000, map[string]string{"tensor-parallel-size": "1"}, 1); err == nil {
		t.Fatal("TP offered on one GPU")
	}
	for _, model := range []string{"../etc", "x/y/z", "x;rm", "-option", "x//y"} {
		if _, err := buildVLLMArgs(model, 8000, nil, 1); err == nil {
			t.Fatal(model)
		}
	}
}

func TestVLLMParamsSavedPerModel(t *testing.T) {
	testHome(t)
	w := httptest.NewRecorder()
	handleVLLMParams(w, vllmJSONPost(strings.NewReader(`{"model":"Qwen/Qwen3-8B","values":{"max-model-len":"4096","reasoning-parser":"none","trust-remote-code":"off"}}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	values, err := loadVLLMParams("Qwen/Qwen3-8B")
	if err != nil || values["max-model-len"] != "4096" {
		t.Fatalf("%v %v", values, err)
	}
	other, err := loadVLLMParams("another/model")
	if err != nil || len(other) != 0 {
		t.Fatal("settings leaked to another model")
	}
	args, err := buildVLLMArgs("Qwen/Qwen3-8B", 8000, values, 1)
	if err != nil || !strings.Contains(strings.Join(args, " "), "--max-model-len 4096") {
		t.Fatal(args, err)
	}
	w = httptest.NewRecorder()
	handleVLLMParams(w, httptest.NewRequest("GET", "/?model=Qwen%2FQwen3-8B", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"max-model-len":"4096"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	handleVLLMParams(w, vllmJSONPost(strings.NewReader(`{"model":"Qwen/Qwen3-8B","values":{"max-model-len":"-1"}}`)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	values, _ = loadVLLMParams("Qwen/Qwen3-8B")
	if values["max-model-len"] != "4096" {
		t.Fatal("invalid save changed settings")
	}
}

func vllmFixtureFile(t *testing.T, root, name, data string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVLLMCacheListing(t *testing.T) {
	cache := t.TempDir()
	vllmFixtureFile(t, cache, "models--org--ready/snapshots/abc/config.json", "{}")
	vllmFixtureFile(t, cache, "models--org--ready/blobs/weights", "123456")
	if err := os.Symlink("../../blobs/weights", filepath.Join(cache, "models--org--ready/snapshots/abc/model.safetensors")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	vllmFixtureFile(t, cache, "models--org--gguf/snapshots/abc/model.gguf", "GGUF")
	vllmFixtureFile(t, cache, "models--org--partial/snapshots/abc/config.json", "{}")
	vllmFixtureFile(t, cache, "models--org--partial/snapshots/abc/a.safetensors", "first shard")
	vllmFixtureFile(t, cache, "models--org--partial/snapshots/abc/model.safetensors.index.json", `{"weight_map":{"a":"a.safetensors","b":"b.safetensors"}}`)
	vllmFixtureFile(t, cache, "datasets--org--ignored/data", "ignored")
	models, err := listVLLMCache(cache)
	if err != nil || len(models) != 3 {
		t.Fatal(models, err)
	}
	if models[0].Servable || models[1].Servable || !models[2].Servable || models[2].Size != 8 {
		t.Fatalf("%+v", models)
	}
	vllmFixtureFile(t, cache, "models--org--ready/snapshots/abc/.loom-prefetch", "incomplete")
	models, err = listVLLMCache(cache)
	if err != nil || models[2].Servable {
		t.Fatal("partial prefetch shown ready")
	}
	models, err = listVLLMCache(filepath.Join(cache, "absent"))
	if err != nil || len(models) != 0 {
		t.Fatal(models, err)
	}
}

func TestVLLMCachePathsAndDelete(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("HF_HOME", cache)
	t.Setenv("HF_HUB_CACHE", "")
	if vllmHFCache() != filepath.Join(cache, "hub") {
		t.Fatal(vllmHFCache())
	}
	t.Setenv("HF_HUB_CACHE", cache)
	vllmFixtureFile(t, cache, "models--org--model/snapshots/abc/config.json", "{}")
	old := vllm
	vllm = &vllmState{job: "start", model: "org/model"}
	t.Cleanup(func() { vllm = old })
	if err := deleteVLLMCache("org/model"); err == nil {
		t.Fatal("deleted loading model")
	}
	vllm.job = ""
	if err := deleteVLLMCache("../outside"); err == nil {
		t.Fatal("path traversal")
	}
	if err := deleteVLLMCache("org/model"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "models--org--model")); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
}

func TestVLLMHubSearchParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "safetensors" || r.URL.Query().Get("pipeline_tag") != "text-generation" || r.URL.Query().Get("search") != "Qwen" {
			t.Error(r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("missing HF token")
		}
		io.WriteString(w, `[
		{"id":"org/Small-AWQ","pipeline_tag":"text-generation","siblings":[{"rfilename":"config.json"},{"rfilename":"model.safetensors","lfs":{"size":1000000000}}]},
		{"id":"org/Large-8B","pipeline_tag":"text-generation","siblings":[{"rfilename":"config.json"},{"rfilename":"model.safetensors","size":20000000000}]},
		{"id":"org/Unknown","pipeline_tag":"text-generation","siblings":[{"rfilename":"config.json"},{"rfilename":"model.safetensors"}]},
		{"id":"org/GGUF","pipeline_tag":"text-generation","siblings":[{"rfilename":"model.gguf"}]},
		{"id":"org/Embedding","pipeline_tag":"feature-extraction","siblings":[{"rfilename":"config.json"},{"rfilename":"model.safetensors"}]}]`)
	}))
	defer server.Close()
	old := hubAPIBase
	hubAPIBase = server.URL
	t.Cleanup(func() { hubAPIBase = old })
	t.Setenv("HF_TOKEN", "synthetic")
	models, err := searchVLLMHub(context.Background(), "Qwen", "", []map[string]any{{"total": 16384}, {"total": 16384}})
	if err != nil || len(models) != 3 {
		t.Fatal(models, err)
	}
	if models[0]["verdict"] != "fits" || models[1]["verdict"] != "tensor_parallel" || models[2]["estimated_vram_mb"] != nil || models[2]["size"] != nil {
		t.Fatal(models)
	}
	models, err = searchVLLMHub(context.Background(), "Qwen", "awq", nil)
	if err != nil || len(models) != 1 || models[0]["verdict"] != "unknown" {
		t.Fatal(models, err)
	}
	if _, err := searchVLLMHub(context.Background(), "Qwen", "invalid", nil); err == nil {
		t.Fatal("invalid quant filter")
	}
}

func TestVLLMUpdateRefusedWhileRunning(t *testing.T) {
	old := vllm
	vllm = &vllmState{cmd: &exec.Cmd{}, log: []string{"retain"}, model: "org/model"}
	t.Cleanup(func() { vllm = old })
	for _, action := range []string{"install", "update"} {
		w := httptest.NewRecorder()
		handleVLLM(w, vllmJSONPost(strings.NewReader(fmt.Sprintf(`{"action":%q}`, action))))
		if w.Code != 409 || vllm.job != "" || !reflect.DeepEqual(vllm.log, []string{"retain"}) {
			t.Fatal(w.Code, vllm)
		}
	}
	if err := vllm.reserve("update", ""); err == nil {
		t.Fatal("update race accepted")
	}
}

func TestVLLMAutoUpdateIdleOnly(t *testing.T) {
	testHome(t)
	writeLlamaServer(t, vllmBin())
	if err := putStoreJSON(bkState, vllmAutoState, engineAuto{Auto: true}); err != nil {
		t.Fatal(err)
	}
	old := vllm
	vllm = &vllmState{cmd: &exec.Cmd{}}
	t.Cleanup(func() { vllm = old })
	called := false
	vllmAutoTick(time.Now(), func(context.Context) (string, error) { called = true; return "new", nil })
	if called || vllm.job != "" || loadVLLMAuto().CheckedAt != 0 {
		t.Fatal("auto update touched serving environment")
	}
	if err := putStoreJSON(bkState, vllmAutoState, engineAuto{}); err != nil {
		t.Fatal(err)
	}
	vllm.cmd = nil
	vllmAutoTick(time.Now(), func(context.Context) (string, error) { called = true; return "new", nil })
	if called {
		t.Fatal("auto update enabled by default")
	}
}

func TestVLLMDownloadResume(t *testing.T) {
	for _, ranges := range []bool{true, false} {
		t.Run(fmt.Sprint(ranges), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=3-" {
					t.Error("missing resume", r.Header)
				}
				if ranges {
					w.Header().Set("Content-Range", "bytes 3-5/6")
					w.WriteHeader(206)
					io.WriteString(w, "def")
				} else {
					io.WriteString(w, "abcdef")
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			vllmFixtureFile(t, dir, "model.safetensors.loom-incomplete", "abc")
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			var done int64
			if err := fetchVLLMFile(context.Background(), root, "model.safetensors", server.URL, 6, &done); err != nil {
				t.Fatal(err)
			}
			b, err := root.ReadFile("model.safetensors")
			if err != nil || string(b) != "abcdef" || done != 6 {
				t.Fatal(string(b), done, err)
			}
		})
	}
}

func TestVLLMDownloadCancelPreservesPartial(t *testing.T) {
	dir := t.TempDir()
	vllmFixtureFile(t, dir, "model.safetensors.loom-incomplete", "abc")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var done int64
	if err := fetchVLLMFile(ctx, root, "model.safetensors", "https://huggingface.co/unused", 6, &done); err != context.Canceled {
		t.Fatal(err)
	}
	b, err := root.ReadFile("model.safetensors.loom-incomplete")
	if err != nil || string(b) != "abc" {
		t.Fatal("partial deleted", err)
	}
}

func TestVLLMPrefetchHFLayout(t *testing.T) {
	sha := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models/org/model" {
			json.NewEncoder(w).Encode(map[string]any{"id": "org/model", "sha": sha, "siblings": []map[string]any{{"rfilename": "config.json", "size": 2}, {"rfilename": "model.safetensors", "size": 6}, {"rfilename": "tokenizer.json", "size": 2}, {"rfilename": "pytorch_model.bin", "size": 100}, {"rfilename": "../escape.json"}}})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/org/model/resolve/"+sha+"/") {
			t.Error("unpinned revision", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if strings.HasSuffix(r.URL.Path, "model.safetensors") {
			io.WriteString(w, "abcdef")
		} else {
			io.WriteString(w, "{}")
		}
	}))
	defer server.Close()
	oldAPI, oldWeb := hubAPIBase, hubWebBase
	hubAPIBase, hubWebBase = server.URL+"/api/models", server.URL
	t.Cleanup(func() { hubAPIBase, hubWebBase = oldAPI, oldWeb })
	t.Setenv("HF_HUB_CACHE", t.TempDir())
	st := &vllmDownload{Model: "org/model"}
	if err := prefetchVLLM(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	cache := vllmHFCache()
	b, err := os.ReadFile(filepath.Join(cache, "models--org--model/refs/main"))
	if err != nil || string(b) != sha {
		t.Fatal(string(b), err)
	}
	models, err := listVLLMCache(cache)
	if err != nil || len(models) != 1 || !models[0].Servable || st.counter != 10 {
		t.Fatal(models, st, err)
	}
	if _, err := os.Stat(filepath.Join(cache, "escape.json")); !os.IsNotExist(err) {
		t.Fatal("escaped cache")
	}
}

func TestVLLMSearchSkipsForeignFormats(t *testing.T) {
	for _, m := range []vllmHFModel{{ID: "mlx-community/Qwen3-0.6B-4bit"}, {ID: "org/model", Tags: []string{"MLX"}}, {ID: "unsloth/Qwen3-0.6B-unsloth-bnb-4bit"}} {
		if !vllmForeignFormat(m) {
			t.Fatal(m.ID)
		}
	}
	if vllmForeignFormat(vllmHFModel{ID: "Qwen/Qwen3-0.6B", Tags: []string{"safetensors"}}) {
		t.Fatal("native repository rejected")
	}
}
