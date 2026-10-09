package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/notify"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type phoneFixture struct {
	m        *runtimeSessions
	id       string
	gate     chan struct{}
	answer   chan agent.RequestAnswer
	deadline chan bool
}

func (a *phoneFixture) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "phone-fixture", Name: "Phone fixture", Kind: "harness", Implemented: true, Capabilities: []string{"chat"}}
}
func (a *phoneFixture) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	_, hasDeadline := ctx.Deadline()
	a.deadline <- hasDeadline
	select {
	case <-a.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	broker := agent.NewRequestBroker("phone-fixture", func(e AgentEvent) bool {
		return emit(StreamEvent{AgentEvent: &e, ACPEvent: DiscussionEvent{"type": e.Type, "agent_event": e, "request": e.Request, "request_id": e.RequestID}})
	})
	a.m.acpMu.Lock()
	a.m.requests = map[string]*agent.RequestBroker{a.id: broker}
	a.m.acpMu.Unlock()
	defer broker.Cancel()
	request := agent.AgentRequest{ID: "pi:phone", Kind: "user_input", Questions: []agent.InputQuestion{{ID: "target", Question: "PRIVATE_QUESTION choose a target", Options: []agent.RequestOption{{ID: "a", Label: "First"}, {ID: "b", Label: "Second"}}}}}
	answer, err := broker.Ask(ctx, AgentEvent{Type: "request.opened", Runtime: "phone-fixture", Request: &request})
	a.answer <- answer
	emit(StreamEvent{Content: "Finished after phone answer"})
	return nil, err
}

type phase4Transport struct{ handler http.Handler }

