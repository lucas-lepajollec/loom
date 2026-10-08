package loom

import (
	"context"
	"encoding/json"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCuratedAgentsLaunchAvailabilityAndCompatibility(t *testing.T) {
	testHome(t)
	for _, id := range []string{"hermes", "openclaw", "deepseek-tui"} {
		var a acpAgent
		for _, candidate := range builtinACPAgents() {
			if candidate.ID == id {
				a = candidate
			}
		}
		want := []string{"acp"}
		if id == "deepseek-tui" {
			want = []string{"serve", "--acp"}
		}
		if a.Command != id || !reflect.DeepEqual(a.Args, want) || !reflect.DeepEqual(a.Detect, []string{id}) {
			t.Fatal(a)
		}
		a.Command = filepath.Join(t.TempDir(), "missing")
		a.Detect = nil
		if a.available() {
			t.Fatal("missing launcher available")
		}
		a.Command, _ = os.Executable()
		if !a.available() {
			t.Fatal("installed launcher unavailable")
		}
		for _, info := range []map[string]any{nil, {"version": "fixture"}} {
			r := acpCompatibility(a, info, nil)
			if r.Warning != "not yet verified with Loom" || r.TestedVersion != "" || len(r.TestedVersions) != 0 || r.AdapterPackage == "" {
				t.Fatal(r)
			}
		}
		d := (&acpAdapter{agent: a}).Descriptor()
		if !hasRuntimeCapability(d, "user-input") || !hasRuntimeCapability(d, "elicitation") {
			t.Fatal(d)
		}
	}
}

func TestCuratedACPFixtureTurns(t *testing.T) {
	for _, id := range []string{"hermes", "openclaw", "deepseek-tui"} {
		for _, name := range []string{"normal", "permission", "elicitation"} {
			t.Run(id+"/"+name, func(t *testing.T) {
				testHome(t)
				t.Setenv("LOOM_STEP2_ACP_FIXTURE", id)
				t.Setenv("LOOM_STEP2_ACP_CASE", name)
				t.Setenv("LOOM_STEP2_ACP_FIXTURE_ROOT", filepath.Join(mustWorkingDir(t), "testdata/agents"))
				exe, _ := os.Executable()
				m := newRuntimeSessions()
				defer m.shutdownACP()
				a := acpAgent{ID: id, Name: id, Command: exe, Args: []string{"-test.run=^TestStep2ACPFixtureProcess$"}, Custom: true}
				s := RuntimeSession{ID: "fixture", RuntimeID: id, Model: "default", ACPState: ACPState{Workdir: t.TempDir(), Permission: "ask"}}
				if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(filepath.Join("testdata/agents", id, name+".jsonl"))
				var row struct {
					Answer agent.RequestAnswer `json:"answer"`
				}
				for i, b := range data {
					if b == '\n' {
						_ = json.Unmarshal(data[:i], &row)
						break
					}
				}
				var mu sync.Mutex
				opened, resolved := 0, 0
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				result, err := m.runACP(ctx, a, s, RuntimeTurn{Messages: []Message{{Role: "user", Content: "Fixture prompt"}}}, func(e StreamEvent) bool {
					if e.AgentEvent == nil {
						return true
					}
					mu.Lock()
					defer mu.Unlock()
					if e.AgentEvent.Type == "request.resolved" {
						resolved++
					}
					if req := e.AgentEvent.Request; req != nil {
						opened++
						go func() {
							if req.Kind == "approval" {
								if err := m.answerACP(s.ID, req.ID, row.Answer.Decision, false); err != nil {
									t.Error(err)
								}
								return
							}
							m.acpMu.Lock()
							b := m.requests[s.ID]
							m.acpMu.Unlock()
							if err := b.Resolve(req.ID, row.Answer); err != nil {
								t.Error(err)
							}
						}()
					}
					return true
				})
				if err != nil || len(result) != 1 || result[0].Content != "Hello from "+id+"." {
					t.Fatal(result, err)
				}
				mu.Lock()
				defer mu.Unlock()
				want := 1
				if name == "normal" {
					want = 0
				}
				if opened != want || resolved != want {
					t.Fatal(opened, resolved)
				}
			})
		}
	}
}
