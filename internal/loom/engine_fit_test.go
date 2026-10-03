package loom

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEngineFitPreservesUnsetAndExplicitMemoryArgs(t *testing.T) {
	home := testHome(t)
	model := filepath.Join(home, "model.gguf")
	writeMiniGGUF(t, model, "llama", 131072)
	bin := filepath.Join(home, "fit-server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat <<'EOF'\n"+fakeRouterHelp+"--fit [on|off]    fit memory\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		cfg          map[string]string
		want, absent []string
	}{
		{"automatic", map[string]string{"FIT": "on"}, []string{"--fit on"}, []string{"-c ", "-ngl "}},
		{"explicit", map[string]string{"FIT": "on", "CTX": "8192", "NGL": "24"}, []string{"--fit on", "-c 8192", "-ngl 24"}, nil},
		{"native zero", map[string]string{"FIT": "on", "CTX": "0", "NGL": "0"}, []string{"-c 0", "-ngl 0"}, nil},
		{"extra off", map[string]string{"FIT": "on", "EXTRA_ARGS": "--fit off"}, []string{"-c 131072", "-ngl 999", "--fit off"}, nil},
		{"extra on", map[string]string{"FIT": "off", "EXTRA_ARGS": "--fit=on"}, []string{"--fit=on"}, []string{"-c ", "-ngl "}},
		{"legacy", map[string]string{}, []string{"-c 131072", "-ngl 999"}, []string{"--fit "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg["BIN"] = bin
			tc.cfg["MODEL"] = model
			argv, err := buildLlamaServerArgsForConfig(tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(argv, " ")
			for _, v := range tc.want {
				if !strings.Contains(got, v) {
					t.Errorf("missing %s", v)
				}
			}
			for _, v := range tc.absent {
				if strings.Contains(got, v) {
					t.Errorf("unwanted %s", v)
				}
			}
		})
	}
	old := fakeLlamaBin(t, home)
	argv, err := buildLlamaServerArgsForConfig(map[string]string{"BIN": old, "MODEL": model, "FIT": "on"})
	if err != nil || strings.Contains(strings.Join(argv, " "), "--fit ") {
		t.Fatal("unsupported fit passed to old engine")
	}
	if !strings.Contains(strings.Join(argv, " "), "-c 131072") {
		t.Fatal("old binary lost legacy defaults")
	}
}

func TestNakedFitRouterINIAndRememberedOverrides(t *testing.T) {
	home := testHome(t)
	bin := filepath.Join(home, "fit-server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat <<'EOF'\n"+fakeRouterHelp+"--fit [on|off]    fit memory\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(home, "models", "fit.gguf")
	writeMiniGGUF(t, model, "llama", 131072)
	setConfig(t, "BIN="+bin+"\nMODEL="+model+"\n")
	fr := startFakeRouter(t, home)
	if err := loadNakedModel(model); err != nil {
		t.Fatal(err)
	}
	if err := localEngine().Load(context.Background(), ModelConfig{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "router-models.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "fit = on") || strings.Contains(string(raw), "\nc =") || strings.Contains(string(raw), "\nngl =") {
		t.Fatal("automatic settings pinned in router INI")
	}
	before := ReadConfig()
	if err := rememberNakedFromContent(model, "CTX=4096\nNGL=20\nFIT=off\n"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, ReadConfig()) || len(fr.loads) != 1 {
		t.Fatal("remember changed live engine")
	}
	if err := loadNakedModel(model); err != nil {
		t.Fatal(err)
	}
	cfg := ReadConfig()
	if cfg["CTX"] != "4096" || cfg["NGL"] != "20" || cfg["FIT"] != "off" {
		t.Fatal("explicit remembered settings lost")
	}
}

func TestObservedContextDoesNotAutoloadOrGuess(t *testing.T) {
	home := testHome(t)
	fr := startFakeRouter(t, home)
	_ = putStr(bkState, routerStateCurrent, "selected")
	_ = putStr(bkState, routerStateActive, "selected")
	for _, st := range []string{"unloaded", "loading", "sleeping"} {
		fr.mu.Lock()
		fr.status["selected"] = st
		fr.mu.Unlock()
		if observedEngineCtx() != nil {
			t.Fatal("context guessed before load")
		}
	}
	fr.mu.Lock()
	reads := fr.propsReads
	fr.status["selected"] = "loaded"
	fr.props = `{"default_generation_settings":{"n_ctx":4096},"total_slots":4}`
	fr.mu.Unlock()
	if reads != 0 {
		t.Fatal("props requested for an unloaded model")
	}
	before := ReadConfig()
	got := observedEngineCtx()
	if got == nil || *got != 4096 {
		t.Fatal("actual slot allocation not exposed")
	}
	if !reflect.DeepEqual(before, ReadConfig()) {
		t.Fatal("observation changed config")
	}
	fr.mu.Lock()
	fr.props = `{"default_generation_settings":{"n_ctx":0}}`
	fr.mu.Unlock()
	if observedEngineCtx() != nil {
		t.Fatal("zero context guessed")
	}
	fr.mu.Lock()
	fr.props = "not-json"
	fr.mu.Unlock()
	if observedEngineCtx() != nil {
		t.Fatal("malformed props accepted")
	}
	fr.mu.Lock()
	fr.props = `{"default_generation_settings":{"n_ctx":4096}}`
	fr.onProps = func() { _ = putStr(bkState, routerStateCurrent, "different") }
	fr.mu.Unlock()
	if observedEngineCtx() != nil {
		t.Fatal("stale observation after selection switch")
	}
	if len(fr.loads) != 0 {
		t.Fatal("status inspection loaded a model")
	}
}

func TestStatusKeepsRequestedAndObservedContextSeparate(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_SERVICE", "loom-test-fit-inactive")
	setConfig(t, "MODEL=absent.gguf\nCTX=4096\nFIT=on\n")
	w := httptest.NewRecorder()
	newWebMux().ServeHTTP(w, localTestRequest("GET", "/api/status", nil))
	var body map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal("bad status response")
	}
	if string(body["ctx_effective"]) != "null" || string(body["ctx"]) != "4096" {
		t.Fatal("requested context exposed as allocation")
	}
}
