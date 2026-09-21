package ajean

import (
	"encoding/json"
	"testing"
)

func TestAdaptOAICompletionLiftsOptions(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	setConfig(t, "BIN=/usr/bin/llama-server\nCTX=8192\nNP=1\nTEMP=0.8\n")
	in := []byte(`{
		"model":"ajean",
		"messages":[{"role":"user","content":"hi"}],
		"options":{"temperature":0.2,"top_p":0.8,"num_predict":128,"n_parallel":4}
	}`)
	out, split, err := adaptOAICompletion(in)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["temperature"] != 0.2 || m["top_p"] != 0.8 {
		t.Fatalf("sampling non remonté : %v", m)
	}
	if m["max_tokens"] != 128.0 {
		t.Fatalf("num_predict → max_tokens : %v", m["max_tokens"])
	}
	if _, ok := m["n_parallel"]; ok {
		t.Fatal("n_parallel ne doit pas rester dans le corps llama")
	}
	if split.Run["NP"] != "4" {
		t.Fatalf("NP overlay : %v", split.Run)
	}
	cfg := ReadConfig()
	if cfg["NP"] != "1" || cfg["TEMP"] != "0.8" || cfg["CTX"] != "8192" {
		t.Fatalf("adapt a écrit la config : %v", cfg)
	}
}

func TestAdaptOAICompletionKeepsClientValues(t *testing.T) {
	in := []byte(`{"temperature":0.1,"options":{"temperature":0.9,"max_tokens":16}}`)
	out, _, err := adaptOAICompletion(in)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["temperature"] != 0.1 {
		t.Fatalf("le top-level de l'app doit gagner : %v", m["temperature"])
	}
	if m["max_tokens"] != 16.0 {
		t.Fatalf("max_tokens depuis options : %v", m["max_tokens"])
	}
}

func TestAdaptOAICompletionAliases(t *testing.T) {
	in := []byte(`{"max_completion_tokens":64,"topK":20,"reasoning_effort":"low","parallel_tool_calls":true}`)
	out, split, err := adaptOAICompletion(in)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["max_tokens"] != 64.0 || m["top_k"] != 20.0 {
		t.Fatalf("alias : %v", m)
	}
	if m["parallel_tool_calls"] != true {
		t.Fatal("parallel_tool_calls n'est pas le nombre de slots")
	}
	if split.Run["NP"] != "" {
		t.Fatalf("parallel_tool_calls a fuité vers NP : %v", split.Run)
	}
	kw, _ := m["chat_template_kwargs"].(map[string]any)
	if kw["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort : %v", m["chat_template_kwargs"])
	}
}

func TestAdaptOAICompletionDoesNotTouchConfig(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	setConfig(t, "CTX=8192\nNGL=99\nTEMP=0.8\nNP=1\n")
	_, split, err := adaptOAICompletion([]byte(`{"options":{"temperature":0.1,"num_ctx":32768,"num_gpu":10,"n_parallel":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if split.Run["CTX"] != "32768" || split.Run["NGL"] != "10" || split.Run["NP"] != "3" {
		t.Fatalf("overlay : %v", split.Run)
	}
	cfg := ReadConfig()
	if cfg["CTX"] != "8192" || cfg["NGL"] != "99" || cfg["TEMP"] != "0.8" || cfg["NP"] != "1" {
		t.Fatalf("une requête /v1 a modifié la config : %v", cfg)
	}
}

func TestOAIRuntimeGetDoesNotWriteConfig(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	setConfig(t, "NP=1\n")
	clearOAIRuntime()
	t.Cleanup(clearOAIRuntime)
	oaiRunMu.Lock()
	oaiRunKV["NP"] = "4"
	oaiRunMu.Unlock()
	if oaiRuntimeGet("NP") != "4" {
		t.Fatal("overlay NP illisible")
	}
	if ReadConfig()["NP"] != "1" {
		t.Fatalf("overlay a écrit NP dans la config : %q", ReadConfig()["NP"])
	}
}
