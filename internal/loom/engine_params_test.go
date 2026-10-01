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
