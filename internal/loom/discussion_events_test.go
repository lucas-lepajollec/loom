package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativeDiscussionEventMapping(t *testing.T) {
	rt := RuntimeTurnRecord{RuntimeID: "llama.cpp", Model: "local"}
	cases := []struct {
		delta map[string]any
		kind  string
		field string
		value any
	}{
		{map[string]any{"user": "question", "files": []string{"a"}}, "turn_start", "text", "question"},
		{map[string]any{"content": "<script>"}, "text_delta", "text", "<script>"},
		{map[string]any{"reasoning_content": "trace"}, "reasoning_delta", "text", "trace"},
		{map[string]any{"drop_reasoning": true}, "reasoning_delta", "drop", true},
		{map[string]any{"tool_used": map[string]any{"name": "read"}}, "tool_start", "", nil},
		{map[string]any{"tool_used": map[string]any{"typing": true}}, "tool_delta", "", nil},
		{map[string]any{"tool_used": map[string]any{"done": true}}, "tool_end", "", nil},
		{map[string]any{"stats": &StatsEvent{GenTokens: 12}}, "usage", "", nil},
		{map[string]any{"error": "failure"}, "error", "error", "failure"},
		{map[string]any{"turn_done": true, "runtime_turn": rt}, "turn_done", "provenance", rt},
	}
	for _, tc := range cases {
		tc.delta["seq"], tc.delta["replace"], tc.delta["portable_text"], tc.delta["toks"] = 17, true, true, 12
		got := nativeDiscussionEvents(tc.delta)
		if len(got) != 1 || got[0]["type"] != tc.kind {
			t.Fatalf("mapping %v = %v", tc.delta, got)
		}
		if tc.field != "" && !reflect.DeepEqual(got[0][tc.field], tc.value) {
			t.Fatalf("lost payload: %v", got)
		}
		if got[0]["seq"] != 17 || got[0]["replace"] != true || got[0]["portable_text"] != true || got[0]["toks"] != 12 {
			t.Fatalf("lost replay metadata: %v", got)
		}
	}
	control := map[string]any{"reset": true, "replay": true, "ctx_used": 40, "compacted": true}
	if got := nativeDiscussionEvents(control); len(got) != 1 || !reflect.DeepEqual(map[string]any(got[0]), control) {
		t.Fatalf("control: %v", got)
	}
}

func TestRuntimeDiscussionEventMappingIsHonest(t *testing.T) {
	rt := RuntimeTurnRecord{RuntimeID: "antigravity", Model: "native"}
	if got := runtimeDiscussionEvents(StreamEvent{Reasoning: "unreported", NativeSessionID: "private", DurationSeconds: 2}, rt); len(got) != 0 {
		t.Fatalf("invented events: %v", got)
	}
	got := runtimeDiscussionEvents(StreamEvent{Content: "answer"}, rt)
	if len(got) != 1 || got[0]["text"] != "answer" || got[0]["toks"] != nil || got[0]["usage"] != nil {
		t.Fatalf("invented token count: %v", got)
	}
	rt.RuntimeID = "codex"
	got = runtimeDiscussionEvents(StreamEvent{Reasoning: "reported summary"}, rt)
	if got[0]["type"] != "reasoning_delta" || got[0]["summary"] != true {
		t.Fatal(got)
	}
	for state, kind := range map[string]string{"ACTIVE": "tool_start", "DONE": "tool_end", "UNKNOWN": "tool_delta"} {
		h := HarnessEvent{Index: 1, Name: "write_to_file", State: state, Failed: true, FileTarget: "target"}
		got = runtimeDiscussionEvents(StreamEvent{HarnessEvent: &h}, rt)
		if got[0]["type"] != kind || got[0]["native_tool"] != h || got[0]["tool"] != nil {
			t.Fatalf("tool metadata: %v", got)
		}
	}
}

type discussionFakeRuntime struct {
	run func(context.Context, ChatCallback) error
}

func (discussionFakeRuntime) Descriptor() RuntimeDescriptor { return RuntimeDescriptor{ID: "codex"} }
func (a discussionFakeRuntime) Run(ctx context.Context, _ RuntimeTurn, emit ChatCallback) ([]Message, error) {
	return nil, a.run(ctx, emit)
}

