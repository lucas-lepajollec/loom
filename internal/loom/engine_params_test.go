package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCuratedParamsSchemaAndInstalledHelp(t *testing.T) {
	c := readCuratedParams()
	seen := map[string]bool{}
	for _, p := range c.Params {
		if seen[p.ID] || p.ID == "" || p.Label == "" || p.Tip == "" || (p.Tier != "essential" && p.Tier != "advanced") {
			t.Fatalf("invalid curated control: %+v", p)
		}
		seen[p.ID] = true
		for _, choice := range p.Choices {
			if len(choice) != 2 {
				t.Fatalf("invalid choices for %s", p.ID)
			}
		}
		if p.RequiresFlag && p.Flag == "" {
			t.Fatalf("%s requires an unnamed flag", p.ID)
		}
	}
	flags := parseLlamaHelp(fakeRouterHelp + "\n--fit [on|off]    fit memory\n--seed N    seed\n--obsolete N    deprecated\n")
	got := mergeEngineParams(c, flags)
	find := func(id string) ParamSpec {
		for _, p := range got {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("missing %s", id)
		return ParamSpec{}
	}
	if p := find("gpu-layers"); p.Flag != "--gpu-layers" || p.Key != "NGL" || !p.Supported {
		t.Fatalf("aliases not merged: %+v", p)
	}
	if p := find("fit"); p.Kind != "bool" || p.NativeKind != "enum" || !p.Available {
		t.Fatalf("fit switch lost native valued flag: %+v", p)
	}
	if find("batch-size").Available {
		t.Fatal("absent native flag became available")
	}
	if !find("sysprompt").Available || !find("kv").Available {
		t.Fatal("composite/model controls require installed flags")
	}
	if p := find("seed"); p.Tier != "expert" || p.Label != "--seed" || !p.Supported {
		t.Fatalf("missing help-driven expert: %+v", p)
	}
	for _, p := range got {
		if p.Tier == "expert" && (p.ID == "api-key" || p.ID == "model" || p.ID == "mmap" || p.ID == "obsolete") {
			t.Fatalf("hidden/deprecated/covered flag exposed: %s", p.ID)
		}
	}
}

func TestNewAdvancedParamNeedsOnlyCatalogData(t *testing.T) {
	c := readCuratedParams()
	// Simulates one JSON entry, not a backend switch or UI allowlist edit.
	c.Params = append(c.Params, ParamSpec{ID: "seed", Flag: "--seed", Label: "Graine", Tier: "advanced", Kind: "number", RequiresFlag: true})
	got := mergeEngineParams(c, parseLlamaHelp("--seed N    random seed (default: -1)\n"))
	count := 0
	for _, p := range got {
		if p.ID == "seed" {
			count++
			if !p.Available || p.Tier != "advanced" || p.Default != "-1" {
				t.Fatalf("new control not resolved: %+v", p)
			}
		}
	}
	if count != 1 {
		t.Fatal("new curated flag also appears in Expert")
	}
}

func TestEngineParamsHTTPReadOnlyAuthAndMissingBinary(t *testing.T) {
	testHome(t)
	bin := fakeLlamaBin(t, LoomHome())
	if err := SetConfigKey("BIN", bin); err != nil {
		t.Fatal(err)
	}
	before := ReadConfig()
	mux := newWebMux()
	call := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/api/engine/params", nil))
		return w
	}
	w := call("GET")
	var response struct {
		OK     bool        `json:"ok"`
		ID     string      `json:"engine_id"`
		Params []ParamSpec `json:"params"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || !response.OK || response.ID != "llama.cpp" || len(response.Params) == 0 {
		t.Fatal("bad ParamSpec endpoint")
	}
	if w.Header().Get("Cache-Control") != "no-store" || !reflect.DeepEqual(before, ReadConfig()) {
		t.Fatal("catalog cached or configuration changed")
	}
	if w := call("POST"); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("mutation method accepted")
	}
	if err := SetConfigKey("BIN", ""); err != nil {
		t.Fatal(err)
	}
	for _, p := range localEngine().Params() {
		if p.Supported || p.Tier == "expert" {
			t.Fatal("old help reused after removing binary")
		}
	}
	if err := storeWebKey("synthetic-params-test"); err != nil {
		t.Fatal(err)
	}
	if call("GET").Code != 401 {
		t.Fatal("catalog bypassed web auth")
	}
}

func TestEngineWrapperRouterLoadAndTransientInput(t *testing.T) {
	home, fr := setupRouterHome(t)
	e := llamaCppEngine{}
	if e.ID() != "llama.cpp" || !e.Capabilities().Router || !e.Capabilities().Slots || !e.Capabilities().Estimate {
		t.Fatal("installed capabilities not discovered")
	}
	if err := e.Load(context.Background(), ModelConfig{}); err != nil {
		t.Fatal(err)
	}
	active := getStr(bkState, routerStateActive)
	before := ReadConfig()
	other := filepath.Join(home, "other.gguf")
	if err := os.WriteFile(other, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Load(context.Background(), ModelConfig{Model: other, Content: "CTX=4096\nNGL=12\n"}); err != nil {
		t.Fatal(err)
	}
	current := routerCurrentName()
	if current == active || getStr(bkState, routerStateActive) != active || !reflect.DeepEqual(before, ReadConfig()) {
		t.Fatal("transient load changed saved/active configuration")
	}
	if len(fr.loads) != 2 {
		t.Fatalf("expected router loads, got %d", len(fr.loads))
	}
	if err := e.Unload(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, ReadConfig()) {
		t.Fatal("targeted unload saved a configuration")
	}
	if err := e.Unload(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()["MODEL"] != "" || !routerReachable() {
		t.Fatal("unload did not preserve the router")
	}
	if !strings.HasSuffix(e.Endpoint(), "/v1") {
		t.Fatal("endpoint is not API-compatible")
	}
}

func TestEngineWrapperCanceledActionsAreInert(t *testing.T) {
	testHome(t)
	before := ReadConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := llamaCppEngine{}
	for _, err := range []error{e.Start(ctx), e.Load(ctx, ModelConfig{}), e.Unload(ctx, "")} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled action: %v", err)
		}
	}
	if !reflect.DeepEqual(before, ReadConfig()) {
		t.Fatal("canceled action changed configuration")
	}
}
