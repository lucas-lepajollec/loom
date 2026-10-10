package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
	retrieval "github.com/lucas-lepajollec/loom/tools/brain-retrieval"
)

type retrievalNoNetwork struct{ t *testing.T }

func (r retrievalNoNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Error("offline retrieval benchmark attempted network/model access")
	return nil, errors.New("offline benchmark forbids network")
}

func retrievalHome(t *testing.T) *brainService {
	t.Helper()
	brainSvcMu.Lock()
	originalBrain := brainSvc
	brainSvcMu.Unlock()
	t.Cleanup(func() { brainSvcMu.Lock(); brainSvc = originalBrain; brainSvcMu.Unlock() })
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "CODEX_HOME", "PI_CODING_AGENT_DIR", "HERMES_HOME", "DSH_HOME"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasSuffix(key, "_API_KEY") || strings.HasSuffix(key, "_TOKEN") || key == "API_KEY" || key == "ANTHROPIC_AUTH_TOKEN" {
			t.Setenv(key, "")
		}
	}
	service := continuityBase(t)
	brainSvcMu.Lock()
	old := brainSvc
	brainSvc = newBrainService(LoomHome())
	service = brainSvc
	brainSvcMu.Unlock()
	t.Cleanup(func() {
		service.cancelMemoryConsolidation()
		service.leaveJobs.Wait()
		transcriptJobs.Wait()
		service.writeRefresh.Wait()
		brainSvcMu.Lock()
		brainSvc = old
		brainSvcMu.Unlock()
	})
	oldTransport, oldClient := http.DefaultTransport, brainModelClient
	http.DefaultTransport = retrievalNoNetwork{t}
	brainModelClient = &http.Client{Transport: retrievalNoNetwork{t}}
	t.Cleanup(func() { http.DefaultTransport = oldTransport; brainModelClient = oldClient })
	if err := service.storage.write("semantic.json", []byte(`{"model":"nomic","enabled":false,"auto_index":false}`)); err != nil {
		t.Fatal(err)
	}
	return service
}

// capturePrepared exercises production dispatch to an injected adapter. It
// deliberately bypasses model readiness/credential checks, not preparation.
func capturePrepared(t *testing.T, s RuntimeSession, p DiscussionPreview) []Message {
	t.Helper()
	var received []Message
	adapter := memoryContextAdapter{id: "benchmark-capture", run: func(turn RuntimeTurn, _ ChatCallback) { received = append([]Message(nil), turn.Messages...) }}
	s.Messages = append(s.Messages, um("benchmark draft"), am(""))
	s.Turns = []RuntimeTurnRecord{{MessageIndex: len(s.Messages) - 1}}
	s.FrozenSnapshot = p.Context.Snapshot
	ctx, cancel := context.WithCancel(context.Background())
	run := &runtimeRun{session: s, cancel: cancel}
	m := newRuntimeSessions()
	m.runs[s.ID] = run
	m.generate(ctx, run, adapter, p.Messages, p.Context)
	if run.session.Error != "" {
		t.Fatal(run.session.Error)
	}
	return received
}

func retrievalMessages(messages []Message) string {
	var text strings.Builder
	for _, msg := range messages {
		text.WriteString(msgText(msg))
		text.WriteByte('\n')
	}
	return text.String()
}
func retrievalTokens(messages []Message) int {
	n := 0
	for _, msg := range messages {
		n += brain.Tokens(msgText(msg))
	}
	return n
}