func TestDiscussionSnapshotThenLiveAndReconnect(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s := RuntimeSession{ID: "events", RuntimeID: "codex", Model: "native", Status: "running", Messages: []Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: ""}}, Turns: []RuntimeTurnRecord{{MessageIndex: 1, RuntimeID: "codex", Model: "native", StartedAt: 123}}}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &runtimeRun{session: s, cancel: func() {}}
	m.runs[s.ID] = run
	events := make(chan DiscussionEvent, 32)
	caught := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.subscribeDiscussion(ctx, s.ID, func(e DiscussionEvent) bool {
			events <- e
			if e["caught_up"] == true {
				close(caught)
			}
			return e["type"] != "turn_done"
		})
	}()
	select {
	case <-caught:
	case <-time.After(time.Second):
		t.Fatal("snapshot timeout")
	}
	adapter := discussionFakeRuntime{run: func(_ context.Context, emit ChatCallback) error {
		emit(StreamEvent{Content: "partial"})
		emit(StreamEvent{Reasoning: "summary"})
		emit(StreamEvent{Usage: &RuntimeUsage{Input: 9, Output: 3, Total: 12}})
		emit(StreamEvent{NativeSessionID: "native-id", DurationSeconds: 1.5})
		return context.Canceled
	}}
	m.generate(context.Background(), run, adapter, nil)
	<-done
	var live []DiscussionEvent
	for len(events) > 0 {
		live = append(live, <-events)
	}
	var kinds []string
	for _, e := range live {
		if k, ok := e["type"].(string); ok {
			kinds = append(kinds, k)
		}
	}
	want := []string{"turn_start", "text_delta", "text_delta", "reasoning_delta", "usage", "error", "turn_done"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("events: %v", kinds)
	}
	saved, ok := m.get(s.ID)
	if !ok || saved.Status != "cancelled" || saved.Messages[1].Content != "partial" {
		t.Fatalf("lost cancellation/text: %+v", saved)
	}
	rt := saved.Turns[0]
	if rt.NativeSessionID != "native-id" || rt.DurationSeconds != 1.5 || rt.Usage.Output != 3 || rt.ReasoningSummary != "summary" {
		t.Fatalf("lost provenance: %+v", rt)
	}
	replay := runtimeReplay(saved)
	if replay[len(replay)-1]["caught_up"] != true {
		t.Fatal("missing replay boundary")
	}
	for _, e := range replay {
		if e["type"] == "turn_done" {
			if !reflect.DeepEqual(e["provenance"], &rt) {
				t.Fatalf("changed replay provenance: %v", e)
			}
		}
	}
	if strings.Contains(fmt.Sprint(saved.Messages), "summary") {
		t.Fatal("reasoning leaked into portable transcript")
	}
}

func TestDiscussionSlowSubscriberDoesNotBlockRuntime(t *testing.T) {
	m := newRuntimeSessions()
	sub := &discussionSubscriber{events: make(chan DiscussionEvent, 1)}
	m.subscribers["id"] = map[*discussionSubscriber]bool{sub: true}
	m.publishLocked("id", DiscussionEvent{"type": "text_delta"}, DiscussionEvent{"type": "turn_done"})
	if len(m.subscribers["id"]) != 0 {
		t.Fatal("slow reader retained")
	}
	<-sub.events
	if _, open := <-sub.events; open {
		t.Fatal("slow reader not disconnected")
	}
}

