package llamacpp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type memoryRouterState map[string]json.RawMessage

func (s memoryRouterState) GetString(key string) string   { var v string; s.GetJSON(key, &v); return v }
func (s memoryRouterState) PutString(key, v string) error { return s.PutJSON(key, v) }
func (s memoryRouterState) GetJSON(key string, dst any) bool {
	return json.Unmarshal(s[key], dst) == nil
}
func (s memoryRouterState) PutJSON(key string, v any) error {
	raw, err := json.Marshal(v)
	if err == nil {
		s[key] = raw
	}
	return err
}

type routerTransport func(*http.Request) (*http.Response, error)

func (f routerTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// The fake management API uses a transport, so owner isolation and selection
// ordering remain testable even in sandboxes where listening sockets are denied.
func TestRouterExplicitOwnersAndSelectionOrdering(t *testing.T) {
	newRouter := func(port int) (*Router, *[]string) {
		state := memoryRouterState{}
		var loads []string
		status := map[string]string{}
		r := &Router{
			BinaryPath: "llama-server", BackendPort: port, ModelsMax: 1,
			INIPath: filepath.Join(t.TempDir(), "router-models.ini"),
			State:   state, Lock: &sync.Mutex{},
			Authorize:    func(req *http.Request) { req.Header.Set("Authorization", "Bearer fixture") },
			APIKey:       func() (string, error) { return "fixture", nil },
			SetLastError: func(string) {},
		}
		r.Transport = routerTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "127.0.0.1:"+strconv.Itoa(port) {
				t.Fatalf("wrong router endpoint: %s", req.URL)
			}
			if req.Header.Get("Authorization") != "Bearer fixture" {
				t.Fatal("missing router authorization")
			}
			raw := `{"success":true}`
			switch req.URL.Path {
			case "/props":
				if req.URL.Query().Get("model") == "" {
					raw = `{"role":"router"}`
				} else {
					if req.URL.Query().Get("autoload") != "false" {
						t.Fatal("observation autoloaded")
					}
					raw = `{"default_generation_settings":{"n_ctx":4096}}`
				}
			case "/models":
				if req.URL.Query().Get("reload") == "1" {
					ini, err := os.ReadFile(r.INIPath)
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range strings.Split(string(ini), "\n") {
						if strings.HasPrefix(line, "[") {
							name := strings.Trim(line, "[]")
							if _, ok := status[name]; !ok {
								status[name] = "unloaded"
							}
						}
					}
				}
				data := []any{}
				for id, st := range status {
					data = append(data, map[string]any{"id": id, "status": map[string]any{"value": st}})
				}
				b, _ := json.Marshal(map[string]any{"data": data})
				raw = string(b)
			case "/models/load", "/models/unload":
				var body map[string]string
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				name := body["model"]
				if req.URL.Path == "/models/load" {
					if state.GetString(RouterStateCurrent) != name {
						t.Fatal("load preceded current selection")
					}
					loads = append(loads, name)
					status[name] = "loaded"
				} else {
					status[name] = "unloaded"
				}
			default:
				t.Fatalf("unexpected router path: %s", req.URL)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		return r, &loads
	}
	a, loads := newRouter(18081)
	b, otherLoads := newRouter(18082)
	selected := RouterEntry{Name: "selected", Label: "model", Options: [][2]string{{"m", "model.gguf"}}}
	build := func() (RouterEntry, error) { return selected, nil }
	if !a.Reachable() {
		t.Fatal("fake router unreachable")
	}
	if err := a.Activate(func() bool { return true }, build); err != nil {
		t.Fatal(err)
	}
	if err := a.Activate(func() bool { return true }, build); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*loads, []string{"selected"}) {
		t.Fatalf("loads = %v", *loads)
	}
	if ctx := a.ObservedContext(); ctx == nil || *ctx != 4096 {
		t.Fatalf("observed context = %v", ctx)
	}
	variant := selected
	variant.Name = "variant"
	if name, err := a.ActivateVariant(func() (RouterEntry, error) { return variant, nil }); err != nil || name != "variant" {
		t.Fatalf("variant %s: %v", name, err)
	}
	if a.State.GetString(RouterStateActive) != "selected" || a.CurrentName() != "variant" || a.ObservedContext() != nil {
		t.Fatal("variant replaced active selection or exposed active context")
	}
	if b.CurrentName() != "" || len(b.Entries()) != 0 || len(*otherLoads) != 0 {
		t.Fatal("router owners shared state")
	}
	if err := a.UnloadAll(); err != nil {
		t.Fatal(err)
	}
	if a.CurrentName() != "" {
		t.Fatal("unload retained selection")
	}
}

func TestRouterRetainsReferencedEntries(t *testing.T) {
	state := memoryRouterState{}
	r := &Router{State: state}
	entries := []RouterEntry{}
	for i := 0; i < 12; i++ {
		entries = append(entries, RouterEntry{Name: strings.Repeat("x", i+1), Used: int64(12 - i)})
	}
	_ = state.PutJSON(RouterStateEntries, entries)
	_ = state.PutString(RouterStateActive, entries[10].Name)
	_ = state.PutString(RouterStateCurrent, entries[11].Name)
	next, err := r.RememberEntry(RouterEntry{Name: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != RouterMaxEntries+2 || next[0].Name != "new" {
		t.Fatalf("retention = %v", next)
	}
	if next[len(next)-2].Name != entries[10].Name || next[len(next)-1].Name != entries[11].Name {
		t.Fatal("referenced entries trimmed")
	}
}

func TestRouterServerArgsCredentialPolicy(t *testing.T) {
	r := &Router{BinaryPath: "server", INIPath: "presets.ini", ModelsMax: 0, BackendPort: 18081}
	r.APIKey = func() (string, error) { return "", nil }
	got := r.ServerArgs(func() string { return "fallback-fixture" })
	want := []string{"server", "--models-preset", "presets.ini", "--models-max", "0", "--no-models-autoload", "--api-key", "fallback-fixture", "--host", "127.0.0.1", "--port", "18081"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q", got)
	}
	r.APIKey = func() (string, error) { return "", errors.New("key preparation") }
	got = r.ServerArgs(func() string { t.Fatal("fallback called after failed preparation"); return "" })
	if strings.Contains(strings.Join(got, " "), "--api-key") {
		t.Fatal("key emitted after preparation failure")
	}
}

func TestRouterRememberPreservesResidentSections(t *testing.T) {
	state := memoryRouterState{}
	r := &Router{State: state, ProtectedEntries: []string{"resident"}}
	entries := []RouterEntry{{Name: "resident", Used: 1}}
	for i := 0; i < RouterMaxEntries+2; i++ {
		entries = append(entries, RouterEntry{Name: "old-" + strconv.Itoa(i), Used: int64(i + 2)})
	}
	if err := state.PutJSON(RouterStateEntries, entries); err != nil {
		t.Fatal(err)
	}
	result, err := r.RememberEntry(RouterEntry{Name: "new"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range result {
		found = found || entry.Name == "resident"
	}
	if !found {
		t.Fatal("INI trim removed an in-flight model")
	}
}
