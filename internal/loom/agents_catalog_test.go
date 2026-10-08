package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentsCatalogSnapshotAndExclusions(t *testing.T) {
	testHome(t)
	entries, err := parseAgentsRegistry(agentsRegistrySnapshot)
	if err != nil || len(entries) != 41 {
		t.Fatal(len(entries), err)
	}
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	rows := catalogEntries(entries, nil, missing)
	reserved := map[string]bool{"codex-acp": false, "claude-acp": false, "pi-acp": false, "opencode": false, "antigravity-acp": false, "gemini": false}
	for _, row := range rows {
		if _, ok := reserved[row.ID]; ok {
			reserved[row.ID] = row.Builtin
			if row.Added || row.Installed {
				t.Fatal(row)
			}
			for _, a := range entries {
				if a.ID == row.ID {
					if _, err := a.agent("linux-x86_64", missing); err == nil {
						t.Fatal("reserved addition", a.ID)
					}
				}
			}
		}
	}
	for id, found := range reserved {
		if !found {
			t.Fatal(id)
		}
	}
	for _, a := range builtinACPAgents() {
		if a.ID == "gemini" {
			t.Fatal("Gemini builtin")
		}
	}
}
func TestAgentsRegistryLenientParsingAndArchiveSafety(t *testing.T) {
	data := `{"future":true,"agents":[{"id":"valid","name":"Valid","version":"1.2.3","future":{"x":1},"distribution":{"future":{}}},{"id":3},null,{"id":"valid","name":"Duplicate","version":"1"}]}`
	entries, err := parseAgentsRegistry([]byte(data))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	for _, archive := range []string{"http://example.com/a", "file:///a", "https://user:pass@example.com/a", "https:///a"} {
		data := `{"agents":[{"id":"unsafe","name":"Unsafe","version":"1","distribution":{"binary":{"linux-x86_64":{"cmd":"x","archive":"` + archive + `"}}}}]}`
		if _, err := parseAgentsRegistry([]byte(data)); err == nil {
			t.Fatal(archive)
		}
	}
}
func TestCatalogLaunchDistributions(t *testing.T) {
	data := `{"agents":[
	{"id":"npm","name":"Npm","version":"2.0.0","distribution":{"npx":{"package":"@org/pkg@old","args":["--acp"]}}},
	{"id":"python","name":"Python","version":"2.0.0","distribution":{"uvx":{"package":"pkg==old","args":["acp"]}}},
	{"id":"python-at","name":"Python","version":"2.0.0","distribution":{"uvx":{"package":"pkg@old"}}},
	{"id":"native","name":"Native","version":"2.0.0","repository":"https://example.com/install","distribution":{"binary":{"linux-x86_64":{"cmd":"./dir/agent","archive":"https://example.com/archive","args":["acp"]},"windows-x86_64":{"cmd":".\\dir\\agent.exe","archive":"https://example.com/archive"}}}}]}`
	entries, err := parseAgentsRegistry([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"-y", "@org/pkg@2.0.0", "--acp"}, {"pkg==2.0.0", "acp"}, {"pkg==2.0.0"}, {"acp"}}
	found := func(name string) (string, error) { return "/bin/" + name, nil }
	for i, a := range entries {
		built, err := a.agent("linux-x86_64", found)
		if err != nil || !reflect.DeepEqual(built.Args, want[i]) || !built.Custom || built.RegistryVersion != "2.0.0" {
			t.Fatal(built, err)
		}
		compat := acpCompatibility(built, map[string]any{"version": "3.0.0"}, nil)
		if compat.TestedVersionSource != "registry" || compat.TestedVersion != "2.0.0" || compat.AdapterPackage == "" || compat.AgentVersion != "3.0.0" || compat.Warning == "" {
			t.Fatal(compat)
		}
		if compat := acpCompatibility(built, map[string]any{"version": "2.0.0"}, nil); compat.Warning != "" {
			t.Fatal(compat)
		}
	}
	native := entries[3]
	if _, err := native.agent("linux-x86_64", func(string) (string, error) { return "", os.ErrNotExist }); err == nil {
		t.Fatal("missing binary")
	}
	if _, err := native.launch("plan9-amd64"); err == nil {
		t.Fatal("unsupported platform")
	}
	windows, err := native.launch("windows-x86_64")
	if err != nil || windows.Command != "agent.exe" {
		t.Fatal(windows, err)
	}
	if registryPlatform("linux", "arm64") != "linux-aarch64" {
		t.Fatal("platform")
	}
	entries[0].Distribution.NPX.Package = "--evil"
	if _, err := entries[0].launch("linux-x86_64"); err == nil {
		t.Fatal("option as package")
	}
}