func readDiscussionSSE(t *testing.T, scanner *bufio.Scanner) DiscussionEvent {
	t.Helper()
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var wire struct {
			Choices []struct {
				Delta DiscussionEvent `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &wire); err != nil {
			t.Fatal(err)
		}
		return wire.Choices[0].Delta
	}
	t.Fatalf("SSE closed: %v", scanner.Err())
	return nil
}

type discussionTransport func(*http.Request) (*http.Response, error)

func (f discussionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type discussionPipeWriter struct {
	header http.Header
	writer *io.PipeWriter
	ready  chan struct{}
	once   sync.Once
	status int
}

func (w *discussionPipeWriter) Header() http.Header { return w.header }
func (w *discussionPipeWriter) WriteHeader(status int) {
	w.once.Do(func() { w.status = status; close(w.ready) })
}
func (w *discussionPipeWriter) Write(b []byte) (int, error) {
	w.WriteHeader(200)
	return w.writer.Write(b)
}
func (w *discussionPipeWriter) Flush() {}

func TestDiscussionCloudSSEDoesNotResendOnReconnect(t *testing.T) {
	testHome(t)
	requests := 0
	oldTransport := http.DefaultTransport
	http.DefaultTransport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != "https://fixture.invalid/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("wrong destination or credential")
		}
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"<b>answer</b>\"}}]}\n\ndata: [DONE]\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	old := workspaceSessions
	m := newRuntimeSessions()
	workspaceSessions = m
	t.Cleanup(func() { workspaceSessions = old })
	p, err := m.saveProvider(CloudProvider{Name: "fixture", Endpoint: "https://fixture.invalid/v1", Model: "model"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.create("", p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	connect := func() (*bufio.Scanner, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		reader, writer := io.Pipe()
		w := &discussionPipeWriter{header: make(http.Header), writer: writer, ready: make(chan struct{})}
		req := httptest.NewRequest(http.MethodPost, "/api/discussion/events", strings.NewReader(`{"id":"`+s.ID+`"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		done := make(chan struct{})
		go func() { defer close(done); defer writer.Close(); handleDiscussionEvents(w, req) }()
		select {
		case <-w.ready:
		case <-time.After(time.Second):
			cancel()
			reader.Close()
			t.Fatal("SSE header timeout")
		}
		if w.status != 200 || !strings.Contains(w.header.Get("Cache-Control"), "no-store") {
			t.Fatalf("response: %d", w.status)
		}
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 4096), 1<<20)
		return scan, func() {
			cancel()
			reader.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("subscription did not close")
			}
		}
	}
	scan, closeStream := connect()
	for readDiscussionSSE(t, scan)["caught_up"] != true {
	}
	if err := m.start(s.ID, "request-events", "question"); err != nil {
		closeStream()
		t.Fatal(err)
	}
	var text string
	for {
		e := readDiscussionSSE(t, scan)
		if e["type"] == "text_delta" {
			text += e["text"].(string)
		}
		if e["type"] == "turn_done" {
			break
		}
	}
	closeStream()
	if text != "<b>answer</b>" {
		t.Fatalf("text changed: %q", text)
	}
	scan, closeStream = connect()
	defer closeStream()
	for readDiscussionSSE(t, scan)["caught_up"] != true {
	}
	if requests != 1 {
		t.Fatalf("reconnect generated %d requests", requests)
	}
	if err := m.start(s.ID, "request-events", "question"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("retry resubmitted generation")
	}
}

func TestDiscussionEventsRejectUnknownAndWrongMethod(t *testing.T) {
	testHome(t)
	for _, tc := range []struct {
		method, body string
		status       int
	}{{"GET", "{}", 405}, {"POST", `{"id":"missing"}`, 404}, {"POST", `{"extra":true}`, 400}} {
		req := httptest.NewRequest(tc.method, "/api/discussion/events", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleDiscussionEvents(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.body, w.Code)
		}
	}
}

func TestDiscussionNativeSSEKeepsReplayAndMetrics(t *testing.T) {
	testHome(t)
	old := conv
	conv = newTestConv()
	t.Cleanup(func() { conv = old })
	rt := RuntimeTurnRecord{RuntimeID: "llama.cpp", Model: "local"}
	conv.appendDelta(conv.epoch, map[string]any{"user": "question"})
	conv.appendDelta(conv.epoch, map[string]any{"content": "imported <b>", "portable_text": true})
	conv.appendDelta(conv.epoch, map[string]any{"stats": &StatsEvent{GenTokens: 4, GenPerSecond: 12}})
	conv.appendDelta(conv.epoch, map[string]any{"turn_done": true, "runtime_turn": rt, "elapsed_ms": 150})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/api/discussion/events", strings.NewReader(`{}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	reader, writer := io.Pipe()
	w := &discussionPipeWriter{header: make(http.Header), writer: writer, ready: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); defer writer.Close(); handleDiscussionEvents(w, req) }()
	defer func() { cancel(); reader.Close(); <-done }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var kinds []string
	for {
		event := readDiscussionSSE(t, scanner)
		if event["caught_up"] == true {
			break
		}
		if kind, ok := event["type"].(string); ok {
			kinds = append(kinds, kind)
		}
		if event["type"] == "text_delta" && event["portable_text"] != true {
			t.Fatal("portable text became executable")
		}
		if event["type"] == "turn_done" {
			metrics := event["metrics"].(map[string]any)
			stats := metrics["stats"].(map[string]any)
			if metrics["elapsed_ms"] != float64(150) || stats["gen_per_second"] != float64(12) {
				t.Fatalf("lost metrics: %v", metrics)
			}
			if event["provenance"].(map[string]any)["model"] != "local" {
				t.Fatal("lost provenance")
			}
		}
	}
	if !reflect.DeepEqual(kinds, []string{"turn_start", "text_delta", "usage", "turn_done"}) {
		t.Fatal(kinds)
	}
}