func TestBrainRetrievalFinalContext(t *testing.T) {
	dir := retrieval.FixtureDir("brain/testdata/retrieval")
	corpus, err := retrieval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	report := retrieval.NewReport(corpus)
	for _, c := range corpus.Cases {
		for _, route := range []string{"local", "cloud", "harness"} {
			for _, budget := range retrieval.Budgets {
				t.Run(c.ID+"/"+route+"/"+stringBudget(budget), func(t *testing.T) {
					service := retrievalHome(t)
					e, docs, index, err := retrieval.Build(t.TempDir(), c)
					if err != nil {
						t.Fatal(err)
					}
					service.engine = e
					var memory brain.MemoryFile
					if c.MemoryBefore != "" {
						memory, err = service.MemoryWrite(brain.MemoryWrite{Scope: "global", Name: "Compass preference", Description: "Compass orientation", Type: "user", Text: c.MemoryBefore})
						if err != nil {
							t.Fatal(err)
						}
						service.writeRefresh.Wait()
					}
					p, err := saveProjectContext(ChatProject{Name: "Benchmark", BrainBudget: budget})
					if err != nil {
						t.Fatal(err)
					}
					for _, d := range docs {
						if d.Source != "conversations" {
							continue
						}
						id := strings.Split(strings.TrimPrefix(d.Path, "discussion/"), "/")[0]
						allowed := true
						if prefixes, ok := c.Paths[d.Source]; ok {
							allowed = false
							for _, prefix := range prefixes {
								if strings.HasPrefix(d.Path, prefix+"/") {
									allowed = true
								}
							}
						}
						if allowed {
							if err = putStoreJSON(bkRuntimeSessions, id, RuntimeSession{ID: id, ProjectID: p.ID}); err != nil {
								t.Fatal(err)
							}
						}
					}
					row, all, _, err := retrieval.Measure(e, c, corpus.Evidence[c.ID], docs, budget, index)
					if err != nil {
						t.Fatal(err)
					}
					row.Route = route
					runtimeID := map[string]string{"local": "llama.cpp", "cloud": "openai-compatible", "harness": "benchmark-harness"}[route]
					isolateRuntimeRegistry(t, memoryContextAdapter{id: "benchmark-harness", run: func(RuntimeTurn, ChatCallback) { t.Fatal("unexpected real harness invocation") }})
					s := RuntimeSession{ID: "bench-turn", ProjectID: p.ID, RuntimeID: runtimeID, Title: "Benchmark"}
					if route == "local" {
						s.PortableMessages = []Message{}
					}
					var prepared DiscussionPreview
					times := []time.Duration{}
					for i := 0; i <= retrieval.Repetitions; i++ {
						start := time.Now()
						if route == "local" {
							conversation := &Conversation{ID: s.ID}
							conversation.cond = sync.NewCond(&conversation.mu)
							assembled, err := conversation.captureDiscussionContext(conversation.epoch, s, c.Query)
							if err != nil {
								t.Fatal(err)
							}
							prepared = discussion.PrepareDiscussion(s, c.Query, assembled)
						} else {
							prepared = prepareDiscussion(s, c.Query)
						}
						elapsed := time.Since(start)
						if i > 0 {
							times = append(times, elapsed)
						}
					}
					if prepared.Problem != "" {
						t.Fatal(prepared.Problem)
					}
					row.AssemblyLatency = retrieval.Percentiles(times)
					assembled := retrieval.Present(corpus.Evidence[c.ID].Spans, docs, all, prepared.Context.System+"\n"+prepared.Context.Extras)
					row.Assembled = &assembled
					received := capturePrepared(t, s, prepared)
					final := retrieval.Present(corpus.Evidence[c.ID].Spans, docs, all, retrievalMessages(received))
					row.Final = &final
					if c.ID == "source-scope" && strings.Contains(retrievalMessages(received), "OUTSIDE_SCOPE") {
						row.ScopeLeaks++
					}
					row.InjectionTokens = 0
					for _, item := range prepared.Context.Items {
						if item.Kind == "brain_passage" || item.Kind == "memory" {
							row.InjectionTokens += item.Tokens
						}
					}
					row.PreparedTokens = retrievalTokens(received)
					if final.Complete != nil && !*final.Complete {
						switch {
						case row.Retrieved.Complete != nil && !*row.Retrieved.Complete:
							row.MissingStage = "retrieval"
						case row.Packed.Complete != nil && !*row.Packed.Complete:
							row.MissingStage = "packing-budget-or-dedup"
						case !*assembled.Complete:
							row.MissingStage = "assembly-policy-or-frozen-context"
						default:
							row.MissingStage = "portable-window-or-dispatch"
						}
					}
					report.Rows = append(report.Rows, row)
					if c.MemoryAfter != "" {
						s.FrozenSnapshot = prepared.Context.Snapshot
						s.Messages = []Message{um(c.Query), am("synthetic previous response")}
						if route == "local" {
							s.PortableMessages = append([]Message{}, s.Messages...)
							s.FrozenSnapshot = discussion.TrackFrozenPortable(s.FrozenSnapshot, s.PortableMessages)
						}
						if _, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", File: memory.File, Name: memory.Name, Description: memory.Description, Type: memory.Type, Text: c.MemoryAfter}); err != nil {
							t.Fatal(err)
						}
						service.writeRefresh.Wait()
						for _, stage := range []string{"before-refresh", "after-refresh"} {
							if stage == "after-refresh" {
								m := newRuntimeSessions()
								if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
									t.Fatal(err)
								}
								s, err = m.refreshContext(s.ID)
								if err != nil {
									t.Fatal(err)
								}
							}
							var next DiscussionPreview
							durations := []time.Duration{}
							for i := 0; i <= retrieval.Repetitions; i++ {
								start := time.Now()
								next = prepareDiscussion(s, c.Query)
								elapsed := time.Since(start)
								if i > 0 {
									durations = append(durations, elapsed)
								}
							}
							got := capturePrepared(t, s, next)
							// Frozen ContextItem.Text is deliberately not serialized;
							// inspect the actual sent system message. In this fixture
							// only Markdown memory supplies the compass support there.
							var authoritative strings.Builder
							for _, msg := range got {
								if msg.Role == "system" {
									authoritative.WriteString(msgText(msg))
								}
							}
							observation := retrieval.FrozenUpdate{Route: route, Budget: budget, Stage: stage, CurrentPresent: strings.Contains(authoritative.String(), c.MemoryAfter), StaleAuthoritativeMemory: strings.Contains(authoritative.String(), c.MemoryBefore), PreparedTokens: retrievalTokens(got), AssemblyLatency: retrieval.Percentiles(durations)}
							if observation.CurrentPresent != (stage == "after-refresh") || observation.StaleAuthoritativeMemory != (stage == "before-refresh") {
								t.Fatalf("memory update/refresh changed freeze contract: %+v; authoritative=%q; system=%q", observation, authoritative.String(), next.Context.System)
							}
							report.FrozenUpdates = append(report.FrozenUpdates, observation)
						}
					}
				})
			}
		}
	}
	t.Run("portable-window-loss", func(t *testing.T) {
		retrievalHome(t)
		var c retrieval.Case
		for _, candidate := range corpus.Cases {
			if candidate.ID == "budget-drop" {
				c = candidate
				break
			}
		}
		e, docs, _, err := retrieval.Build(t.TempDir(), c)
		if err != nil {
			t.Fatal(err)
		}
		all, err := e.Chunks(brain.SearchRequest{})
		if err != nil {
			t.Fatal(err)
		}
		s := RuntimeSession{ID: "window-benchmark", RuntimeID: "openai-compatible", Messages: []Message{um(c.Documents[0].Text), am(strings.Repeat("x", maxPortableBytes))}}
		before := retrieval.Present(corpus.Evidence[c.ID].Spans, docs, all, retrievalMessages(s.Messages))
		var prepared DiscussionPreview
		durations := []time.Duration{}
		for i := 0; i <= retrieval.Repetitions; i++ {
			start := time.Now()
			prepared = prepareDiscussion(s, "latest")
			elapsed := time.Since(start)
			if i > 0 {
				durations = append(durations, elapsed)
			}
		}
		if prepared.Problem != "" {
			t.Fatal(prepared.Problem)
		}
		received := capturePrepared(t, s, prepared)
		final := retrieval.Present(corpus.Evidence[c.ID].Spans, docs, all, retrievalMessages(received))
		if before.Covered != 1 || final.Covered != 0 || prepared.Omitted == 0 {
			t.Fatal("portable window did not expose full-span loss")
		}
		report.WindowFit = &retrieval.WindowObservation{CaseID: c.ID, HistoryBeforeFit: before, Final: final, Omitted: prepared.Omitted, PreparedTokens: retrievalTokens(received), AssemblyLatency: retrieval.Percentiles(durations)}
	})
	if err = report.Write(os.Getenv("LOOM_BRAIN_RETRIEVAL_OUT")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("brain/testdata/retrieval/baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline retrieval.Baseline
	if err = json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LOOM_BRAIN_RETRIEVAL_CASES") == "" {
		if err = retrieval.Gate(report, baseline); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(report.Summary())
}

func stringBudget(n int) string { data, _ := json.Marshal(n); return string(data) }

func TestBrainRetrievalFrozenRefreshAndWindow(t *testing.T) {
	service := retrievalHome(t)
	item, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", Name: "Compass preference", Description: "Compass orientation", Type: "user", Text: "The frozen compass points toward North."})
	if err != nil {
		t.Fatal(err)
	}
	service.writeRefresh.Wait()
	for _, runtimeID := range []string{"llama.cpp", "openai-compatible", "benchmark-harness"} {
		s := RuntimeSession{ID: runtimeID, RuntimeID: runtimeID, Title: "Frozen benchmark"}
		first := prepareDiscussion(s, "frozen compass")
		if !strings.Contains(first.Context.System, item.Text) {
			t.Fatal("initial supporting memory absent")
		}
		s.FrozenSnapshot = first.Context.Snapshot
		updated := "The frozen compass points toward South."
		if _, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", File: item.File, Name: item.Name, Description: item.Description, Type: item.Type, Text: updated}); err != nil {
			t.Fatal(err)
		}
		service.writeRefresh.Wait()
		before := prepareDiscussion(s, "frozen compass")
		if !strings.Contains(before.Context.System, item.Text) || strings.Contains(before.Context.System, updated) {
			t.Fatal("mutable edit silently changed freeze contract")
		}
		m := newRuntimeSessions()
		if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
			t.Fatal(err)
		}
		s, err := m.refreshContext(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		after := prepareDiscussion(s, "frozen compass")
		received := capturePrepared(t, s, after)
		if !strings.Contains(retrievalMessages(received), updated) || strings.Contains(after.Context.System, item.Text) {
			t.Fatal("explicit refresh did not update sent context")
		}
		if _, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", File: item.File, Name: item.Name, Description: item.Description, Type: item.Type, Text: item.Text}); err != nil {
			t.Fatal(err)
		}
		service.writeRefresh.Wait()
	}
	// A full supporting historical message lost during real window fitting must
	// not count just because it was present before PrepareDiscussion.
	s := RuntimeSession{ID: "window", RuntimeID: "openai-compatible", Messages: []Message{um("WINDOW_EVIDENCE"), am(strings.Repeat("x", maxPortableBytes))}}
	p := discussion.PrepareDiscussion(s, "latest", DiscussionContext{})
	if p.Omitted == 0 || strings.Contains(retrievalMessages(capturePrepared(t, s, p)), "WINDOW_EVIDENCE") {
		t.Fatal("window-fit evidence loss was not observed")
	}
}
