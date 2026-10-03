package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBenchMixedQueueKeepsCloudOnControlPlane(t *testing.T) {
	benchTestWorkspace(t)
	var nodeCalls, cloudCalls, cleanup atomic.Int32
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer node-fixture" {
			t.Error("wrong node credential")
		}
		switch r.URL.Path {
		case "/api/bench/tests":
			fmt.Fprint(w, `{"ok":true,"test":{"id":"node-custom"}}`)
		case "/api/bench/tests/delete":
			cleanup.Add(1)
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/bench/queue":
			var request struct {
				Test   string      `json:"test_id"`
				Models []benchPick `json:"models"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.Test != "node-custom" || len(request.Models) != 1 || request.Models[0].ChoiceID != "" || request.Models[0].Model != "node-model.gguf" {
				t.Error("cloud choice or wrong local test sent to node")
			}
			fmt.Fprint(w, `{"ok":true,"scoped_cancel":true,"job":{"id":"node-job","status":"done","rows":[{"status":"ok","output":"Node answer","result":{"elapsed_sec":1.5,"prompt_per_second":100,"predicted_per_second":40}}]}}`)
		default:
			t.Errorf("unexpected node route %s", r.URL.Path)
		}
	}))
	defer node.Close()
	if err := setEngineNode(&engineNode{URL: node.URL, WebKey: "node-fixture"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(nil) })
	endpoint := benchTestServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer cloud-fixture" {
			t.Error("wrong cloud credential")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Cloud answer")+"data: [DONE]\n\n")
	})
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Fixture API", Endpoint: endpoint, Model: "cloud-model"}, "cloud-fixture")
	if err != nil {
		t.Fatal(err)
	}
	test, err := saveCustomBenchTest("Mixed", "Same test", 41)
	if err != nil {
		t.Fatal(err)
	}
	if engineRoutes["/api/bench/queue"] || engineRoutes["/api/bench/tests"] || engineRoutes["/api/bench/runs"] || engineRoutes["/api/bench/queue/cancel"] {
		t.Fatal("mixed queue still proxied to node")
	}
	if _, err = startBenchQueue(test.ID, []benchPick{{Model: "node-model.gguf"}, {ChoiceID: cloudChoiceID(p.ID, p.Model)}}, true); err != nil {
		t.Fatal(err)
	}
	j := awaitBenchQueue(t)
	if len(j.Rows) != 2 || j.Rows[0].Status != "ok" || j.Rows[1].Status != "ok" || j.Rows[0].Output != "Node answer" || j.Rows[1].Output != "Cloud answer" || cloudCalls.Load() != 1 || nodeCalls.Load() != 3 || cleanup.Load() != 1 {
		t.Fatalf("mixed results: %+v node=%d cloud=%d cleanup=%d", j, nodeCalls.Load(), cloudCalls.Load(), cleanup.Load())
	}
	raw, _ := json.Marshal(j)
	if strings.Contains(string(raw), "node-fixture") || strings.Contains(string(raw), "cloud-fixture") {
		t.Fatal("queue credential leak")
	}
}

func TestBenchNodeCancellationIsScopedAndLegacyIsNotCancelled(t *testing.T) {
	for _, scoped := range []bool{true, false} {
		t.Run(fmt.Sprint(scoped), func(t *testing.T) {
			var cancelled atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/bench/queue/cancel" {
					if r.Header.Get("X-Loom-Bench-Job") != "owned-job" {
						t.Error("unscoped cancellation")
					}
					cancelled.Add(1)
					fmt.Fprint(w, `{"ok":true}`)
					return
				}
				fmt.Fprintf(w, `{"ok":true,"scoped_cancel":%t,"job":{"id":"owned-job","status":"running","rows":[{"status":"running"}]}}`, scoped)
				if r.Method == http.MethodPost {
					w.(http.Flusher).Flush()
					go func() { time.Sleep(30 * time.Millisecond); cancel() }()
				}
			}))
			defer srv.Close()
			_, _, err := runNodeBenchTest(ctx, benchTest{ID: "perf", Kind: "perf"}, benchJobRow{Model: "fixture.gguf"}, engineNode{URL: srv.URL, WebKey: "fixture"})
			if err == nil || (cancelled.Load() == 1) != scoped {
				t.Fatalf("cancel=%d err=%v", cancelled.Load(), err)
			}
		})
	}
}

func TestBenchScopedCancelDoesNotStopAnotherJob(t *testing.T) {
	testHome(t)
	j := &benchJob{ID: "other-job", Status: "running"}
	saveBenchJob(j)
	benchStop.Store(false)
	r := httptest.NewRequest(http.MethodPost, "/api/bench/queue/cancel", nil)
	r.Header.Set("X-Loom-Bench-Job", "old-job")
	w := httptest.NewRecorder()
	handleBenchQueueCancel(w, r)
	if w.Code != 409 || benchStop.Load() || loadBenchJob().Status != "running" {
		t.Fatal("another node queue was cancelled")
	}
}

func TestBenchCompactPollDoesNotRepeatFullOutput(t *testing.T) {
	testHome(t)
	j := &benchJob{ID: "output-job", Status: "done", Rows: []benchJobRow{{Status: "ok", Output: "Full fixture output", Preview: "Short fixture"}}}
	saveBenchJob(j)
	archiveBenchJob(*j)
	for _, path := range []string{"/api/bench/queue?compact=1", "/api/bench/runs?compact=1"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if strings.Contains(path, "runs") {
			handleBenchRuns(w, r)
		} else {
			handleBenchQueue(w, r)
		}
		if strings.Contains(w.Body.String(), "Full fixture output") || !strings.Contains(w.Body.String(), "Short fixture") {
			t.Fatal("poll repeats full outputs")
		}
	}
	w := httptest.NewRecorder()
	handleBenchRuns(w, httptest.NewRequest(http.MethodGet, "/api/bench/runs?id=output-job", nil))
	if !strings.Contains(w.Body.String(), "Full fixture output") {
		t.Fatal("saved output lost")
	}
}

func TestBenchDirectServerUsesChosenModelWithoutChangingSelection(t *testing.T) {
	benchTestWorkspace(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model     string
			MaxTokens int `json:"max_tokens"`
			Messages  []Message
		}
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer native-fixture" || json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Model != "second-native-model" || payload.MaxTokens != 41 || len(payload.Messages) != 2 || payload.Messages[1].Content != "Same prompt" {
			t.Error("direct benchmark did not use its selected model and own key")
		}
		fmt.Fprint(w, `{"usage":{"prompt_tokens":5,"completion_tokens":8},"choices":[{"message":{"content":"Native server answer"}}]}`)
	}))
	defer srv.Close()
	n := &engineNode{Direct: true, Model: "first-native-model", V1: srv.URL, APIKey: "native-fixture"}
	if err := setEngineNode(n); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(nil) })
	test, err := saveCustomBenchTest("Direct", "Same prompt", 41)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startBenchQueue(test.ID, []benchPick{{Model: "second-native-model"}}); err != nil {
		t.Fatal(err)
	}
	j := awaitBenchQueue(t)
	if j.Rows[0].Status != "ok" || j.Rows[0].Output != "Native server answer" || j.Rows[0].Result.PromptPerSecond != nil || currentEngineNode().Model != "first-native-model" {
		t.Fatalf("direct result or persisted selection changed: %+v", j)
	}
}

func TestBenchRestartMarksUnfinishedQueueInterrupted(t *testing.T) {
	testHome(t)
	if benchBusy.Load() {
		t.Fatal("live fixture queue")
	}
	saveBenchJob(&benchJob{ID: "interrupted", Status: "running", Rows: []benchJobRow{{Status: "running"}, {Status: "pending"}, {Status: "ok", Output: "Retained"}}})
	w := httptest.NewRecorder()
	handleBenchQueue(w, httptest.NewRequest(http.MethodGet, "/api/bench/queue", nil))
	j := loadBenchJob()
	if j.Status != "cancel" || j.Finished == 0 || j.Rows[0].Status != "skip" || j.Rows[1].Status != "skip" || j.Rows[2].Output != "Retained" || len(loadBenchRuns()) != 1 {
		t.Fatal("interrupted queue remained running or lost results")
	}
}
