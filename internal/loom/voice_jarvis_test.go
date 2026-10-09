package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

func jarvisTestSetup(t *testing.T) {
	t.Helper()
	testHome(t)
	oldJ, oldW, oldC, oldHTTP, oldNode := jarvis, workspaceSessions, conv, http.DefaultClient, currentEngineNode()
	jarvis = &jarvisSessions{sessions: map[string]*jarvisSession{}}
	workspaceSessions = newRuntimeSessions()
	conv = newTestConv()
	conv.ID = newSessionID()
	setEngineNode(nil)
	t.Cleanup(func() {
		// Transcript writes schedule index refreshes; drain both before replacing
		// process globals used by that existing background pipeline.
		transcriptJobs.Wait()
		brainSvcMu.Lock()
		service := brainSvc
		brainSvcMu.Unlock()
		if service != nil {
			service.writeRefresh.Wait()
		}
		jarvis = oldJ
		workspaceSessions = oldW
		conv = oldC
		http.DefaultClient = oldHTTP
		setEngineNode(oldNode)
	})
	if err := SetConfigKey("MODEL", "fixture.gguf"); err != nil {
		t.Fatal(err)
	}
	savePolicyRules(t)
	if err := SetConfigKey("voice.jarvis_fallback", "local:fixture.gguf"); err != nil {
		t.Fatal(err)
	}
}
func jarvisRequest(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleJarvis(w, r)
	return w
}
func jarvisCreate(t *testing.T, discussionID string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"discussion_id": discussionID})
	w := jarvisRequest(t, "/api/voice/jarvis/session", string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.SessionID == "" {
		t.Fatal(w.Body.String())
	}
	return response.SessionID
}
func TestJarvisModelResolution(t *testing.T) {
	local := ModelChoice{ID: "local:fixture", Kind: "local", Model: "fixture.gguf"}
	cloud := ModelChoice{ID: "cloud:fixture", Kind: "cloud", ProviderID: "p", Endpoint: "https://fixture.invalid/v1", Model: "m"}
	harness := ModelChoice{ID: "codex:m", Kind: "harness", RuntimeID: "codex", Model: "m"}
	for _, tc := range []struct {
		s        RuntimeSession
		settings jarvisSettings
		want     string
		fail     bool
	}{
		{RuntimeSession{RuntimeID: "llama.cpp", Model: "fixture.gguf"}, jarvisSettings{Model: "discussion", Fallback: cloud.ID}, local.ID, false},
		{RuntimeSession{RuntimeID: "openai-compatible", ProviderID: "p", Endpoint: cloud.Endpoint, Model: "m"}, jarvisSettings{Model: "discussion", Fallback: local.ID}, cloud.ID, false},
		{RuntimeSession{RuntimeID: "codex", Model: "m"}, jarvisSettings{Model: "discussion", Fallback: local.ID}, local.ID, false},
		{RuntimeSession{}, jarvisSettings{Model: "discussion", Fallback: cloud.ID}, cloud.ID, false},
		{RuntimeSession{RuntimeID: "codex"}, jarvisSettings{Model: "discussion"}, "", true},
		{RuntimeSession{}, jarvisSettings{Model: harness.ID}, "", true},
	} {
		routes := []ModelChoice{local, cloud}
		c, err := resolveJarvisModel(tc.settings, tc.s, routes)
		if (err != nil) != tc.fail || c.ID != tc.want {
			t.Fatalf("resolution: %+v %v", c, err)
		}
	}
}
func TestJarvisSessionLifecycleIsolationLimitsAndExpiry(t *testing.T) {
	jarvisTestSetup(t)
	id := jarvisCreate(t, conv.ID)
	jarvis.mu.Lock()
	s := jarvis.sessions[id]
	s.Owner = "another-user"
	jarvis.mu.Unlock()
	w := jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"hello"}`)
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = jarvisRequest(t, "/api/voice/jarvis/end", `{"session_id":"`+id+`","inject":true}`)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	jarvis.mu.Lock()
	s.LastUsed = time.Now().Add(-jarvisIdle)
	s.Busy = true
	jarvis.expireLocked(time.Now())
	if jarvis.sessions[id] == nil {
		t.Fatal("active turn expired")
	}
	s.Busy = false
	jarvis.expireLocked(time.Now())
	if jarvis.sessions[id] != nil {
		t.Fatal("idle session retained")
	}
	jarvis.mu.Unlock()
	id = jarvisCreate(t, "")
	s = jarvis.sessions[id]
	s.Turns = make([]Message, 2*jarvisMaxTurns)
	w = jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"hello"}`)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	for i := 0; i < jarvisMaxSessions; i++ {
		jarvis.sessions[newSessionID()] = &jarvisSession{Grant: s.Grant, LastUsed: time.Now()}
	}
	w = jarvisRequest(t, "/api/voice/jarvis/session", `{"discussion_id":"`+conv.ID+`"}`)
	if w.Code != 409 {
		t.Fatal("unbounded sessions", w.Code)
	}
}
func TestJarvisStreamingContextAndNoDiscussionWrite(t *testing.T) {
	jarvisTestSetup(t)
	conv.Messages = []Message{{Role: "user", Content: "Earlier discussion"}, {Role: "assistant", Content: "Earlier answer"}}
	id := jarvisCreate(t, conv.ID)
	calls := 0
	http.DefaultClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Model     string    `json:"model"`
			Messages  []Message `json:"messages"`
			MaxTokens int       `json:"max_tokens"`
			Tools     any       `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/v1/chat/completions" || payload.Model != "fixture.gguf" || payload.MaxTokens != 160 || payload.Tools != nil {
			t.Fatalf("bad request: %+v %s", payload, r.URL)
		}
		if !strings.Contains(payload.Messages[0].Content.(string), "Earlier discussion") || !strings.Contains(payload.Messages[0].Content.(string), "no tools") {
			t.Fatal("missing context")
		}
		if calls == 2 && len(payload.Messages) != 4 {
			t.Fatal("missing voice history", payload.Messages)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sseChunk("Bonjour. ") + sseChunk("Comment allez-vous ?") + "data: [DONE]\n\n"))}, nil
	})}
	for i := 0; i < 2; i++ {
		w := jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"Salut"}`)
		if !strings.Contains(w.Body.String(), `"type":"delta"`) || !strings.Contains(w.Body.String(), `"type":"done"`) {
			t.Fatal(w.Body.String())
		}
	}
	if len(conv.Messages) != 2 || conv.Seq != 0 || conv.Generating || len(workspaceSessions.runs) != 0 {
		t.Fatal("voice altered discussion")
	}
	w := jarvisRequest(t, "/api/voice/jarvis/end", `{"session_id":"`+id+`","inject":false}`)
	if w.Code != 200 || jarvis.sessions[id] != nil || len(conv.Messages) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestJarvisProviderPolicyAndDisconnect(t *testing.T) {
	jarvisTestSetup(t)
	p := CloudProvider{ID: "p", Name: "Provider", Model: "m", Endpoint: "https://fixture.invalid/v1"}
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.keys[p.ID] = "fixture-key"
	choice := cloudChoiceID(p.ID, p.Model)
	if err := SetConfigKey("voice.jarvis_model", choice); err != nil {
		t.Fatal(err)
	}
	id := jarvisCreate(t, "")
	reached := false
	http.DefaultClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) { reached = true; return nil, errors.New("unexpected") })}
	for _, decision := range []policy.Decision{policy.Confirm, policy.Deny} {
		savePolicyRules(t, policy.Rule{ID: "voice-policy", Scope: "global", Subject: "data.send_provider", Decision: decision})
		w := jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"Secret context"}`)
		if !strings.Contains(w.Body.String(), `"type":"error"`) || reached || len(jarvis.sessions[id].Turns) != 0 || len(workspaceSessions.list()) != 0 {
			t.Fatal("policy boundary", w.Body.String())
		}
	}
	savePolicyRules(t, policy.Rule{ID: "voice-policy", Scope: "global", Subject: "data.send_provider", Decision: policy.Allow})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/voice/jarvis/turn", strings.NewReader(`{"session_id":"`+id+`","text":"hello"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleJarvis(w, r)
	if len(jarvis.sessions[id].Turns) != 0 || jarvis.sessions[id].Busy {
		t.Fatal("disconnected turn retained")
	}
}
func TestJarvisRecentContextUTF8Bound(t *testing.T) {
	messages := []Message{{Role: "user", Content: "too old"}, {Role: "tool", Content: "private tool state"}, {Role: "assistant", Content: strings.Repeat("é", 6000)}, {Role: "user", Content: "newest"}}
	text := jarvisRecent(messages)
	if len(text) > 6<<10 || strings.Contains(text, "too old") || strings.Contains(text, "private tool") || !strings.HasSuffix(text, "user: newest\n") {
		t.Fatal("context not bounded")
	}
}
func TestJarvisInjectionNativeWorkspaceAndIdempotence(t *testing.T) {
	for _, kind := range []string{"native", "archive", "workspace", "bound-native"} {
		t.Run(kind, func(t *testing.T) {
			jarvisTestSetup(t)
			id := conv.ID
			if kind == "archive" {
				id = "archive"
				if err := saveArchive(&convArchive{ID: id, Title: "Archive"}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "workspace" || kind == "bound-native" {
				id = "workspace"
				s := RuntimeSession{ID: id, RuntimeID: "codex", Status: "idle"}
				if kind == "bound-native" {
					s.RuntimeID = "llama.cpp"
					s.Model = "fixture.gguf"
					s.NativeArchive = conv.ID
				}
				if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
					t.Fatal(err)
				}
			}
			sid := jarvisCreate(t, id)
			jarvis.sessions[sid].Turns = []Message{{Role: "user", Content: "Hello by voice"}, {Role: "assistant", Content: "Hello back"}}
			for i := 0; i < 2; i++ {
				w := jarvisRequest(t, "/api/voice/jarvis/end", `{"session_id":"`+sid+`","inject":true}`)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if jarvis.sessions[sid] != nil || conv.Generating || len(workspaceSessions.runs) != 0 || len(workspaceSessions.acp) != 0 {
				t.Fatal("injection triggered execution")
			}
			var messages []Message
			if s, ok := workspaceSessions.get(id); ok {
				messages = s.Messages
			} else {
				a, ok := loadArchive(id)
				if !ok {
					t.Fatal("not persisted")
				}
				messages = archivePortableText(a)
			}
			if len(messages) != 2 || messages[0].Source != "voice" || messages[1].Source != "voice" {
				t.Fatal("not idempotent or missing source", messages)
			}
			// Durable deduplication also covers a retried persistence operation.
			if err := injectVoiceExchange(id, sid, []Message{{Role: "user", Content: "Hello by voice"}, {Role: "assistant", Content: "Hello back"}}); err != nil {
				t.Fatal(err)
			}
			wire, _ := json.Marshal(modelMessages(messages))
			if strings.Contains(string(wire), "source") {
				t.Fatal("provenance sent to model")
			}
			transcriptJobs.Wait()
		})
	}
}

func TestJarvisCloudStreamUsesExistingDiscussionConsent(t *testing.T) {
	jarvisTestSetup(t)
	p := CloudProvider{ID: "p", Name: "Provider", Model: "m", Endpoint: "https://fixture.invalid/v1"}
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.keys[p.ID] = "selected-key"
	s := RuntimeSession{ID: "cloud-discussion", RuntimeID: "openai-compatible", ProviderID: p.ID, Endpoint: p.Endpoint, Model: p.Model, Status: "idle"}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	id := jarvisCreate(t, s.ID)
	http.DefaultClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != p.Endpoint+"/chat/completions" || r.Header.Get("Authorization") != "Bearer selected-key" {
			t.Fatal("wrong provider/credential")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sseChunk("Spoken cloud reply.") + "data: [DONE]\n\n"))}, nil
	})}
	w := jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"Hello"}`)
	if !strings.Contains(w.Body.String(), `"type":"done"`) || len(jarvis.sessions[id].Turns) != 2 {
		t.Fatal(w.Body.String())
	}
	saved, _ := workspaceSessions.get(s.ID)
	if len(saved.Messages) != 0 || len(saved.Turns) != 0 {
		t.Fatal("cloud voice wrote discussion")
	}
	delete(workspaceSessions.keys, p.ID)
	w = jarvisRequest(t, "/api/voice/jarvis/turn", `{"session_id":"`+id+`","text":"Again"}`)
	if !strings.Contains(w.Body.String(), `"type":"error"`) || len(jarvis.sessions[id].Turns) != 2 {
		t.Fatal("disconnected provider retained turn", w.Body.String())
	}
}
func TestJarvisCancelledStreamStopsProvider(t *testing.T) {
	jarvisTestSetup(t)
	id := jarvisCreate(t, conv.ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	http.DefaultClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			defer close(stopped)
			defer writer.Close()
			io.WriteString(writer, sseChunk("First words."))
			cancel()
			<-r.Context().Done()
			writer.CloseWithError(r.Context().Err())
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})}
	r := httptest.NewRequest("POST", "/api/voice/jarvis/turn", strings.NewReader(`{"session_id":"`+id+`","text":"Hello"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleJarvis(w, r)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("upstream did not abort")
	}
	if len(jarvis.sessions[id].Turns) != 0 || jarvis.sessions[id].Busy || strings.Contains(w.Body.String(), `"type":"done"`) {
		t.Fatal("cancelled stream completed", w.Body.String())
	}
}
func TestJarvisInjectionPublishesOnceAndRejectsBusyTargets(t *testing.T) {
	jarvisTestSetup(t)
	s := RuntimeSession{ID: "workspace", RuntimeID: "codex", Status: "idle"}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	sub := &discussionSubscriber{events: make(chan DiscussionEvent, 16)}
	workspaceSessions.subscribers[s.ID] = map[*discussionSubscriber]bool{sub: true}
	workspaceSessions.preparing[s.ID] = true
	messages := []Message{{Role: "user", Content: "Question"}, {Role: "assistant", Content: "Answer"}}
	if err := injectVoiceExchange(s.ID, "voice-1", messages); err == nil {
		t.Fatal("injected into busy target")
	}
	workspaceSessions.preparing[s.ID] = false
	for i := 0; i < 2; i++ {
		if err := injectVoiceExchange(s.ID, "voice-1", messages); err != nil {
			t.Fatal(err)
		}
	}
	if len(sub.events) != 4 {
		t.Fatal("duplicate or missing publication", len(sub.events))
	}
	first := <-sub.events
	if first["type"] != "turn_start" || first["source"] != "voice" {
		t.Fatal(first)
	}
	saved, _ := workspaceSessions.get(s.ID)
	if len(saved.Messages) != 2 || saved.Status != "idle" || len(saved.Turns) != 0 {
		t.Fatal("injection changed execution state", saved)
	}
	transcript := sessionTranscript(saved)
	if len(transcript.Entries) != 2 {
		t.Fatal("voice missing from transcript", transcript)
	}
}

