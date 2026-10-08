package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/engine/llamacpp"
	"time"
)

type engineServiceHTTPFixture struct {
	router        *fakeRouter
	front         http.Handler
	streamStarted chan struct{}
	streamEnd     chan struct{}
	once          sync.Once
	mu            sync.Mutex
	calls         int
	auth          string
}

// Le transport simule le serveur externe, sans bind loopback ni vrai moteur.
func serviceHTTPFixture(t *testing.T) *engineServiceHTTPFixture {
	t.Helper()
	home := testHome(t)
	bin := fakeLlamaBin(t, home)
	models := filepath.Join(home, "models")
	if err := os.MkdirAll(models, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.gguf", "b.gguf", "c.gguf"} {
		if err := os.WriteFile(filepath.Join(models, name), []byte("GGUF"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	setConfig(t, "BIN="+bin+"\nMODEL="+filepath.Join(models, "a.gguf")+"\nHOST=127.0.0.1\nPORT=8081\nNP=4\n")
	if err := writeAPIKey("backend-key"); err != nil {
		t.Fatal(err)
	}
	f := &engineServiceHTTPFixture{router: &fakeRouter{iniDir: home, status: map[string]string{}}, streamStarted: make(chan struct{}), streamEnd: make(chan struct{})}
	old := http.DefaultTransport
	http.DefaultTransport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/loom/") {
			rec := httptest.NewRecorder()
			f.front.ServeHTTP(rec, r)
			resp := rec.Result()
			resp.Request = r
			return resp, nil
		}
		if r.URL.Path == "/v1/chat/completions" {
			f.mu.Lock()
			f.calls++
			f.auth = r.Header.Get("Authorization")
			f.mu.Unlock()
			var payload struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			f.router.mu.Lock()
			loaded := f.router.status[payload.Model] == "loaded"
			f.router.mu.Unlock()
			if !loaded {
				return nil, errors.New("inference reached unloaded model")
			}
			if payload.Stream {
				rd, wr := io.Pipe()
				go func() {
					_, _ = io.WriteString(wr, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
					f.once.Do(func() { close(f.streamStarted) })
					select {
					case <-f.streamEnd:
					case <-r.Context().Done():
					}
					_, _ = io.WriteString(wr, "data: {\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":7},\"timings\":{\"prompt_n\":2,\"predicted_n\":7}}\n\ndata: [DONE]\n\n")
					wr.Close()
				}()
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: rd, Request: r}, nil
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":5}}`)), Request: r}, nil
		}
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, r)
		if r.URL.Path == "/models/load" {
			f.router.mu.Lock()
			for id, status := range f.router.status {
				if status == "loading" {
					f.router.status[id] = "loaded"
				}
			}
			f.router.mu.Unlock()
		}
		resp := rec.Result()
		resp.Request = r
		return resp, nil
	})
	llamaOwner.InvalidateRouterMode()
	f.front = newOAIRouter("")
	t.Cleanup(func() {
		f.once.Do(func() { close(f.streamStarted) })
		select {
		case <-f.streamEnd:
		default:
			close(f.streamEnd)
		}
		http.DefaultTransport = old
		llamaOwner.InvalidateRouterMode()
	})
	if err := routerActivate(); err != nil {
		t.Fatal(err)
	}
	return f
}
func serviceHTTPCall(h http.Handler, model, secret, priority string, stream bool) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"model": model, "messages": []any{}, "stream": stream, "stream_options": map[string]bool{"include_usage": true}})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("X-Loom-Priority", priority)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func engineKeyAPICall(h http.Handler, path, body string) *httptest.ResponseRecorder {
	method := "POST"
	if body == "" {
		method = "GET"
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	h.ServeHTTP(w, r)
	return w
}
func createServiceHTTPKey(t *testing.T, body string) (engineAPIIdentity, string) {
	t.Helper()
	w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys", body)
	if w.Code != 200 {
		t.Fatalf("create key %d: %s", w.Code, w.Body)
	}
	var result struct {
		Key    engineAPIIdentity `json:"key"`
		Secret string            `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Key, result.Secret
}
func readServiceHTTPKeys(t *testing.T) []engineAPIIdentity {
	t.Helper()
	w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys", "")
	if w.Code != 200 {
		t.Fatalf("list %d: %s", w.Code, w.Body)
	}
	var result struct {
		Keys []engineAPIIdentity `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Keys
}
func TestEngineServiceHTTPStreamDrainAndTimeout(t *testing.T) {
	f := serviceHTTPFixture(t)
	stream := make(chan *httptest.ResponseRecorder, 1)
	go func() { stream <- serviceHTTPCall(f.front, "loom", "backend-key", "interactive", true) }()
	select {
	case <-f.streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not start")
	}
	old := routerCurrentName()
	s := currentEngineService()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"b.gguf"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer backend-key")
	w := httptest.NewRecorder()
	f.front.ServeHTTP(w, r)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"model_busy"`) || w.Header().Get("Retry-After") == "" {
		t.Fatalf("timeout %d %s", w.Code, w.Body)
	}
	swapped := make(chan *httptest.ResponseRecorder, 1)
	go func() { swapped <- serviceHTTPCall(f.front, "b.gguf", "backend-key", "interactive", false) }()
	waitServiceQueue(t, s, 1)
	f.router.mu.Lock()
	if f.router.status[old] != "loaded" || len(f.router.loads) != 1 {
		t.Error("queued swap interrupted old model")
	}
	f.router.mu.Unlock()
	close(f.streamEnd)
	select {
	case w := <-stream:
		if w.Code != 200 || !strings.Contains(w.Body.String(), "[DONE]") {
			t.Errorf("broken stream %d %s", w.Code, w.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung")
	}
	select {
	case w := <-swapped:
		if w.Code != 200 {
			t.Fatalf("swap %d %s", w.Code, w.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("swap hung")
	}
}
func TestEngineServiceHTTPBackgroundIdleAndReload(t *testing.T) {
	f := serviceHTTPFixture(t)
	if w := serviceHTTPCall(f.front, "loom", "backend-key", "interactive", false); w.Code != 200 {
		t.Fatalf("interactive %d %s", w.Code, w.Body)
	}
	if w := serviceHTTPCall(f.front, "b.gguf", "backend-key", "background", false); w.Code != 503 || !strings.Contains(w.Body.String(), "model_busy") {
		t.Fatalf("background %d %s", w.Code, w.Body)
	}
	if w := serviceHTTPCall(f.front, "loom", "backend-key", "background", false); w.Code != 200 {
		t.Fatalf("resident background %d %s", w.Code, w.Body)
	}
	s := currentEngineService()
	now := time.Now()
	s.mu.Lock()
	s.now = func() time.Time { return now }
	s.lastActivity = now.Add(-31 * time.Minute)
	s.mu.Unlock()
	s.idleTick()
	f.router.mu.Lock()
	for _, status := range f.router.status {
		if status != "unloaded" {
			t.Error("resident not unloaded")
		}
	}
	f.router.mu.Unlock()
	if w := serviceHTTPCall(f.front, "loom", "backend-key", "interactive", false); w.Code != 200 {
		t.Fatalf("wake %d %s", w.Code, w.Body)
	}
	health := httptest.NewRecorder()
	f.front.ServeHTTP(health, httptest.NewRequest("GET", "/health", nil))
	if health.Code != 200 {
		t.Fatalf("health after wake %d %s", health.Code, health.Body)
	}

}
func TestEngineKeysHTTPAuthLimitsAndUsage(t *testing.T) {
	f := serviceHTTPFixture(t)
	key, secret := createServiceHTTPKey(t, `{"name":"Editor","allowed_models":["a.gguf"],"max_concurrency":1,"requests_per_minute":2}`)
	if strings.Contains(string(getBytes(bkState, engineKeysState)), secret) {
		t.Fatal("secret stored in clear")
	}
	if key.ID == "" || key.Priority != "interactive" || !strings.HasPrefix(secret, "sk-loom-") {
		t.Fatalf("invalid key %+v", key)
	}
	if w := serviceHTTPCall(f.front, "loom", "wrong", "interactive", false); w.Code != 401 {
		t.Fatalf("auth %d", w.Code)
	}
	if w := serviceHTTPCall(f.front, "b.gguf", secret, "interactive", false); w.Code != 403 || !strings.Contains(w.Body.String(), "model_not_allowed") {
		t.Fatalf("allowed models %d %s", w.Code, w.Body)
	}
	stream := make(chan *httptest.ResponseRecorder, 1)
	go func() { stream <- serviceHTTPCall(f.front, "loom", secret, "interactive", true) }()
	select {
	case <-f.streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("no stream")
	}
	if w := serviceHTTPCall(f.front, "loom", secret, "interactive", false); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("concurrency %d %s", w.Code, w.Body)
	}
	close(f.streamEnd)
	select {
	case <-stream:
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung")
	}
	if w := serviceHTTPCall(f.front, "loom", secret, "interactive", false); w.Code != 200 {
		t.Fatalf("nonstream %d %s", w.Code, w.Body)
	}
	if w := serviceHTTPCall(f.front, "loom", secret, "interactive", false); w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limit") {
		t.Fatalf("rpm %d %s", w.Code, w.Body)
	}
	found := false
	for _, k := range readServiceHTTPKeys(t) {
		if k.ID == key.ID {
			found = true
			if k.Usage.Requests != 2 || k.Usage.Prompt != 24 || k.Usage.Completion != 12 || k.LastUsed.IsZero() {
				t.Fatalf("usage %+v", k)
			}
		}
	}
	if !found {
		t.Fatal("missing named key")
	}
	f.mu.Lock()
	if f.auth != "Bearer backend-key" {
		t.Error("client secret passed upstream")
	}
	f.mu.Unlock()
	if w := serviceHTTPCall(f.front, "loom", "backend-key", "interactive", false); w.Code != 200 {
		t.Fatalf("legacy %d %s", w.Code, w.Body)
	}
	for _, k := range readServiceHTTPKeys(t) {
		if k.ID == "default" && k.Usage.Requests != 1 {
			t.Fatalf("legacy usage %+v", k)
		}
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer "+loomInferenceSecret())
	identity, ok := oaiKeyOK(r)
	if !ok || identity.ID != "loom" {
		t.Fatal("internal identity missing")
	}
	if w := serviceHTTPCall(f.front, "loom", loomInferenceSecret(), "interactive", false); w.Code != 200 {
		t.Fatal(w.Body)
	}
	for _, k := range readServiceHTTPKeys(t) {
		if k.ID == "loom" {
			t.Fatal("internal identity persisted as a named key")
		}
	}
}
func TestEngineKeysHTTPUpdateRotateDelete(t *testing.T) {
	f := serviceHTTPFixture(t)
	key, secret := createServiceHTTPKey(t, `{"name":"Worker","priority":"background"}`)
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	identity, ok := oaiKeyOK(r)
	if !ok || enginePriority(r, identity) != "background" {
		t.Fatal("stored priority missing")
	}
	for _, test := range []struct {
		path, body string
		status     int
	}{
		{"/update", `{"id":"` + key.ID + `","name":"Renamed","allowed_models":["b.gguf"],"max_concurrency":2}`, 200},
		{"/update", `{"id":"` + key.ID + `","requests_per_minute":-1}`, 400},
		{"/update", `{"id":"missing","name":"x"}`, 404},
	} {
		w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys"+test.path, test.body)
		if w.Code != test.status {
			t.Fatalf("%s %d %s", test.path, w.Code, w.Body)
		}
	}
	w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys/rotate", `{"id":"`+key.ID+`"}`)
	var rotated struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rotated)
	if w.Code != 200 || rotated.Secret == "" || rotated.Secret == secret {
		t.Fatalf("rotate %d %s", w.Code, w.Body)
	}
	if w := serviceHTTPCall(f.front, "loom", secret, "interactive", false); w.Code != 401 {
		t.Fatal("old secret still accepted")
	}
	r.Header.Set("Authorization", "Bearer "+rotated.Secret)
	identity, ok = oaiKeyOK(r)
	if !ok || identity.Name != "Renamed" || identity.Concurrency != 2 {
		t.Fatal("rotation lost metadata")
	}
	w = engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys/delete", `{"id":"`+key.ID+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if _, ok := oaiKeyOK(r); ok {
		t.Fatal("deleted key still accepted")
	}
	w = engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys/update", "")
	if w.Code != 405 {
		t.Fatal("GET update should be inert")
	}
}
func TestEngineServiceControlHTTP(t *testing.T) {
	_ = serviceHTTPFixture(t)
	mux := http.NewServeMux()
	registerEngineControlRoutes(webAPI(mux))
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://gpu:2510"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/engine/service", "/api/engine/keys"} {
		if w := request("GET", path, "", "backend-key"); w.Code != 401 {
			t.Fatalf("control auth %d %s", w.Code, w.Body)
		}
	}
	w := request("POST", "/api/engine/service", `{"idle_unload_minutes":0,"swap_wait_seconds":10,"interactive_grace_minutes":2,"models_max":2}`, "control")
	if w.Code != 200 {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	var status map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	for _, field := range []string{"resident", "queue", "last_activity", "idle_unload_at", "models_max", "idle_unload_minutes", "swap_wait_seconds", "interactive_grace_minutes"} {
		if status[field] == nil {
			t.Fatalf("missing %s", field)
		}
	}
	if ReadConfig()["MODELS_MAX"] != "2" || ReadConfig()["ENGINE_IDLE_UNLOAD"] != "0" {
		t.Fatal("policy not saved")
	}
	for _, body := range []string{`{"idle_unload_minutes":-1}`, `{"models_max":65}`, `{"swap_wait_seconds":0}`, `{"swap_wait_seconds":1,"interactive_grace_minutes":10081}`} {
		if w := request("POST", "/api/engine/service", body, "control"); w.Code != 400 {
			t.Fatalf("invalid %s %d", body, w.Code)
		}
	}
	if ReadConfig()["ENGINE_SWAP_WAIT"] != "10" {
		t.Fatal("partial invalid update changed configuration")
	}
	w = request("GET", "/api/engine/keys", "", "control")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "http://gpu:8081/v1") || strings.Contains(w.Body.String(), "hash") {
		t.Fatalf("keys %d %s", w.Code, w.Body)
	}
}
func TestEngineUsageTapTimingsAndFragmentation(t *testing.T) {
	for _, test := range []struct {
		stream             bool
		raw                string
		prompt, completion int64
	}{
		{false, `{"timings":{"prompt_n":8,"predicted_n":3}}`, 8, 3},
		{true, "data: {\"timings\":{\"prompt_n\":1,\"predicted_n\":1}}\n\ndata: {\"timings\":{\"prompt_n\":8,\"predicted_n\":3}}\n\ndata: [DONE]\n\n", 8, 3},
		{false, `{"choices":[]}`, 0, 0},
	} {
		tap := &engineUsageTap{body: io.NopCloser(strings.NewReader(test.raw)), stream: test.stream}
		b := make([]byte, 1)
		for {
			_, err := tap.Read(b)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if tap.prompt != test.prompt || tap.completion != test.completion {
			t.Fatalf("tokens %d/%d", tap.prompt, tap.completion)
		}
	}
}
func TestEngineKeysRateWindowClock(t *testing.T) {
	_ = serviceHTTPFixture(t)
	key, _ := createServiceHTTPKey(t, `{"name":"rate","requests_per_minute":1}`)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	old := engineKeyNow
	engineKeyNow = func() time.Time { return now }
	t.Cleanup(func() { engineKeyNow = old })
	finish, err := beginEngineKey(key)
	if err != nil {
		t.Fatal(err)
	}
	finish(0, 0, "model_busy")
	if _, err = beginEngineKey(key); err == nil {
		t.Fatal("rpm not enforced")
	}
	now = now.Add(time.Minute)
	finish, err = beginEngineKey(key)
	if err != nil {
		t.Fatal(err)
	}
	finish(2, 1, "")
	for _, k := range readServiceHTTPKeys(t) {
		if k.ID == key.ID && (k.Usage.Requests != 2 || k.Usage.Prompt != 2 || k.Usage.LastError != "") {
			t.Fatal(k.Usage)
		}
	}
}

func TestEngineKeysLegacyRotationKeepsBackendCredential(t *testing.T) {
	f := serviceHTTPFixture(t)
	noteEngineLaunch([]string{"llama-server", "--api-key", "backend-key", "--models-max", "1"})
	w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys/rotate", `{"id":"default"}`)
	var result struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Secret == "" {
		t.Fatalf("rotate %d %s", w.Code, w.Body)
	}
	if w := serviceHTTPCall(f.front, "loom", "backend-key", "interactive", false); w.Code != 401 {
		t.Fatal("old legacy credential accepted")
	}
	if w := serviceHTTPCall(f.front, "loom", result.Secret, "interactive", false); w.Code != 200 {
		t.Fatalf("new legacy credential %d %s", w.Code, w.Body)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auth != "Bearer backend-key" {
		t.Fatal("rotation replaced the running backend credential")
	}
}
func TestEngineKeysNamedPriorityCannotPromote(t *testing.T) {
	f := serviceHTTPFixture(t)
	_, secret := createServiceHTTPKey(t, `{"name":"background","priority":"background"}`)
	if w := serviceHTTPCall(f.front, "b.gguf", secret, "interactive", false); w.Code != 503 || !strings.Contains(w.Body.String(), "model_busy") {
		t.Fatalf("background key promoted %d %s", w.Code, w.Body)
	}
}
func TestEngineServiceBenchRowUsesQueueAndPriority(t *testing.T) {
	f := serviceHTTPFixture(t)
	old := routerCurrentName()
	stream := make(chan *httptest.ResponseRecorder, 1)
	go func() { stream <- serviceHTTPCall(f.front, "loom", "backend-key", "interactive", true) }()
	select {
	case <-f.streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("stream not started")
	}
	// Dispatch front requests through the fake transport, just as web/serve use
	// separate processes. No management load is allowed before the old drain.
	transport := http.DefaultTransport
	http.DefaultTransport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "127.0.0.1:8081" && r.URL.Path == "/v1/chat/completions" {
			rec := httptest.NewRecorder()
			f.front.ServeHTTP(rec, r)
			resp := rec.Result()
			resp.Request = r
			return resp, nil
		}
		return transport.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = transport }()
	result := make(chan error, 1)
	go func() {
		_, _, err := runCompletionBenchModelContext(context.Background(), "bench", 32, nil, "b.gguf")
		result <- err
	}()
	waitServiceQueue(t, currentEngineService(), 1)
	f.router.mu.Lock()
	if f.router.status[old] != "loaded" {
		t.Error("bench preloaded before draining")
	}
	f.router.mu.Unlock()
	close(f.streamEnd)
	select {
	case <-stream:
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bench hung")
	}
	for _, key := range readServiceHTTPKeys(t) {
		if key.ID == "default" && key.Usage.Requests != 1 {
			t.Fatal("bench did not use internal identity")
		}
	}
}

func TestEngineServiceSingleIdleStopsOnlyOwnedChild(t *testing.T) {
	home := testHome(t)
	bin := filepath.Join(home, "llama-server")
	script := "#!/bin/sh\ncase \"$1\" in --help) exit 0;; esac\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(home, "a.gguf")
	if err := os.WriteFile(model, []byte("GGUF"), 0600); err != nil {
		t.Fatal(err)
	}
	setConfig(t, "BIN="+bin+"\nMODEL="+model+"\nPORT=8081\nENGINE_MODE=single\n")
	owner := llamaOwner
	llamaOwner = llamacpp.NewSupervisor()
	initOwnedLlamaSupervisor()
	t.Cleanup(func() { shutdownOwnedLlamaSupervisor(); llamaOwner = owner })
	if err := startOwnedLlama(bin, []string{bin}); err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport
	http.DefaultTransport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw := `{}`
		if r.URL.Path == "/health" {
			raw = `{"status":"ok"}`
		}
		if r.URL.Path == "/v1/chat/completions" {
			raw = `{"usage":{"prompt_tokens":3,"completion_tokens":2}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = transport })
	s := currentEngineService()
	now := time.Now()
	s.mu.Lock()
	s.now = func() time.Time { return now }
	s.lastActivity = now.Add(-time.Hour)
	s.mu.Unlock()
	s.idleTick()
	if ownedLlamaRunning() || !ownedLlamaManaged() {
		t.Fatal("idle did not retain the Loom front owner")
	}
	if ReadConfig()["MODEL"] != model {
		t.Fatal("idle forgot selected model")
	}
	h := newOAIRouter("")
	w := serviceHTTPCall(h, "loom", "", "interactive", false)
	if w.Code != 200 || !ownedLlamaRunning() {
		t.Fatalf("single wake %d %s", w.Code, w.Body)
	}
}

func TestEngineKeysLastDeletionDoesNotOpenKeylessFront(t *testing.T) {
	f := serviceHTTPFixture(t)
	if err := writeAPIKey(""); err != nil {
		t.Fatal(err)
	}
	key, secret := createServiceHTTPKey(t, `{"name":"only"}`)
	w := engineKeyAPICall(http.HandlerFunc(handleEngineKeys), "/api/engine/keys/delete", `{"id":"`+key.ID+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	for _, token := range []string{"", secret} {
		if w := serviceHTTPCall(f.front, "loom", token, "interactive", false); w.Code != 401 {
			t.Fatal("deleting last named key opened inference")
		}
	}
}

func TestEngineServiceCapacityHTTPAppliesAfterStreamDrain(t *testing.T) {
	f := serviceHTTPFixture(t)
	bin := ReadConfig()["BIN"]
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then\ncat <<'HELP'\n" + fakeRouterHelp + "HELP\nexit 0\nfi\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	previous := llamaOwner
	llamaOwner = llamacpp.NewSupervisor()
	initOwnedLlamaSupervisor()
	t.Cleanup(func() { shutdownOwnedLlamaSupervisor(); llamaOwner = previous })
	args := routerServerArgs(bin)
	if err := startOwnedLlama(bin, args); err != nil {
		t.Fatal(err)
	}
	stream := make(chan *httptest.ResponseRecorder, 1)
	go func() { stream <- serviceHTTPCall(f.front, "loom", "backend-key", "interactive", true) }()
	select {
	case <-f.streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("stream not started")
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- engineKeyAPICall(http.HandlerFunc(handleEngineService), "/api/engine/service", `{"models_max":2}`)
	}()
	waitServiceQueue(t, currentEngineService(), 1)
	engineLaunchState.Lock()
	max := engineLaunchState.max
	engineLaunchState.Unlock()
	if max != 1 {
		t.Fatal("capacity restarted before stream drained")
	}
	close(f.streamEnd)
	select {
	case <-stream:
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung")
	}
	select {
	case w := <-response:
		if w.Code != 200 {
			t.Fatalf("capacity %d %s", w.Code, w.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("capacity not applied")
	}
	engineLaunchState.Lock()
	max = engineLaunchState.max
	engineLaunchState.Unlock()
	if max != 2 || ReadConfig()["MODELS_MAX"] != "2" {
		t.Fatal("native capacity not applied")
	}
}

func TestEngineServicePreparingQueuedModelKeepsGPUEnvironment(t *testing.T) {
	_ = serviceHTTPFixture(t)
	t.Setenv("CUDA_VISIBLE_DEVICES", "original")
	cfg := ReadConfig()
	cfg["CUDA_VISIBLE_DEVICES"] = "queued"
	if _, err := buildServiceModelArgs(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CUDA_VISIBLE_DEVICES") != "original" {
		t.Fatal("preparing a queued/background model changed the process environment")
	}
}
