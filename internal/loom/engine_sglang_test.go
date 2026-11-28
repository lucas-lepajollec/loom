package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

func TestSGLangNativeParamsAndArgs(t *testing.T) {
	specs := sglangParams(2)
	if len(specs) != 12 {
		t.Fatalf("got %d controls", len(specs))
	}
	for _, p := range specs {
		if !p.Available || !p.Supported || !p.RequiresReload || p.Flag != "--"+p.ID {
			t.Fatalf("parameter metadata: %+v", p)
		}
		if p.ID == "trust-remote-code" && (!p.Dangerous || p.Default != "off" || p.Tier != "expert") {
			t.Fatalf("trust metadata: %+v", p)
		}
	}
	values := map[string]string{"context-length": "4096", "tp-size": "2", "dtype": "bfloat16", "quantization": "auto", "kv-cache-dtype": "fp8_e5m2",
		"chunked-prefill-size": "-1", "disable-radix-cache": "true", "max-running-requests": "4", "reasoning-parser": "qwen3", "tool-call-parser": "qwen25", "trust-remote-code": "off"}
	args, err := buildSGLangArgs("Qwen/Qwen3-8B", 30000, values, 2)
	want := []string{"-m", "sglang.launch_server", "--model-path", "Qwen/Qwen3-8B", "--host", "127.0.0.1", "--port", "30000",
		"--context-length", "4096", "--mem-fraction-static", "0.85", "--tp-size", "2", "--dtype", "bfloat16", "--kv-cache-dtype", "fp8_e5m2", "--chunked-prefill-size", "-1",
		"--disable-radix-cache", "--max-running-requests", "4", "--reasoning-parser", "qwen3", "--tool-call-parser", "qwen25"}
	if err != nil || !slices.Equal(args, want) {
		t.Fatalf("args = %v, %v", args, err)
	}
	for _, bad := range []map[string]string{
		{"tp-size": "2"}, {"context-length": "1.5"}, {"mem-fraction-static": "NaN"}, {"mem-fraction-static": "1.1"},
		{"kv-cache-dtype": "bf16"}, {"chunked-prefill-size": "0"}, {"max-running-requests": "0"}, {"trust-remote-code": "yes"},
		{"tool-call-parser": "--host"}, {"extra-args": "--host 0.0.0.0"},
	} {
		if _, err := buildSGLangArgs("Qwen/Qwen3-8B", 30000, bad, 1); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestSGLangParamsPersistPerModel(t *testing.T) {
	testHome(t)
	rec := httptest.NewRecorder()
	handleSGLangParams(rec, vllmJSONPost(strings.NewReader(`{"model":"Qwen/Qwen3-8B","values":{"context-length":"8192","disable-radix-cache":"false"}}`)))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	for _, tc := range []struct{ model, value string }{{"Qwen%2FQwen3-8B", "8192"}, {"Qwen%2FQwen3-4B", ""}} {
		rec := httptest.NewRecorder()
		handleSGLangParams(rec, httptest.NewRequest("GET", "/api/engines/sglang/params?model="+tc.model, nil))
		var response struct {
			Values   map[string]string
			EngineID string `json:"engine_id"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.EngineID != "sglang" || response.Values["context-length"] != tc.value {
			t.Fatalf("saved params = %d %s", rec.Code, rec.Body)
		}
	}
	if values, err := loadVLLMParams("Qwen/Qwen3-8B"); err != nil || len(values) != 0 {
		t.Fatalf("SGLang settings leaked into vLLM: %v %v", values, err)
	}
}

func TestSGLangManagedChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper requires Unix")
	}
	testHome(t)
	engine := &pythonEngine{id: "sglang", label: "SGLang"}
	t.Cleanup(engine.stop)
	t.Setenv("LOOM_TEST_SGLANG_CHILD", os.Args[0])
	if err := os.MkdirAll(filepath.Dir(engine.python()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.python(), []byte("#!/bin/sh\nexec \"$LOOM_TEST_SGLANG_CHILD\" -test.run=^TestSGLangHelperProcess$ -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(engine.dir(), "lib", "python3.12", "site-packages", "sglang-0.5.dist-info")
	if err := os.MkdirAll(meta, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "METADATA"), []byte("Version: 0.5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if engine.version() != "0.5" {
		t.Fatal("installed environment not detected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := engine.start(ctx, nil, "Qwen/Qwen3-8B", map[string]string{"context-length": "4096"}, 1); err != nil {
		t.Fatal(err)
	}
	node := currentEngineNode()
	if node == nil || node.Kind != "sglang" || node.Managed != "sglang" || node.Model != "Qwen/Qwen3-8B" || node.Ctx != 4096 {
		t.Fatalf("owned engine = %+v", node)
	}
	if saved, _, err := engineConnectionSnapshot(); err != nil || len(saved) != 0 {
		t.Fatalf("transient link persisted: %v %v", saved, err)
	}
	if _, err := engine.reserve("update", ""); err == nil {
		t.Fatal("update admitted while serving")
	}
	engine.stop()
	if currentEngineNode() != nil {
		t.Fatal("stopped engine remained selected")
	}

	startCtx, err := engine.reserve("start", "Qwen/Qwen3-8B")
	if err != nil {
		t.Fatal(err)
	}
	if !engine.cancelStart() {
		t.Fatal("loading engine could not cancel")
	}
	err = engine.start(startCtx, nil, "Qwen/Qwen3-8B", nil, 1)
	engine.complete(err)
	if err != context.Canceled || currentEngineNode() != nil {
		t.Fatalf("canceled startup = %v", err)
	}
}

func TestSGLangHelperProcess(t *testing.T) {
	if os.Getenv("LOOM_TEST_SGLANG_CHILD") == "" {
		return
	}
	var port string
	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	if port == "" {
		t.Fatal("missing port")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":[{"id":"Qwen/Qwen3-8B","owned_by":"sglang"}]}`)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	if err := http.Serve(ln, mux); err != nil {
		t.Fatal(fmt.Errorf("serve fixture: %w", err))
	}
}

func TestSGLangInstallPolicyAndSharedCacheProtection(t *testing.T) {
	testHome(t)
	if runtime.GOOS == "linux" {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		for name, script := range map[string]string{
			"nvidia-smi": "#!/bin/sh\nprintf 'Fixture GPU, 0, 12288, 0, 35\\n'\n",
			"uv":         "#!/bin/sh\nexit 99\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
		}
		savePolicyRules(t, policy.Rule{ID: "deny-sglang", Scope: "machine", ScopeID: "local", Subject: "node.install", Decision: policy.Deny})
		rec := httptest.NewRecorder()
		handleSGLang(rec, vllmJSONPost(strings.NewReader(`{"action":"install"}`)))
		if rec.Code != 403 {
			t.Fatalf("install policy = %d %s", rec.Code, rec.Body)
		}
		if _, err := os.Stat(sglang.dir()); !os.IsNotExist(err) {
			t.Fatal("denied install touched the environment")
		}
	}

	cache := t.TempDir()
	t.Setenv("HF_HUB_CACHE", cache)
	model := "Qwen/Qwen3-8B"
	dir := filepath.Join(cache, vllmCacheName(model))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, err := sglang.reserve("start", model)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	err = deleteVLLMCache(model)
	sglang.complete(nil)
	if err == nil || !strings.Contains(err.Error(), "used by SGLang") {
		t.Fatalf("shared cache deletion while loading: %v", err)
	}
	if err := deleteVLLMCache(model); err != nil {
		t.Fatal(err)
	}
}
