package loom

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNodeReleaseInstallerLayoutUpdateAndServiceRestart(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_UI_SERVICE", "loom-node")
	dir := filepath.Join(t.TempDir(), ".local", "lib", "loom-node")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "loom")
	if err := os.WriteFile(exe, []byte("previous installed node"), 0755); err != nil {
		t.Fatal(err)
	}
	previousExe := updateExecutable
	updateExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { updateExecutable = previousExe })
	loomUpdateMu.Lock()
	previousInstalled := loomInstalledUpdate
	loomInstalledUpdate = ""
	loomUpdateMu.Unlock()
	t.Cleanup(func() { loomUpdateMu.Lock(); loomInstalledUpdate = previousInstalled; loomUpdateMu.Unlock() })
	// The main is on edge; the saved node's own preference stays stable.
	if err := putStoreJSON(bkState, updateChannelKey, "edge"); err != nil {
		t.Fatal(err)
	}
	m := RemoteMachine{ID: "fixture-update", Host: "192.168.1.20"}
	n := &engineNode{URL: "http://192.168.1.20:2511", WebKey: "node-update-key", Role: "engine-node", NodeID: "node-update-id", Modules: []string{"engine"}}
	if _, err := savePairedMachine(m, n); err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\nif [ \"$1 $2\" = 'node capabilities' ]; then echo engine-node-v1; else echo 99.0.0-dev.1; fi\n")
	hash := sha256.Sum256(content)
	release := fmt.Sprintf(`{"tag_name":"edge","name":"edge 99.0.0-dev.1","assets":[{"name":%q,"browser_download_url":"https://fixture/binary","size":%d},{"name":"SHA256SUMS.txt","browser_download_url":"https://fixture/sums"}]}`, updateAssetName(), len(content))
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	runningVersion := filepath.Join(t.TempDir(), "running-version")
	nodeMux := newEngineWorkerMux(n.WebKey)
	http.DefaultTransport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch r.URL.Host {
		case "192.168.1.20:2511":
			if r.URL.Query().Get("channel") != "edge" || r.Header.Get("Authorization") != "Bearer "+n.WebKey || r.Header.Get("Cookie") != "" {
				t.Error("wrong channel or node credential")
			}
			// Simulate the node having its independent stable preference.
			_ = putStoreJSON(bkState, updateChannelKey, "stable")
			if r.URL.Path == "/api/ping" {
				version, err := os.ReadFile(runningVersion)
				if err != nil {
					t.Fatal(err)
				}
				sendJSON(w, 200, map[string]any{"version": strings.TrimSpace(string(version))})
			} else {
				nodeMux.ServeHTTP(w, r)
			}
			_ = putStoreJSON(bkState, updateChannelKey, "edge")
		case "api.github.com":
			if !strings.HasSuffix(r.URL.Path, "/tags/edge") {
				t.Error("wrong release channel", r.URL.Path)
			}
			io.WriteString(w, release)
		case "fixture":
			if r.URL.Path == "/binary" {
				w.Write(content)
			} else {
				fmt.Fprintf(w, "%x  %s\n", hash, updateAssetName())
			}
		default:
			t.Fatal("unexpected request", r.URL)
		}
		return w.Result(), nil
	})
	log := filepath.Join(t.TempDir(), "systemctl.log")
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(log) + "\nif [ \"$2\" = restart ]; then " + shellQuote(exe) + " --version > " + shellQuote(runningVersion) + "; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	base := "/api/machines/" + m.ID + "/node/update"
	req := httptest.NewRequest("GET", base, nil)
	req.SetPathValue("id", m.ID)
	w := httptest.NewRecorder()
	handleMachineNodeUpdate(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"latest":"99.0.0-dev.1"`) || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("check: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest("POST", base+"/apply", strings.NewReader(`{"version":"99.0.0-dev.1"}`))
	req.SetPathValue("id", m.ID)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handleMachineNodeUpdate(w, req)
	var result struct {
		OK         bool `json:"ok"`
		Restarting bool `json:"restarting"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || !result.OK || !result.Restarting {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	installed, _ := os.ReadFile(exe)
	previous, _ := os.ReadFile(exe + ".previous")
	if string(installed) != string(content) || string(previous) != "previous installed node" {
		t.Fatal("release layout update or rollback binary failed")
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(log)
		if strings.Contains(string(raw), "--user restart loom-node") {
			if _, err := os.Stat(runningVersion); err != nil {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			req := httptest.NewRequest("GET", base+"/ping", nil)
			req.SetPathValue("id", m.ID)
			w := httptest.NewRecorder()
			handleMachineNodeUpdate(w, req)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"99.0.0-dev.1"`) {
				t.Fatalf("running node: %d %s", w.Code, w.Body.String())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("node user service did not restart")
}
