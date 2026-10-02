package loom

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteProbeScriptRunsAndParses(t *testing.T) {
	out, err := exec.Command("sh", "-c", remoteProbeScript).Output()
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseRemoteProbe("bruit\n" + string(out))
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if info.User == "" || !strings.HasPrefix(info.Home, "/") {
		t.Fatalf("description incomplète: %+v", info)
	}
}

func TestRemoteAgentCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := RemoteMachine{ID: "box", Host: "10.0.0.2", User: "root", Port: 2222, Home: "/root",
		Tools: []RemoteTool{{ID: "hermes", Path: "/usr/local/bin/hermes"}, {ID: "npx", Path: "/root/.nvm/versions/node/v24/bin/npx"}}}
	a, err := remoteAgent(m, "hermes", "/k")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(a.Args, " ")
	want := "-T -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -i /k -p 2222 root@10.0.0.2 sh -c 'PATH=/usr/local/bin:/root/.nvm/versions/node/v24/bin:\"$PATH\" exec hermes acp'"
	if a.ID != "custom-box-hermes" || !a.Remote || a.RemoteHome != "/root" || got != want {
		t.Fatalf("%s\n%s", a.ID, got)
	}
	cc, err := remoteAgent(m, "claude-code", "")
	if err != nil || !strings.Contains(strings.Join(cc.Args, " "), "exec npx -y @agentclientprotocol/claude-agent-acp@") {
		t.Fatalf("%v %v", err, cc.Args)
	}
}

func TestRemoteMachineValidation(t *testing.T) {
	for _, bad := range []RemoteMachine{{Host: "-oProxyCommand=x", User: "a"}, {Host: "h", User: "a b"}, {Host: "h", User: "a", Port: 70000}} {
		if _, err := validRemoteMachine(bad); err == nil {
			t.Fatalf("accepté: %+v", bad)
		}
	}
	m, err := validRemoteMachine(RemoteMachine{Name: "Hermes Agent", Host: "192.168.1.50", User: "root"})
	if err != nil || m.ID != "hermes-agent" || m.Port != 22 {
		t.Fatalf("%v %+v", err, m)
	}
}

func TestOffersNeedNpxForAdapters(t *testing.T) {
	offers := remoteOffers(RemoteMachine{Tools: []RemoteTool{{ID: "claude", Path: "/x/claude"}, {ID: "hermes", Path: "/x/hermes"}}})
	for _, o := range offers {
		switch o["id"] {
		case "hermes":
			if o["ready"] != true {
				t.Fatal("hermes devrait être prêt")
			}
		case "claude-code":
			if o["ready"] != false || o["installed"] != true || o["missing"] == "" {
				t.Fatalf("claude sans npx: %+v", o)
			}
		}
	}
}

func TestLegacyModelAPIBecomesModelOption(t *testing.T) {
	var r acpSessionResponse
	if err := jsonUnmarshalString(`{"sessionId":"s","models":{"currentModelId":"a:x","availableModels":[{"modelId":"a:x","name":"X"},{"modelId":"b:y","name":"Y"}]}}`, &r); err != nil {
		t.Fatal(err)
	}
	o := acpModelOption(r.options())
	if o == nil || o["currentValue"] != "a:x" || o[acpLegacyModelKey] != true || !acpConfigValueAllowed(o, "b:y") {
		t.Fatalf("%+v", o)
	}
	p := &acpBinding{}
	p.applySessionResponse(r)
	p.applyConfig([]map[string]any{{"id": "effort", "category": "thought_level"}})
	if acpModelOption(p.state.AvailableConfigOptions) == nil {
		t.Fatal("option modèle perdue après une mise à jour")
	}
}

func jsonUnmarshalString(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func TestRemoteMachineWithoutHarnessesPersistsAndRemainsEditable(t *testing.T) {
	home := testHome(t)
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "fixture-bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	// No real SSH connection or harness probe: the target returns installed offers,
	// but the operator explicitly selects none.
	probe := `#!/bin/sh
cat >/dev/null
printf '%s\n' 'LOOM-MACHINE {"hostname":"GPU","home":"/home/fixture","os":"Linux","tools":[{"id":"codex","path":"/fixture/codex"}]}'
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(probe), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := os.MkdirAll(filepath.Join(home, "ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"loom_ed25519", "loom_ed25519.pub"} {
		if err := os.WriteFile(filepath.Join(home, "ssh", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`{"machine":{"id":"gpu","name":"GPU","host":"fixture","user":"fixture"},"harnesses":[]}`,
		`{"machine":{"id":"gpu","name":"GPU renamed","host":"fixture","user":"fixture"},"harnesses":[]}`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/machines", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handleRemoteMachines(rec, req)
		if rec.Code != 200 {
			t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
		}
		machines := loadRemoteMachines()
		if len(machines) != 1 || machines[0].ID != "gpu" || len(machines[0].Harnesses) != 0 || machines[0].Home != "/home/fixture" {
			t.Fatal("machine missing or harnesses added")
		}
		if len(loadCustomACPAgents()) != 0 {
			t.Fatal("registered an unselected harness")
		}
	}
}
