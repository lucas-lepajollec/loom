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

func jsonUnmarshalString(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func TestRemoteMachinePersistsAndRemainsEditable(t *testing.T) {
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
printf '%s\n' 'LOOM-MACHINE {"hostname":"GPU","home":"/home/fixture","os":"Linux","tools":[{"id":"codex","path":"/fixture/codex"},{"id":"npx","path":"/fixture/npx"}]}'
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
		`{"machine":{"id":"gpu","name":"GPU","host":"fixture","user":"fixture"}}`,
		`{"machine":{"id":"gpu","name":"GPU renamed","host":"fixture","user":"fixture"}}`,
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
	}
}

func TestRemoteOutputIgnoresShellBanner(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo 'System: Fedora 44 Kernel 7.2.5-200'; echo; "+remoteOutputStart+"echo 2.1.292")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := afterRemoteMarker(string(out)); got != "2.1.292\n" {
		t.Fatalf("%q", got)
	}
	if got := afterRemoteMarker("0.160.1\n"); got != "0.160.1\n" {
		t.Fatalf("unmarked output changed: %q", got)
	}
}
