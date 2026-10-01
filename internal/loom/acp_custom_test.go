package loom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomACPAgentsRegisterLiveAndStayOutOfBuiltins(t *testing.T) {
	testHome(t)
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if path == "/api/harness/custom" {
			handleCustomACP(w, r)
		} else {
			handleCustomACPDelete(w, r)
		}
		return w
	}
	w := post("/api/harness/custom", `{"name":"Hermes LXC","command":"ssh","args":["-T","hermes","hermes","acp"],"remote":true}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out struct{ Agent acpAgent }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Agent.ID != "custom-hermes-lxc" || !out.Agent.Remote || out.Agent.Logo != "hermes" {
		t.Fatalf("%+v", out.Agent)
	}
	t.Cleanup(func() { registeredRuntimes.remove("custom-hermes-lxc") })
	a, ok := registeredRuntimes.lookup("custom-hermes-lxc")
	if !ok || !a.Descriptor().Custom || hasRuntimeCapability(a.Descriptor(), "mcp") {
		t.Fatal("custom agent not registered honestly")
	}
	if w := post("/api/harness/custom", `{"id":"codex","name":"x","command":"x"}`); w.Code == 200 {
		t.Fatal("built-in id overwritten")
	}
	if w := post("/api/harness/custom", `{"name":"Codex","command":"evil"}`); w.Code == 200 {
		// "Codex" slugs to custom-codex: allowed, but never replaces the built-in.
		registeredRuntimes.remove("custom-codex")
	}
	if b, _ := registeredRuntimes.lookup("codex"); b.(*acpAdapter).agent.Custom {
		t.Fatal("built-in replaced")
	}
	if w := post("/api/harness/custom/delete", `{"id":"custom-hermes-lxc"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, ok := registeredRuntimes.lookup("custom-hermes-lxc"); ok {
		t.Fatal("not removed")
	}
}

func TestRemoteWorkdirAndRemoteDiffScope(t *testing.T) {
	if _, err := remoteWorkdir("relative/dir"); err == nil {
		t.Fatal("relative accepted")
	}
	if p, err := remoteWorkdir("/srv/app/../app/"); err != nil || p != "/srv/app" {
		t.Fatalf("%q %v", p, err)
	}
	p := &acpBinding{remoteRoot: "/srv/app"}
	if !p.insideRootsLocked("/srv/app/main.go") || p.insideRootsLocked("/srv/application/x") || p.insideRootsLocked("/etc/passwd") {
		t.Fatal("remote scope")
	}
}