func (f phase4Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
func TestBackgroundTurnPhoneNotificationAnswerAndPersistence(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	old := workspaceSessions
	workspaceSessions = m
	t.Cleanup(func() { m.shutdownACP(); drainPhase4Jobs(); workspaceSessions = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		if m.notify != nil {
			m.notify.workers.Wait()
		}
	}()
	fixture := &phoneFixture{m: m, gate: make(chan struct{}), answer: make(chan agent.RequestAnswer, 1), deadline: make(chan bool, 1)}
	isolateRuntimeRegistry(t, fixture)
	cfg := notify.Default()
	cfg.PublicBaseURL = "https://loom.test"
	cfg.Ntfy.Enabled = true
	cfg.Ntfy.Topic = "private-topic"
	cfg.Rules.RateSeconds = 1
	if err := putStoreJSON(bkState, notifyConfigKey, cfg); err != nil {
		t.Fatal(err)
	}
	published := make(chan notify.NtfyPayload, 4)
	originalClient := notificationClient
	t.Cleanup(func() { notificationClient = originalClient })
	notificationClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload notify.NtfyPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		published <- payload
		w.WriteHeader(200)
	})}}
	mux := http.NewServeMux()
	api := func(p string, h http.HandlerFunc) { mux.HandleFunc(p, requireWebAuth(h)) }
	registerNotifications(mux, api, ctx)
	api("/api/tasks", handleTasks)
	api("/api/events", handleDomainEvents)
	if err := storeWebKey("control-fixture"); err != nil {
		t.Fatal(err)
	}
	session, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	session.RuntimeID = "phone-fixture"
	session.Model = "default"
	fixture.id = session.ID
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	// A connected tab leaves before the agent asks. Its cancellation must not
	// cancel execution, requests, or notification delivery.
	subCtx, disconnect := context.WithCancel(ctx)
	subReady := make(chan struct{}, 1)
	subDone := make(chan bool, 1)
	go func() {
		subDone <- m.subscribeDiscussion(subCtx, session.ID, func(DiscussionEvent) bool {
			select {
			case subReady <- struct{}{}:
			default:
			}
			return true
		})
	}()
	<-subReady
	if err := m.start(session.ID, "background-phone-turn", "Run background work"); err != nil {
		t.Fatal(err)
	}
	if hasDeadline := <-fixture.deadline; hasDeadline {
		t.Fatal("harness still has cloud's three minute timeout")
	}
	disconnect()
	<-subDone
	m.mu.Lock()
	subCount := len(m.subscribers[session.ID])
	m.mu.Unlock()
	if subCount != 0 {
		t.Fatal("tab still subscribed")
	}
	close(fixture.gate)
	var payload notify.NtfyPayload
	select {
	case payload = <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not notify with all tabs closed")
	}
	if payload.Click != "https://loom.test/#/chat/"+session.ID || len(payload.Actions) != 2 || payload.Actions[1].Label != "Second" || strings.Contains(payload.Message, "PRIVATE_QUESTION") {
		t.Fatal(payload)
	}
	stored := RuntimeSession{}
	if !getStoreJSON(bkRuntimeSessions, session.ID, &stored) || len(stored.PendingRequests) != 1 {
		t.Fatal("request not persisted")
	}
	tasks := m.tasks(time.Now())
	if len(tasks) != 1 || tasks[0].Status != "waiting_input" || len(tasks[0].Requests) != 1 {
		t.Fatal(tasks)
	}
	tokenURL := payload.Actions[1].URL
	// A capability cannot authorize any other API, either as a cookie or Bearer.
	forbidden := httptest.NewRequest("GET", "/api/tasks", nil)
	forbidden.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(tokenURL, "https://loom.test/api/notify/answer/"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, forbidden)
	if w.Code != 401 {
		t.Fatal("action token authorized Tasks", w.Code)
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", tokenURL, strings.NewReader(`{"answers":{"target":["a"]}}`))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("phone action failed", w.Code, w.Body.String())
	}
	select {
	case answer := <-fixture.answer:
		if answer.Answers["target"][0] != "b" {
			t.Fatal("body overrode signed answer", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("agent did not resume")
	}
	awaitCloudFinished(t, m, session.ID)
	stored = RuntimeSession{}
	if !getStoreJSON(bkRuntimeSessions, session.ID, &stored) || stored.Status != "complete" || len(stored.PendingRequests) != 0 || stored.Messages[len(stored.Messages)-1].Content != "Finished after phone answer" {
		t.Fatal("result not persisted", stored)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", tokenURL, nil))
	if w.Code != 409 {
		t.Fatal("action replay accepted", w.Code)
	}
	eventsList, _, _ := m.events.Snapshot(0)
	types := []events.Type{}
	for _, e := range eventsList {
		if e.DiscussionID == session.ID {
			types = append(types, e.Type)
		}
	}
	want := []events.Type{events.TaskStarted, events.TaskWaiting, events.TaskResumed, events.TaskCompleted}
	if len(types) != len(want) {
		t.Fatal(types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatal(types)
		}
	}
	tasks = m.tasks(time.Now())
	if len(tasks) != 1 || tasks[0].Status != "done" || tasks[0].FinishedAt == 0 {
		t.Fatal("recent completion missing", tasks)
	}
}
func TestNotificationConfigSecretsSecureContextAndVAPIDPersistence(t *testing.T) {
	home := testHome(t)
	m := newRuntimeSessions()
	s := m.notifications(nil)
	for _, raw := range []string{"http://192.168.1.9/api/notify", "http://loom.test/api/notify", "https://loom.test/api/notify?secure=false"} {
		w := httptest.NewRecorder()
		s.handleConfig(w, httptest.NewRequest("GET", raw, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"secure":false`) || !strings.Contains(w.Body.String(), `"push_public_key":""`) {
			t.Fatal("insecure reporting", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.handleConfig(w, httptest.NewRequest("GET", "https://loom.test/api/notify", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	priv, pub, err := vapidKeys()
	if err != nil {
		t.Fatal(err)
	}
	priv2, pub2, err := vapidKeys()
	if err != nil || priv != priv2 || pub != pub2 {
		t.Fatal("VAPID keys changed")
	}
	if getStr(bkState, stPushVAPIDPriv) != "" {
		t.Fatal("VAPID private key left plaintext in state")
	}
	sealed, err := os.ReadFile(filepath.Join(home, "secrets", "providers", "notify-vapid.sealed"))
	if err != nil || strings.Contains(string(sealed), priv) {
		t.Fatal("VAPID key not sealed", err)
	}
	c := notify.Default()
	c.Ntfy.Enabled = true
	c.Ntfy.Topic = "topic"
	body, _ := json.Marshal(map[string]any{"config": c, "ntfy_token": "SECRET_TOKEN", "webhook_secret": "SECRET_HEADER"})
	req := httptest.NewRequest("POST", "http://192.168.1.9/api/notify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.handleConfig(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "SECRET_") || !strings.Contains(w.Body.String(), `"token_set":true`) || !strings.Contains(w.Body.String(), `"public_base_url":"http://192.168.1.9"`) {
		t.Fatal("redaction/default base URL", w.Code, w.Body.String())
	}
	if secret, err := readProviderSecret("notify-ntfy"); err != nil || secret != "SECRET_TOKEN" {
		t.Fatal("secret not remembered", secret, err)
	}
	c.Push.Enabled = true
	body, _ = json.Marshal(map[string]any{"config": c})
	req = httptest.NewRequest("POST", "http://192.168.1.9/api/notify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.handleConfig(w, req)
	if w.Code != 400 {
		t.Fatal("push offered on LAN HTTP", w.Code)
	}
	// Subscribe has the same secure-context and destination checks as settings.
	for _, raw := range []string{`{"endpoint":"http://push.test/x"}`, `{"endpoint":"https://127.0.0.1/x"}`, `{"endpoint":"https://localhost/x"}`} {
		req := httptest.NewRequest("POST", "https://loom.test/api/notify/push/subscribe", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handlePushSubscribe(w, req)
		if w.Code != 400 {
			t.Fatal("unsafe push destination", raw, w.Code)
		}
	}
}
func TestOfferedNotificationActionsAndHealthEvents(t *testing.T) {
	for _, r := range []agent.AgentRequest{
		{Kind: "approval", Options: []agent.RequestOption{{ID: "allow", Label: "Allow"}, {ID: "deny", Label: "Deny"}}},
		{Kind: "user_input", Questions: []agent.InputQuestion{{ID: "choice", Options: []agent.RequestOption{{ID: "one", Label: "One"}}}}},
	} {
		labels, answers := requestChoices(r)
		if len(labels) != len(answers) || len(answers) == 0 {
			t.Fatal(r)
		}
		for _, a := range answers {
			if agent.ValidateAnswer(r, a) != nil {
				t.Fatal("button offered invalid answer", a)
			}
		}
	}
	for _, r := range []agent.AgentRequest{{Kind: "elicitation"}, {Kind: "user_input", Questions: []agent.InputQuestion{{ID: "q", Secret: true, Options: []agent.RequestOption{{ID: "one"}}}}}, {Kind: "user_input", Questions: []agent.InputQuestion{{ID: "q", MultiSelect: true, Options: []agent.RequestOption{{ID: "one"}}}}}} {
		_, a := requestChoices(r)
		if len(a) != 0 {
			t.Fatal("unsafe question got action buttons")
		}
	}
	m := newRuntimeSessions()
	m.observeHealth("node:a", "Node", "a", true, false)
	m.observeHealth("node:a", "Node", "a", false, false)
	m.observeHealth("node:a", "Node", "a", false, false)
	m.observeHealth("node:a", "Node", "a", true, false)
	m.observeHealth("engine", "Engine", "", true, true)
	m.observeHealth("engine", "Engine", "", false, true)
	list, _, _ := m.events.Snapshot(0)
	if len(list) != 3 || list[0].Type != events.NodeOffline || list[1].Type != events.NodeOnline || list[2].Type != events.EngineDown {
		t.Fatal(list)
	}
}
func TestTasksRetentionBoundsAndEventCursor(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	now := time.Now()
	session, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	session.Status = "complete"
	for i := 0; i < 130; i++ {
		finish := now.Add(-time.Duration(i) * time.Minute).UnixMilli()
		session.Turns = append(session.Turns, RuntimeTurnRecord{StartedAt: finish - 1000, FinishedAt: finish, Outcome: "done", RuntimeID: "fixture", Model: "model"})
	}
	session.Turns = append(session.Turns, RuntimeTurnRecord{StartedAt: now.Add(-49 * time.Hour).UnixMilli(), FinishedAt: now.Add(-48 * time.Hour).UnixMilli(), Outcome: "failed"})
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	list := m.tasks(now)
	if len(list) != 100 {
		t.Fatal("recent bound", len(list))
	}
	for _, task := range list {
		if task.ElapsedSeconds != 1 || task.Requests == nil || task.Status != "done" {
			t.Fatal(task)
		}
	}
	original := workspaceSessions
	workspaceSessions = m
	defer func() { workspaceSessions = original }()
	w := httptest.NewRecorder()
	handleDomainEvents(w, httptest.NewRequest("GET", "/api/events?since=not-a-number", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	m.events.Publish(events.Event{Type: events.TaskStarted, Title: "Started"})
	w = httptest.NewRecorder()
	handleDomainEvents(w, httptest.NewRequest("GET", "/api/events?since=999", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"gap":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestNotificationPendingNeverReturnsActionTokens(t *testing.T) {
	testHome(t)
	s := newRuntimeSessions().notifications(nil)
	s.pending = []notify.Message{{Title: "Loom", URL: "https://loom.test/#/chat/d", Event: events.Event{At: time.Now().UnixMilli()}, Actions: []notify.Action{{URL: "CAPABILITY"}}}}
	w := httptest.NewRecorder()
	s.handlePending(w, httptest.NewRequest("GET", "https://loom.test/api/notify/pending", nil))
	if strings.Contains(w.Body.String(), "CAPABILITY") {
		t.Fatal("pending exposed action capability")
	}
	if _, err := publicPushDial(context.Background(), "tcp", "127.0.0.1:12345"); err == nil {
		t.Fatal("local push dial accepted")
	}
}

func TestACPApprovalWithoutTabsAnsweredByCapability(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	session := createACPSession(t, m, a, "ask")
	if err := m.start(session.ID, "phone-approval-turn", "fixture"); err != nil {
		t.Fatal(err)
	}
	var request agent.AgentRequest
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := m.get(session.ID)
		if len(s.PendingRequests) > 0 {
			request = s.PendingRequests[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if request.ID == "" {
		t.Fatal("no persisted approval without subscribers")
	}
	s := m.notifications(nil)
	cfg := notify.Default()
	cfg.PublicBaseURL = "https://loom.test"
	message := s.message(cfg, events.Event{Type: events.TaskWaiting, DiscussionID: session.ID, TaskID: "fixture", RequestID: request.ID, Title: "Fixture", Status: "waiting_approval"})
	if len(message.Actions) != len(request.Options) || message.Actions[0].Action != "http" {
		t.Fatal("approval options lost", message.Actions)
	}
	req := httptest.NewRequest("POST", message.Actions[0].URL, nil)
	u, _ := url.Parse(message.Actions[0].URL)
	req.SetPathValue("token", strings.TrimPrefix(u.Path, "/api/notify/answer/"))
	w := httptest.NewRecorder()
	s.handleAnswer(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	finished := waitACPTurn(t, m, session.ID)
	if len(finished.PendingRequests) != 0 || len(finished.Messages) < 2 {
		t.Fatal("ACP result missing", finished)
	}
	var stored RuntimeSession
	if !getStoreJSON(bkRuntimeSessions, session.ID, &stored) || stored.Status != "complete" {
		t.Fatal("ACP result not persisted")
	}
}

type nativePhoneFixture struct {
	gate    chan struct{}
	started chan struct{}
}

func (a nativePhoneFixture) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "llama.cpp", Name: "Native fixture", Kind: "local", Implemented: true, Capabilities: []string{"chat"}}
}
func (a nativePhoneFixture) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	close(a.started)
	select {
	case <-a.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	emit(StreamEvent{Content: "Native background result"})
	return nil, nil
}
func TestNativeTurnWithoutSubscriberPersistsAndAppearsInTasks(t *testing.T) {
	testHome(t)
	setConfig(t, "MODEL=fixture.gguf\nCTX=4096\n")
	m := newRuntimeSessions()
	oldManager, oldConv, oldHealth := workspaceSessions, conv, healthClient
	c := newTestConv()
	c.ID = newSessionID()
	workspaceSessions = m
	conv = c
	t.Cleanup(func() { drainPhase4Jobs(); workspaceSessions = oldManager; conv = oldConv; healthClient = oldHealth })
	healthClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}}
	a := nativePhoneFixture{gate: make(chan struct{}), started: make(chan struct{})}
	isolateRuntimeRegistry(t, a)
	ctx, disconnect := context.WithCancel(context.Background())
	ready := make(chan bool, 1)
	done := make(chan bool, 1)
	go func() {
		c.Subscribe(ctx, 0, func(map[string]any) bool {
			select {
			case ready <- true:
			default:
			}
			return true
		})
		done <- true
	}()
	if err := c.StartTurn("Work locally", nil, Caps{}, 0.7); err != nil {
		t.Fatal(err)
	}
	<-a.started
	disconnect()
	<-done
	tasks := m.tasks(time.Now())
	if len(tasks) != 1 || tasks[0].Status != "running" {
		t.Fatal("original native discussion missing", tasks)
	}
	close(a.gate)
	deadline := time.Now().Add(3 * time.Second)
	saved := false
	for time.Now().Before(deadline) {
		raw, ok := getStoreBytes(bkChat, "conversation")
		if ok && strings.Contains(string(raw), "Native background result") && strings.Contains(string(raw), `"outcome":"done"`) {
			saved = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !saved {
		t.Fatal("native result not persisted with closed tab")
	}
	tasks = m.tasks(time.Now())
	if len(tasks) != 1 || tasks[0].Status != "done" || tasks[0].FinishedAt == 0 {
		t.Fatal("native completion missing", tasks)
	}
	for time.Now().Before(deadline) {
		list, _, _ := m.events.Snapshot(0)
		if len(list) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	list, _, _ := m.events.Snapshot(0)
	if len(list) != 2 || list[0].Type != events.TaskStarted || list[1].Type != events.TaskCompleted {
		t.Fatal(list)
	}
}

func TestActionEndpointBindingExpiryTamperAndMethod(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s := m.notifications(nil)
	session, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	broker := agent.NewRequestBroker("fixture", func(AgentEvent) bool { return true })
	defer broker.Cancel()
	request := agent.AgentRequest{ID: "pi:bound", Kind: "approval", Options: []agent.RequestOption{{ID: "allow", Label: "Allow"}}}
	if _, err := broker.Open(context.Background(), AgentEvent{Type: "request.opened", Request: &request}); err != nil {
		t.Fatal(err)
	}
	m.requests = map[string]*agent.RequestBroker{session.ID: broker}
	tokens, err := s.actionTokens()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	issue := func(sid, rid string, at time.Time, ttl time.Duration) string {
		token, err := tokens.Issue(sid, rid, agent.RequestAnswer{Decision: "allow"}, at, ttl)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	call := func(method, token string) int {
		r := httptest.NewRequest(method, "https://loom.test/api/notify/answer/"+token, nil)
		r.SetPathValue("token", token)
		w := httptest.NewRecorder()
		s.handleAnswer(w, r)
		return w.Code
	}
	good := issue(session.ID, request.ID, now, time.Hour)
	for _, token := range []string{issue("other", request.ID, now, time.Hour), issue(session.ID, "pi:wrong", now, time.Hour), issue(session.ID, request.ID, now.Add(-2*time.Second), time.Second), good + "x"} {
		if got := call("POST", token); got != 409 {
			t.Fatal("invalid capability accepted", got)
		}
	}
	if got := call("GET", good); got != 405 {
		t.Fatal("GET action executed", got)
	}
	if got := call("POST", good); got != 200 {
		t.Fatal("valid capability rejected", got)
	}
	if got := call("POST", good); got != 409 {
		t.Fatal("replay", got)
	}
}
func TestNtfyLargeApprovalSplitsOfferedButtons(t *testing.T) {
	count, options := 0, 0
	client := &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notify.NtfyPayload
		json.NewDecoder(r.Body).Decode(&p)
		count++
		options += len(p.Actions)
		if len(p.Actions) > 3 {
			t.Error("ntfy action limit exceeded")
		}
		w.WriteHeader(200)
	})}}
	m := notify.Message{Title: "Fixture", Body: "waiting_approval", URL: "https://loom.test/#/chat/d", Actions: []notify.Action{{Label: "A"}, {Label: "B"}, {Label: "C"}, {Label: "D"}}}
	if err := notify.SendNtfy(context.Background(), client, notify.Ntfy{ServerURL: "https://ntfy.test", Topic: "fixture"}, "", m); err != nil {
		t.Fatal(err)
	}
	if count != 2 || options != 4 {
		t.Fatal("offered choices lost", count, options)
	}
}

type taskStreamRecorder struct {
	header http.Header
	frames chan string
}

func (w *taskStreamRecorder) Header() http.Header { return w.header }
func (w *taskStreamRecorder) WriteHeader(int)     {}
func (w *taskStreamRecorder) Write(b []byte) (int, error) {
	select {
	case w.frames <- string(b):
	default:
	}
	return len(b), nil
}
func (w *taskStreamRecorder) Flush() {}
func TestTasksStreamSnapshotsAndDisconnect(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	original := workspaceSessions
	workspaceSessions = m
	defer func() { workspaceSessions = original }()
	session, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	session.Status = "running"
	session.Turns = []RuntimeTurnRecord{{StartedAt: time.Now().Add(-time.Hour).UnixMilli(), ActivityAt: time.Now().UnixMilli(), ActivityText: "Working", RuntimeID: "fixture", StepsStarted: 2}}
	m.runs[session.ID] = &runtimeRun{session: session, cancel: func() {}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &taskStreamRecorder{header: http.Header{}, frames: make(chan string, 8)}
	done := make(chan bool, 1)
	go func() {
		handleTasksStream(w, httptest.NewRequest("GET", "https://loom.test/api/tasks/stream", nil).WithContext(ctx))
		done <- true
	}()
	select {
	case frame := <-w.frames:
		if !strings.Contains(frame, `"status":"running"`) || !strings.Contains(frame, `"steps_started":2`) {
			t.Fatal(frame)
		}
	case <-time.After(time.Second):
		t.Fatal("missing initial task snapshot")
	}
	m.mu.Lock()
	m.runs[session.ID].session.PendingRequests = []agent.AgentRequest{{ID: "pi:question", Kind: "user_input", Message: "Question"}}
	m.mu.Unlock()
	m.events.Publish(events.Event{Type: events.TaskWaiting})
	select {
	case frame := <-w.frames:
		if !strings.Contains(frame, `"status":"waiting_input"`) {
			t.Fatal(frame)
		}
	case <-time.After(time.Second):
		t.Fatal("no lifecycle snapshot")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task stream did not disconnect")
	}
	m.mu.Lock()
	running := m.runs[session.ID] != nil
	m.mu.Unlock()
	if !running {
		t.Fatal("task SSE disconnect stopped work")
	}
	if w.header.Get("Cache-Control") != "no-store" {
		t.Fatal("task snapshots cacheable")
	}
}

// These fixtures replace application globals. Drain the existing asynchronous
// Brain work before restoring them, just as testHome does before removing data.
func drainPhase4Jobs() {
	skillSinkJobs.Wait()
	brainSvcMu.Lock()
	b := brainSvc
	brainSvcMu.Unlock()
	if b != nil {
		b.cancelMemoryConsolidation()
		b.leaveJobs.Wait()
		transcriptJobs.Wait()
		b.writeRefresh.Wait()
	}
}
