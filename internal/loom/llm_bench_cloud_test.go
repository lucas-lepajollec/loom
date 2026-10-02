package loom

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type benchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f benchRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type benchPipeWriter struct {
	io.Writer
	header http.Header
}

func (w benchPipeWriter) Header() http.Header { return w.header }
func (benchPipeWriter) WriteHeader(int)       {}
func (benchPipeWriter) Flush()                {}

// Exercise the very same SSE handler over TCP and, for restricted sandboxes,
// over a pipe transport. Production still uses the existing cloud HTTP client.
func benchTestServer(t *testing.T, sockets bool, handler http.HandlerFunc) string {
	t.Helper()
	if sockets {
		srv := httptest.NewServer(handler)
		t.Cleanup(func() {
			if benchBusy.Load() {
				cancelBenchQueue()
				awaitBenchQueue(t)
			}
			srv.Close()
		})
		return srv.URL + "/v1"
	}
	old := http.DefaultTransport
	http.DefaultTransport = benchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			handler(benchPipeWriter{Writer: writer, header: make(http.Header)}, r)
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: r}, nil
	})
	t.Cleanup(func() {
		if benchBusy.Load() {
			cancelBenchQueue()
			awaitBenchQueue(t)
		}
		http.DefaultTransport = old
	})
	return "http://127.0.0.1:12345/v1"
}

func benchTestWorkspace(t *testing.T) {
	t.Helper()
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	if benchBusy.Load() {
		t.Fatal("previous benchmark still running")
	}
	t.Cleanup(func() {
		if benchBusy.Load() {
			cancelBenchQueue()
			awaitBenchQueue(t)
		}
	})
}

func awaitBenchQueue(t *testing.T) *benchJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for benchBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if benchBusy.Load() {
		t.Fatal("benchmark did not finish")
	}
	return loadBenchJob()
}

func postBenchQueue(t *testing.T, testID string, picks []benchPick, consent bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"test_id": testID, "models": picks, "consent": consent})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleBenchQueue(w, httptest.NewRequest(http.MethodPost, "/api/bench/queue", strings.NewReader(string(body))))
	return w
}

func TestBenchCloudConsent(t *testing.T) {
	benchTestWorkspace(t)
	for _, consent := range []string{"", `,"consent":false`} {
		body := `{"models":[{"model":"local.gguf"},{"choice_id":"cloud:unknown","name":"Cloud"}]` + consent + `}`
		w := httptest.NewRecorder()
		handleBenchQueue(w, httptest.NewRequest(http.MethodPost, "/api/bench/queue", strings.NewReader(body)))
		var response struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || w.Code != 400 || response.OK || response.Error == "" {
			t.Fatalf("consent response: %d %s", w.Code, w.Body.String())
		}
		if benchBusy.Load() || loadBenchJob() != nil || len(loadBenchRuns()) != 0 {
			t.Fatal("queue started before consent")
		}
	}
	if _, err := startBenchQueue(benchTestPerf, []benchPick{{ChoiceID: "cloud:unknown"}}); err == nil {
		t.Fatal("internal caller bypassed consent")
	}
}

