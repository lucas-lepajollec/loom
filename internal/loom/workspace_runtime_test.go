package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type registryFixtureAdapter struct {
	descriptor               RuntimeDescriptor
	connects, quotas, runs   int
	models                   any
	quota                    QuotaSnapshot
	connectError, quotaError error
	onConnect                func()
	onQuota                  func()
}

func (a *registryFixtureAdapter) Descriptor() RuntimeDescriptor { return a.descriptor }
func (a *registryFixtureAdapter) Run(context.Context, RuntimeTurn, ChatCallback) ([]Message, error) {
	a.runs++
	return nil, errors.New("generation must not run in registry action tests")
}
func (a *registryFixtureAdapter) Connect(_ context.Context, consent bool) (any, error) {
	if !consent {
		return nil, errors.New("fixture requires consent")
	}
	a.connects++
	if a.onConnect != nil {
		a.onConnect()
	}
	return a.models, a.connectError
}
func (a *registryFixtureAdapter) Quota(context.Context) (QuotaSnapshot, error) {
	a.quotas++
	if a.onQuota != nil {
		a.onQuota()
	}
	return a.quota, a.quotaError
}

func newRegistryFixture(id string) *registryFixtureAdapter {
	return &registryFixtureAdapter{
		descriptor: RuntimeDescriptor{ID: id, Name: "Compte de test", Kind: "harness", Implemented: true,
			Description: "Catalogue synthétique", CLI: "fixture", Consent: "Lecture du catalogue synthétique seulement.",
			Capabilities: []string{"connect", "quota"}},
		models: []string{"synthetic-model"},
		quota:  QuotaSnapshot{FetchedAt: 123, Windows: []QuotaWindow{{Group: "unknown", Remaining: nil}}},
	}
}

func isolateRuntimeRegistry(t *testing.T, adapters ...RuntimeAdapter) {
	t.Helper()
	old := registeredRuntimes
	registeredRuntimes = newRuntimeRegistry()
	for _, a := range adapters {
		if err := registeredRuntimes.register(a); err != nil {
			t.Fatal(err)
		}
	}
	quotaCache.Lock()
	items, attempts := quotaCache.items, quotaCache.attempts
	quotaCache.items = map[string]QuotaSnapshot{}
	quotaCache.attempts = map[string]time.Time{}
	quotaCache.Unlock()
	t.Cleanup(func() {
		registeredRuntimes = old
		quotaCache.Lock()
		quotaCache.items, quotaCache.attempts = items, attempts
		quotaCache.Unlock()
	})
}

func TestRuntimeRegistryOrderAndIsolation(t *testing.T) {
	reg := newRuntimeRegistry()
	first, second := newRegistryFixture("z-first"), newRegistryFixture("a-second")
	for _, a := range []RuntimeAdapter{first, second, plannedRuntimeAdapter{RuntimeDescriptor{ID: "planned", Kind: "harness"}}} {
		if err := reg.register(a); err != nil {
			t.Fatal(err)
		}
	}
	catalog := reg.catalog()
	if catalog[0].ID != "z-first" || catalog[1].ID != "a-second" || catalog[2].Capabilities == nil || len(catalog[2].Capabilities) != 0 {
		t.Fatalf("unstable order or planned capabilities: %+v", catalog)
	}
	catalog[0].Capabilities[0] = "corrupted"
	if reg.catalog()[0].Capabilities[0] != "connect" {
		t.Fatal("catalog mutated adapter capabilities")
	}
	if err := reg.register(first); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if a, ok := reg.lookup("z-first"); !ok || a != first {
		t.Fatal("duplicate replaced the original adapter")
	}
	if _, ok := reg.lookup("unknown"); ok {
		t.Fatal("unknown runtime resolved")
	}
	for _, a := range []RuntimeAdapter{
		nil,
		plannedRuntimeAdapter{RuntimeDescriptor{ID: "bad/id"}},
		plannedRuntimeAdapter{RuntimeDescriptor{ID: "dishonest-plan", Capabilities: []string{"chat"}}},
		plannedRuntimeAdapter{RuntimeDescriptor{ID: "no-connect-interface", Implemented: true, Capabilities: []string{"connect"}}},
		plannedRuntimeAdapter{RuntimeDescriptor{ID: "no-quota-interface", Implemented: true, Capabilities: []string{"quota"}}},
	} {
		if err := reg.register(a); err == nil {
			t.Fatal("invalid or dishonest adapter accepted")
		}
	}
}

