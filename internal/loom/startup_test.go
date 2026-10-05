package loom

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartupOnlyControlsInstalledLoomUnitsWithoutStartingThem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux systemd contract")
	}
	testHome(t)
	t.Setenv("LOOM_UI_SERVICE", "loom-fixture-ui")
	t.Setenv("LOOM_SERVICE", "loom-fixture-engine")
	old := startupCommand
	defer func() { startupCommand = old }()
	enabled := false
	writes := []string{}
	startupCommand = func(_ context.Context, user, write bool, args ...string) ([]byte, error) {
		if write {
			writes = append(writes, strings.Join(args, " "))
			enabled = args[0] == "enable"
			return nil, nil
		}
		switch args[0] {
		case "show":
			return []byte("loaded\n"), nil
		case "is-enabled":
			if enabled {
				return []byte("enabled\n"), nil
			}
			return []byte("disabled\n"), nil
		case "is-active":
			return []byte("active\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/startup", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handleStartup(w, r)
		return w
	}
	if w := send(`{"service":"ui","enabled":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("startup readback failed", w.Code)
	}
	if len(writes) != 1 || writes[0] != "enable loom-fixture-ui" {
		t.Fatalf("unexpected commands: %v", writes)
	}
	if w := send(`{"service":"sshd","enabled":false}`); w.Code == 200 {
		t.Fatal("arbitrary service accepted")
	}
	if w := send(`{"policy":{"engine":"unknown"}}`); w.Code == 200 {
		t.Fatal("invalid engine accepted")
	}
	if len(writes) != 1 {
		t.Fatal("invalid request mutated services")
	}
}
func TestRemoteStartupScriptUsesAllowlistAndReportsUserLinger(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	if _, err := remoteStartupScript("ui; touch /tmp/unsafe", true); err == nil {
		t.Fatal("shell injection accepted")
	}
	dir := t.TempDir()
	for name, script := range map[string]string{"uname": "echo Linux", "id": "echo 1000", "systemctl": `[ "$1" != --user ] || shift
case "$1" in show) echo loaded;; is-enabled) echo enabled;; is-active) echo active;; enable|disable) exit 0;; *) exit 1;; esac`, "loginctl": "echo yes"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := remoteStartupScript("node", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "--now") || strings.Contains(script, "systemctl start") {
		t.Fatal("startup setting starts/stops running services")
	}
	cmd := exec.Command("sh", "-s")
	cmd.Env = append(os.Environ(), "PATH="+dir)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.Output()
	if err != nil || !bytes.Contains(out, []byte(`"linger":true`)) || !bytes.Contains(out, []byte(`"unit":"loom-node","user":true`)) {
		t.Fatalf("remote startup readback failed: %v %s", err, out)
	}
}