func TestBenchCloudMissingKey(t *testing.T) {
	benchTestWorkspace(t)
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Fixture", Endpoint: "https://example.com/v1", Model: "fixture"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	old := http.DefaultTransport
	http.DefaultTransport = benchRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected external request")
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	w := postBenchQueue(t, benchTestPerf, []benchPick{{ChoiceID: cloudChoiceID(p.ID, p.Model)}, {ChoiceID: "cloud:missing"}}, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	j := awaitBenchQueue(t)
	if j.Status != "done" || j.Index != 1 || j.Rows[0].Status != "err" || !strings.Contains(j.Rows[0].Error, "missing key") || j.Rows[1].Status != "err" || !strings.Contains(j.Rows[1].Error, "not found") || calls.Load() != 0 {
		t.Fatalf("missing provider/key: %+v", j)
	}
}

func TestBenchCloudQueueSSE(t *testing.T)      { exerciseBenchCloudQueue(t, true) }
func TestBenchCloudQueueInMemory(t *testing.T) { exerciseBenchCloudQueue(t, false) }

func exerciseBenchCloudQueue(t *testing.T, sockets bool) {
	for _, tc := range []struct {
		name, usage              string
		perf, completion, prompt bool
	}{
		{"custom", `{"prompt_tokens":7,"completion_tokens":12,"total_tokens":19}`, false, true, true},
		{"perf", `{"prompt_tokens":2000,"completion_tokens":12}`, true, true, true},
		{"no usage", "", false, false, false},
		{"prompt only", `{"prompt_tokens":7}`, false, false, true},
		{"completion only", `{"completion_tokens":12}`, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			benchTestWorkspace(t)
			prompt, maxTokens := "Écris une phrase.", 41
			if tc.perf {
				prompt, maxTokens = benchCorpusPrompt(2000), 300
			}
			var calls atomic.Int32
			endpoint := benchTestServer(t, sockets, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var req struct {
					Model         string    `json:"model"`
					Stream        bool      `json:"stream"`
					MaxTokens     int       `json:"max_tokens"`
					Messages      []Message `json:"messages"`
					StreamOptions struct {
						IncludeUsage bool `json:"include_usage"`
					} `json:"stream_options"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "second" || !req.Stream || req.MaxTokens != maxTokens || !req.StreamOptions.IncludeUsage || len(req.Messages) != 1 || req.Messages[0].Content != prompt || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Errorf("invalid cloud bench request: %+v", req)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n")
				w.(http.Flusher).Flush()
				time.Sleep(25 * time.Millisecond)
				fmt.Fprint(w, sseChunk(" Bonjour "))
				w.(http.Flusher).Flush()
				time.Sleep(25 * time.Millisecond)
				fmt.Fprint(w, sseChunk("le monde."))
				if tc.usage != "" {
					fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":%s}\n\n", tc.usage)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
			})
			p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Fixture", Endpoint: endpoint, Model: "first", Models: []string{"second"}}, "fixture-secret")
			if err != nil {
				t.Fatal(err)
			}
			testID := benchTestPerf
			if !tc.perf {
				custom, err := saveCustomBenchTest("Texte", prompt, maxTokens)
				if err != nil {
					t.Fatal(err)
				}
				testID = custom.ID
			}
			before := ReadConfig()
			pick := benchPick{ChoiceID: cloudChoiceID(p.ID, "second"), Name: "Cloud", Model: "ignore.gguf", Preset: "ignore"}
			picks := []benchPick{pick, pick}
			rowIndex := 0
			if tc.name == "custom" {
				// An unavailable destination must not prevent the next row from running.
				picks = append([]benchPick{{ChoiceID: "cloud:unavailable"}}, picks...)
				rowIndex = 1
			}
			w := postBenchQueue(t, testID, picks, true)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			j := awaitBenchQueue(t)
			if j.Status != "done" || len(j.Rows) != rowIndex+1 || calls.Load() != 1 {
				t.Fatalf("queue: %+v", j)
			}
			if rowIndex > 0 && j.Rows[0].Status != "err" {
				t.Fatal("unavailable cloud row did not report an error")
			}
			row := j.Rows[rowIndex]
			res := row.Result
			if row.Status != "ok" || row.Kind != "cloud" || row.Provider != "Fixture" || row.Model != "second" || row.Name != "Cloud" || row.Preview != "Bonjour le monde." || res == nil {
				t.Fatalf("row: %+v", row)
			}
			if res.PromptPerSecond != nil || res.TTFT == nil || *res.TTFT < .020 || res.Elapsed <= *res.TTFT || (res.CompletionTokens != nil) != tc.completion || (res.PromptTokens != nil) != tc.prompt || (res.PredictedPerSec != nil) != tc.completion {
				t.Fatalf("metrics: %+v %+v", res, res.BenchCloudMetrics)
			}
			if tc.completion && (*res.CompletionTokens != 12 || math.Abs(*res.PredictedPerSec-12/(res.Elapsed-*res.TTFT)) > 1e-6) {
				t.Fatal("wrong reported throughput")
			}
			if fmt.Sprint(ReadConfig()) != fmt.Sprint(before) || loadLastBench() != nil || len(loadBenchStore()) != 0 {
				t.Fatal("cloud bench changed local state")
			}
			encoded, _ := json.Marshal(loadBenchRuns())
			if strings.Contains(string(encoded), "fixture-secret") || !strings.Contains(string(encoded), `"prompt_per_second":null`) {
				t.Fatal("invalid persisted cloud metrics or credential leak")
			}
			if !tc.completion && !strings.Contains(string(encoded), `"predicted_per_second":null`) {
				t.Fatal("missing throughput estimated as zero")
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		benchTestWorkspace(t)
		started, cancelled := make(chan struct{}), make(chan struct{})
		endpoint := benchTestServer(t, sockets, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseChunk("Partial"))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			close(cancelled)
		})
		p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Cancel", Endpoint: endpoint, Model: "fixture"}, "fixture-secret")
		if err != nil {
			t.Fatal(err)
		}
		if w := postBenchQueue(t, benchTestPerf, []benchPick{{ChoiceID: cloudChoiceID(p.ID, p.Model)}, {Model: "must-not-load.gguf"}}, true); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("request did not start")
		}
		w := httptest.NewRecorder()
		handleBenchQueueCancel(w, httptest.NewRequest(http.MethodPost, "/api/bench/queue/cancel", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("cloud context was not cancelled")
		}
		j := awaitBenchQueue(t)
		if j.Status != "cancel" || j.Rows[0].Status != "skip" || j.Rows[1].Status != "skip" {
			t.Fatalf("cancelled queue: %+v", j)
		}
	})
}

func TestBenchLocalMetricsUnchanged(t *testing.T) {
	benchTestWorkspace(t)
	old := http.DefaultTransport
	http.DefaultTransport = benchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			if json.NewDecoder(r.Body).Decode(&req) != nil || req["stream"] != false || req["cache_prompt"] != false || req["max_tokens"] != float64(32) {
				t.Error("local request changed")
			}
			body = `{"timings":{"prompt_n":7,"prompt_ms":10,"prompt_per_second":700,"predicted_n":3,"predicted_ms":20,"predicted_per_second":150},"choices":[{"message":{"content":"Local preview"}}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	if err := SetConfigKey("MODEL", "local.gguf"); err != nil {
		t.Fatal(err)
	}
	test, err := saveCustomBenchTest("Local", "Local prompt", 32)
	if err != nil {
		t.Fatal(err)
	}
	w := postBenchQueue(t, test.ID, []benchPick{{Model: "local.gguf"}}, false)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	j := awaitBenchQueue(t)
	r := j.Rows[0]
	if r.Status != "ok" || r.Kind != "local" || r.Provider != "" || r.Preview != "Local preview" || r.Result == nil || r.Result.PromptPerSecond == nil || *r.Result.PromptPerSecond != 700 || *r.Result.PredictedPerSec != 150 || r.Result.BenchCloudMetrics != nil {
		t.Fatalf("local row: %+v", r)
	}
	encoded, _ := json.Marshal(r.Result)
	if strings.Contains(string(encoded), "ttft_sec") || strings.Contains(string(encoded), "completion_tokens") {
		t.Fatal("cloud fields added to local metrics")
	}
}