func TestRuntimeRegistryStartupCatalog(t *testing.T) {
	want := []string{"llama.cpp", "openai-compatible", "antigravity", "codex", "claude-code", "pi", "gemini", "opencode", "hermes"}
	got := []string{}
	for _, d := range runtimeCatalog() {
		got = append(got, d.ID)
		if d.Description == "" {
			t.Fatalf("missing UI description: %s", d.ID)
		}
		if d.ID == "antigravity" {
			if d.CLI != "agy" || d.Consent == "" || !hasRuntimeCapability(d, "workdir") || !hasRuntimeCapability(d, "quota") || hasRuntimeCapability(d, "approvals") {
				t.Fatalf("incomplete native action descriptor: %+v", d)
			}
		}
		if d.ID == "codex" && (!hasRuntimeCapability(d, "quota") || !hasRuntimeCapability(d, "approvals")) {
			t.Fatal("Codex ACP/quota missing")
		}
		if !d.Implemented && len(d.Capabilities) != 0 {
			t.Fatal("planned runtime became executable")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog order = %v, want %v", got, want)
	}
}

func TestRuntimeActionsHTTPBoundary(t *testing.T) {
	testHome(t)
	fixture := newRegistryFixture("new-harness")
	isolateRuntimeRegistry(t, fixture, llamaRuntimeAdapter{}, plannedRuntimeAdapter{RuntimeDescriptor{ID: "planned", Kind: "harness"}})
	if err := storeWebKey("synthetic-registry-auth"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct {
		path, method, body, contentType, auth string
		want                                  int
	}{
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":true}`, "application/json", "", 401},
		{"/api/runtimes/new-harness/quota", "POST", `{}`, "application/json", "", 401},
		{"/api/runtimes/unknown/connect", "POST", `{}`, "application/json", "yes", 404},
		{"/api/runtimes/unknown/quota", "POST", `{}`, "application/json", "yes", 404},
		{"/api/runtimes/planned/connect", "POST", `{"consent":true}`, "application/json", "yes", 501},
		{"/api/runtimes/planned/quota", "POST", `{}`, "application/json", "yes", 400},
		{"/api/runtimes/llama.cpp/connect", "POST", `{"consent":true}`, "application/json", "yes", 501},
		{"/api/runtimes/new-harness/connect", "GET", `{}`, "application/json", "yes", 405},
		{"/api/runtimes/new-harness/quota", "GET", `{}`, "application/json", "yes", 405},
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":true}`, "text/plain", "yes", 415},
		{"/api/runtimes/new-harness/connect", "POST", `{}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":false}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":true,"extra":1}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":true} {}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/connect", "POST", `{"consent":true,"extra":"` + strings.Repeat("x", 128<<10) + `"}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/quota", "POST", `{"runtime_id":"codex"}`, "application/json", "yes", 400},
		{"/api/runtimes/new-harness/quota", "POST", `{} {}`, "application/json", "yes", 400},
	} {
		t.Run(tc.method+tc.path+tc.body[:min(len(tc.body), 30)], func(t *testing.T) {
			r := localTestRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			if tc.auth != "" {
				r.Header.Set("Authorization", "Bearer synthetic-registry-auth")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want != 401 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing private-cache boundary")
			}
		})
	}
	if fixture.connects != 0 || fixture.quotas != 0 || fixture.runs != 0 {
		t.Fatal("rejected request invoked an adapter")
	}
}

func TestRuntimeActionsDispatchAndLegacyAliases(t *testing.T) {
	testHome(t)
	agy, codex, future := newRegistryFixture("antigravity"), newRegistryFixture("codex"), newRegistryFixture("new-harness")
	codex.models = []codexModel{{Model: "synthetic-codex", Name: "Synthetic Codex"}}
	isolateRuntimeRegistry(t, agy, codex, future)
	mux := newWebMux()
	post := func(path, body string) *httptest.ResponseRecorder {
		r := localTestRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, id := range []string{"antigravity", "codex", "new-harness"} {
		w := post("/api/runtimes/"+id+"/connect", `{"consent":true}`)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "synthetic") {
			t.Fatalf("generic dispatch failed: %d %s", w.Code, w.Body.String())
		}
		if id != "new-harness" {
			alias := post("/api/workspace/"+id+"/connect", `{"consent":true}`)
			if alias.Code != 200 || alias.Body.String() != w.Body.String() {
				t.Fatal("legacy native catalog response changed")
			}
		}
	}
	w := post("/api/runtimes/new-harness/quota", `{}`)
	if w.Code != 200 {
		t.Fatalf("new quota reader not dispatched: %d", w.Code)
	}
	var response struct {
		Quota QuotaSnapshot `json:"quota"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Quota.RuntimeID != "new-harness" || response.Quota.Windows[0].Remaining != nil {
		t.Fatal("quota identity or unknown metric changed")
	}
	if got := post("/api/usage/refresh", `{"runtime_id":"new-harness"}`).Code; got != 429 || future.quotas != 1 {
		t.Fatal("generic and legacy quota routes do not share throttle")
	}
	quotaCache.Lock()
	quotaCache.attempts["new-harness"] = time.Time{}
	quotaCache.Unlock()
	future.quotaError = errors.New("synthetic quota unavailable")
	if got := post("/api/usage/refresh", `{"runtime_id":"new-harness"}`).Code; got != 400 {
		t.Fatalf("legacy quota failure = %d", got)
	}
	quotaCache.Lock()
	previous := quotaCache.items["new-harness"]
	quotaCache.Unlock()
	if previous.FetchedAt != 123 || previous.Error == "" || len(previous.Windows) != 1 {
		t.Fatal("failed refresh erased previous snapshot or stale marker")
	}
	if agy.runs != 0 || codex.runs != 0 || future.runs != 0 {
		t.Fatal("catalog/quota action generated a turn")
	}
}

func TestRuntimeActionsLockedVault(t *testing.T) {
	testHome(t)
	fixture := newRegistryFixture("fixture")
	isolateRuntimeRegistry(t, fixture)
	clearMemDEK()
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, action := range []string{"connect", "quota"} {
		r := localTestRequest("POST", "/api/runtimes/fixture/"+action, strings.NewReader(`{"consent":true}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 423 {
			t.Fatalf("locked action %s: %d", action, w.Code)
		}
	}
	if fixture.connects != 0 || fixture.quotas != 0 {
		t.Fatal("locked request consulted an account")
	}
}

func TestRuntimeActionsRelockDuringRead(t *testing.T) {
	for _, action := range []string{"connect", "quota"} {
		t.Run(action, func(t *testing.T) {
			testHome(t)
			clearMemDEK()
			fixture := newRegistryFixture("fixture")
			isolateRuntimeRegistry(t, fixture)
			lockDuringRead := func() {
				if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
					t.Error(err)
				}
			}
			fixture.onConnect, fixture.onQuota = lockDuringRead, lockDuringRead
			fixture.connectError = errors.New("synthetic account read error")
			fixture.quotaError = errors.New("synthetic account read error")
			r := localTestRequest("POST", "/api/runtimes/fixture/"+action, strings.NewReader(`{}`))
			if action == "connect" {
				r = localTestRequest("POST", "/api/runtimes/fixture/connect", strings.NewReader(`{"consent":true}`))
			}
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			newWebMux().ServeHTTP(w, r)
			if w.Code != 423 || strings.Contains(w.Body.String(), "synthetic account") {
				t.Fatalf("account error disclosed after relock: %d %s", w.Code, w.Body.String())
			}
			quotaCache.Lock()
			_, cached := quotaCache.items["fixture"]
			quotaCache.Unlock()
			if cached {
				t.Fatal("locked account result entered the cache")
			}
		})
	}
}