func TestJarvisSettingsAuthAndCredentialRevocation(t *testing.T) {
	jarvisTestSetup(t)
	if err := storeWebKey("control-fixture"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerJarvisRoutes(webAPI(mux))
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/api/voice/jarvis", "", "wrong-key")
	if w.Code != 401 {
		t.Fatal("settings unauthenticated", w.Code)
	}
	w = request("GET", "/api/voice/jarvis", "", "control-fixture")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"routes"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/voice/jarvis", `{"model":"discussion","fallback":"local:fixture.gguf"}`, "control-fixture")
	if w.Code != 200 || readJarvisSettings().Fallback != "local:fixture.gguf" || ReadConfig()["MODEL"] != "fixture.gguf" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/voice/jarvis", `{"model":"codex:default","fallback":""}`, "control-fixture")
	if w.Code != 400 {
		t.Fatal("harness model accepted")
	}
	w = request("POST", "/api/voice/jarvis/session", `{"discussion_id":"`+conv.ID+`"}`, "control-fixture")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if err := storeWebKey("rotated-control"); err != nil {
		t.Fatal(err)
	}
	w = request("POST", "/api/voice/jarvis/turn", `{"session_id":"`+result.SessionID+`","text":"hello"}`, "rotated-control")
	if w.Code != 404 || jarvis.sessions[result.SessionID] != nil {
		t.Fatal("credential rotation retained voice session", w.Code, w.Body.String())
	}
}

func TestJarvisLifecycleShutdownDiscardsSessions(t *testing.T) {
	manager := &jarvisSessions{sessions: map[string]*jarvisSession{"fixture": {ID: "fixture"}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { manager.lifecycle(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("voice lifecycle did not stop")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.sessions) != 0 {
		t.Fatal("shutdown retained voice sessions")
	}
}
