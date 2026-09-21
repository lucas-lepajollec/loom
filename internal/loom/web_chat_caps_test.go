package loom

import "testing"

func ptrBool(b bool) *bool { return &b }

func TestCapsFromBodyJamaisAgent(t *testing.T) {
	testHome(t)
	_ = setAgentEnabled(true)
	_ = setInternetEnabled(true)
	for _, body := range []chatReq{
		{},
		{Agent: ptrBool(true)},
		{Tools: ptrBool(true)},
		{Skills: ptrBool(true)},
	} {
		if caps := capsFromBody(body); caps.Agent {
			t.Errorf("%+v a rallumé l'agent : %+v", body, caps)
		}
	}
}

func TestCapsInternetOptInSansAgent(t *testing.T) {
	testHome(t)
	_ = setInternetEnabled(true)
	if caps := capsFromBody(chatReq{}); caps.Internet {
		t.Fatal("sans champ internet, le tour ne doit pas avoir le web")
	}
	if caps := capsFromBody(chatReq{Internet: ptrBool(true)}); !caps.Internet {
		t.Fatal("internet:true doit allumer les outils web sans agent")
	}
	if caps := capsFromBody(chatReq{Internet: ptrBool(false)}); caps.Internet {
		t.Fatal("internet:false doit rester off")
	}
	names := map[string]bool{}
	for _, tool := range EnabledTools(capsFromBody(chatReq{Internet: ptrBool(true)})) {
		names[tool.Function.Name] = true
	}
	if !names["web_search"] {
		t.Fatal("internet:true doit exposer web_search")
	}
	if names["bash"] || names["write"] || names["task_create"] {
		t.Fatalf("outils agent exposés : %v", names)
	}
}

func TestCapsMCPOptIn(t *testing.T) {
	testHome(t)
	if caps := capsFromBody(chatReq{}); caps.MCP {
		t.Fatal("MCP off par défaut")
	}
	if caps := capsFromBody(chatReq{MCP: ptrBool(true)}); !caps.MCP {
		t.Fatal("mcp:true doit allumer MCP")
	}
}
