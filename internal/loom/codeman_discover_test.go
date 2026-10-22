package loom

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdEnvironmentParsing(t *testing.T) {
	env := map[string]string{}
	systemdEnvironment(`"CODEMAN_PASSWORD=a b\"c\\d" CODEMAN_PORT=3100 PATH=/usr/bin`, env)
	if env["CODEMAN_PASSWORD"] != `a b"c\d` || env["CODEMAN_PORT"] != "3100" || env["PATH"] != "/usr/bin" {
		t.Fatal(env)
	}
	got := parseEnvAssignments("# comment\nexport CODEMAN_USERNAME='me'\nCODEMAN_PASSWORD=\"x y\"\nnot an assignment\n")
	if got["CODEMAN_USERNAME"] != "me" || got["CODEMAN_PASSWORD"] != "x y" || len(got) != 2 {
		t.Fatal(got)
	}
}

func discoverHome(t *testing.T, upstream string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"CODEMAN_DATA_DIR", "CODEMAN_PASSWORD", "CODEMAN_USERNAME", "CODEMAN_PORT"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	u, _ := url.Parse(upstream)
	unit := filepath.Join(home, ".config", "systemd", "user", "codeman-web.service")
	if err := os.MkdirAll(unit+".d", 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/bin/node /opt/codeman web --host 0.0.0.0 --port "+u.Port()+"\n"), 0600)
	os.WriteFile(filepath.Join(unit+".d", "override.conf"), []byte("[Service]\nEnvironment=CODEMAN_PASSWORD=synthetic-codeman\n"), 0600)
	return home
}

func TestCodemanDiscoverAndOneClickConnect(t *testing.T) {
	_, loom := codemanTestServers(t)
	upstream := loadCodemanConfig().URL
	os.Remove(codemanConfigPath())
	discoverHome(t, upstream)
	if _, out := codemanDo(t, "GET", loom.URL+"/api/codeman/config", "", nil); out["configured"] != false {
		t.Fatal("fresh install must report unconfigured", out)
	}
	resp, out := codemanDo(t, "GET", loom.URL+"/api/codeman/discover", "", nil)
	raw, _ := json.Marshal(out)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "synthetic-codeman") {
		t.Fatal("discovery leaked the password or failed", string(raw))
	}
	var f map[string]any
	for _, x := range out["found"].([]any) {
		if m := x.(map[string]any); m["url"] == upstream {
			f = m
		}
	}
	if f == nil {
		t.Fatal(string(raw))
	}
	if f["url"] != upstream || f["auth"] != "ok" || f["local_credentials"] != true || f["source"] != "systemd" || f["version"] != "1.43.0" {
		t.Fatal(f)
	}
	// Un mauvais mot de passe saisi n'enregistre rien.
	if _, out := codemanDo(t, "POST", loom.URL+"/api/codeman/connect", `{"url":"`+upstream+`","password":"wrong"}`, nil); out["ok"] != false || out["code"] != "rejected" {
		t.Fatal(out)
	}
	if _, err := os.Stat(codemanConfigPath()); err == nil {
		t.Fatal("failed connection saved a configuration")
	}
	if _, out := codemanDo(t, "POST", loom.URL+"/api/codeman/connect", `{"url":"`+upstream+`"}`, nil); out["ok"] != false || out["code"] != "password" {
		t.Fatal(out)
	}
	if _, out := codemanDo(t, "POST", loom.URL+"/api/codeman/connect", `{"url":"`+upstream+`","use_local":true}`, nil); out["ok"] != true {
		t.Fatal(out)
	}
	if _, out := codemanDo(t, "GET", loom.URL+"/api/codeman/status", "", nil); out["auth"] != "ok" {
		t.Fatal(out)
	}
	if _, out := codemanDo(t, "POST", loom.URL+"/api/codeman/disconnect", `{}`, nil); out["ok"] != true || codemanPassword() != "" {
		t.Fatal("disconnect kept credentials", out)
	}
}

func TestCodemanDiscoverEnvFileAndOpenServer(t *testing.T) {
	testHome(t)
	open := httptest.NewServer(openCodemanHandler())
	defer open.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEMAN_DATA_DIR", filepath.Join(home, "data"))
	u, _ := url.Parse(open.URL)
	t.Setenv("CODEMAN_PORT", u.Port())
	os.MkdirAll(filepath.Join(home, "data"), 0700)
	os.WriteFile(filepath.Join(home, "data", ".env"), []byte("CODEMAN_USERNAME=me\n"), 0600)
	installs := localCodemanInstalls()
	if len(installs) == 0 || installs[0].URL != "http://127.0.0.1:"+u.Port() || installs[0].Username != "me" {
		t.Fatal(installs)
	}
	p := probeCodeman(t.Context(), installs[0].URL, "", "")
	if !p.Found || p.Auth != "open" {
		t.Fatal(p)
	}
}
