package loom

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/antigravity"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/opencodehttp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

// Captured before extraction from the three root collectors. No subprocess,
// socket, account or model is needed. All committed native JSONL fixtures are
// replayed through their real protocol mappers and the display projection.
func TestNativeAnswerCanonicalFixtures(t *testing.T) {
	for _, family := range []string{"codex", "pi", "opencode", "antigravity"} {
		paths, err := filepath.Glob("testdata/agents/" + family + "/*.jsonl")
		if err != nil || len(paths) == 0 {
			t.Fatalf("missing %s fixtures: %v", family, err)
		}
		for _, path := range paths {
			t.Run(family+"/"+filepath.Base(path), func(t *testing.T) {
				got := replayAnswerFixture(t, family, path)
				golden := "testdata/agents/answers/" + family + "-" + strings.TrimSuffix(filepath.Base(path), ".jsonl") + ".json"
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("canonical output changed byte-for-byte: %s", golden)
				}
			})
		}
	}
}

func replayAnswerFixture(t *testing.T, family, path string) []byte {
	t.Helper()
	policy := agent.ReplaceItemAndChildren
	if family == "opencode" {
		policy = agent.ReplaceItem
	}
	if family == "antigravity" {
		policy = agent.ReplaceAnswer
	}
	answer := agent.NewAnswerAccumulator(policy)
	projection := newAgentProjection()
	type output struct {
		Event    agent.AgentEvent `json:"event"`
		Display  DiscussionEvent  `json:"display"`
		Snapshot *string          `json:"snapshot,omitempty"`
	}
	out := []output{}
	record := func(e agent.AgentEvent, snapshot *string) {
		d := projection.event(e)
		delete(d, "agent_event") // the same canonical envelope is recorded above
		out = append(out, output{e, d, snapshot})
	}
	sink := func(e agent.AgentEvent) bool {
		snapshot := answer.Apply(e)
		if e.Type == "turn.completed" {
			for _, closed := range projection.closeItems(e) {
				record(closed, nil)
			}
		}
		record(e, snapshot)
		return true
	}
	c := &agentstdio.Client{}
	codexapp.New(c, agent.NewRequestBroker("codex", sink), sink)
	pi := pirpc.Mapper{ThreadID: "fixture-session", RunID: "turn-1"}
	oc := opencodehttp.NewMapper("ses_fixture", "USER_ID")
	agy := antigravity.Mapper{TurnID: "turn-1"}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	exitCode := 0
	for scanner.Scan() {
		var row struct {
			Frame    json.RawMessage `json:"frame"`
			ExitCode int             `json:"exit_code"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		exitCode = row.ExitCode
		var frame agentstdio.Frame
		if err := json.Unmarshal(row.Frame, &frame); err != nil {
			t.Fatal(err)
		}
		frame.Raw = row.Frame
		switch family {
		case "codex":
			if len(frame.ID) > 0 {
				e, err := codexapp.Request(frame)
				if err != nil {
					e.Type, e.Request = "raw", nil
					e.Payload = agent.BoundedJSON(frame.Params)
					sink(e)
					sink(agent.AgentEvent{Type: "error", Runtime: "codex", Method: frame.Method, Error: err.Error(), Raw: agent.BoundedJSON(frame.Raw)})
				} else {
					sink(e)
				}
			} else {
				c.Notify(frame)
			}
		case "pi":
			if frame.Type == "response" {
				if e := pirpc.ContextObservation(frame); e != nil {
					sink(*e)
				}
			} else if frame.Type == "extension_ui_request" {
				e, _, err := pirpc.Request(frame)
				sink(e)
				if err != nil {
					sink(agent.AgentEvent{Type: "error", Runtime: "pi", Method: e.Method, Error: err.Error(), Raw: agent.BoundedJSON(frame.Raw)})
				}
			} else {
				for _, e := range pi.Notification(frame) {
					sink(e)
				}
			}
		case "opencode":
			events, err := oc.Events(row.Frame)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range events {
				sink(e)
			}
		case "antigravity":
			events, err := agy.Feed(row.Frame)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range events {
				sink(e)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if family == "antigravity" {
		var processErr error
		if exitCode != 0 {
			processErr = fmt.Errorf("exit status %d", exitCode)
		}
		events, _ := agy.Finish(processErr)
		for _, e := range events {
			sink(e)
		}
	}
	got, err := json.Marshal(struct {
		Events []output `json:"events"`
		Answer string   `json:"answer"`
	}{out, answer.Text()})
	if err != nil {
		t.Fatal(err)
	}
	return append(got, '\n')
}