type catalogRoundTrip func(*http.Request) (*http.Response, error)

func (f catalogRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCatalogRefreshCachingETagAndOfflineFallback(t *testing.T) {
	testHome(t)
	calls := atomic.Int32{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("If-None-Match") == `"registry-v1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"registry-v1"`)
		_, _ = w.Write(agentsRegistrySnapshot)
	})
	// Same httptest handler, without requiring sandbox listener permissions.
	client := &http.Client{Timeout: 10 * time.Second, Transport: catalogRoundTrip(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	c := &agentsCatalogue{url: "https://registry.test", client: client}
	now := time.Now()
	c.refresh(context.Background(), now)
	c.refresh(context.Background(), now.Add(time.Hour))
	if calls.Load() != 1 || c.cache.ETag != `"registry-v1"` {
		t.Fatal(calls.Load(), c.cache)
	}
	c.refresh(context.Background(), now.Add(24*time.Hour))
	if calls.Load() != 2 || len(c.entries()) != 41 {
		t.Fatal(calls.Load())
	}
	restarted := &agentsCatalogue{url: c.url, client: client}
	restarted.refresh(context.Background(), now.Add(25*time.Hour))
	if calls.Load() != 2 {
		t.Fatal("persisted daily throttle")
	}
	client.Transport = catalogRoundTrip(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("offline") })
	restarted.refresh(context.Background(), now.Add(48*time.Hour))
	restarted.refresh(context.Background(), now.Add(49*time.Hour))
	if len(restarted.entries()) != 41 || calls.Load() != 3 {
		t.Fatal("offline cache/throttle", calls.Load())
	}
	if err := putStoreJSON(bkState, agentsRegistryState, registryCache{}); err != nil {
		t.Fatal(err)
	}
	offline := &agentsCatalogue{url: c.url, client: client}
	offline.refresh(context.Background(), now)
	if len(offline.entries()) != 41 {
		t.Fatal("embedded fallback")
	}
}
func TestAgentsCatalogAddRemoveAPI(t *testing.T) {
	testHome(t)
	previous := officialAgentsCatalog
	officialAgentsCatalog = &agentsCatalogue{loaded: true, cache: registryCache{Data: agentsRegistrySnapshot, CheckedAt: time.Now()}}
	t.Cleanup(func() { officialAgentsCatalog = previous; registeredRuntimes.remove("registry-qwen-code") })
	post := func(h http.HandlerFunc, id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+id+`"}`))
		r.Header.Set("Content-Type", "application/json")
		h(w, r)
		return w
	}
	if w := post(handleAgentsCatalogAdd, "qwen-code"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	a, ok := acpAgentFor("registry-qwen-code")
	if !ok || a.RegistryVersion == "" || len(loadCatalogAgents()) != 1 {
		t.Fatal(a, ok)
	}
	registeredRuntimes.remove(a.ID)
	registerCatalogAgents()
	if _, ok := acpAgentFor(a.ID); !ok {
		t.Fatal("startup restoration")
	}
	if w := post(handleAgentsCatalogAdd, "qwen-code"); w.Code != 200 || len(loadCatalogAgents()) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	handleAgentsCatalog(w, httptest.NewRequest("GET", "/", nil))
	var rows []catalogAgent
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "qwen-code" && !row.Added {
			t.Fatal(row)
		}
	}
	if w := post(handleAgentsCatalogAdd, "gemini"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := post(handleAgentsCatalogAdd, "missing"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := post(handleAgentsCatalogRemove, "qwen-code"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, ok := acpAgentFor(a.ID); ok || len(loadCatalogAgents()) != 0 {
		t.Fatal("not removed")
	}
	if w := post(handleAgentsCatalogRemove, "qwen-code"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
