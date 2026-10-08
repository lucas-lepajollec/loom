package codexapp

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

// Synthetic 0.162.0 fixtures derive from the committed schema. The subprocess
// corpus also replays these alongside the unchanged 0.159.2 exchanges.
func TestFixtureCodex162Payloads(t *testing.T) {
	for _, name := range []string{"metadata", "phases", "forward-errors"} {
		t.Run(name, func(t *testing.T) {
			file, err := os.Open("../../testdata/agents/codex/" + name + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				var row struct {
					Frame  json.RawMessage `json:"frame"`
					Events []string        `json:"events"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				var f agentstdio.Frame
				if err := json.Unmarshal(row.Frame, &f); err != nil {
					t.Fatal(err)
				}
				f.Raw = row.Frame
				e := Notification(f)[0]
				if e.Type != row.Events[len(row.Events)-1] || e.ThreadID != "fixture-thread" || e.TurnID != "turn-1" {
					t.Fatalf("event mapping: %+v", e)
				}
				var params map[string]json.RawMessage
				_ = json.Unmarshal(f.Params, &params)
				wantPayload := f.Params
				if f.Method == "item/completed" {
					wantPayload = params["item"]
					var item struct {
						ID, Type string
					}
					_ = json.Unmarshal(wantPayload, &item)
					if e.ItemID != item.ID || e.ItemType != ItemType(item.Type) {
						t.Fatalf("item mapping: %+v", e)
					}
				}
				var got, want any
				_ = json.Unmarshal(e.Payload, &got)
				_ = json.Unmarshal(wantPayload, &want)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(e.Raw, row.Frame) {
					t.Fatalf("lost native metadata: %+v", e)
				}
				if f.Method == "error" {
					var detail struct{ Message string }
					_ = json.Unmarshal(params["error"], &detail)
					message := e.Error
					if e.Type == "warning" {
						message = e.Message
					}
					if message != detail.Message {
						t.Fatalf("rewrote native error: %q != %q", message, detail.Message)
					}
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTurnStartAncestryIsOptional(t *testing.T) {
	var p TurnStartParams
	if err := json.Unmarshal([]byte(`{"threadId":"thread","parentTurnId":"parent","rootTurnId":"root"}`), &p); err != nil || p.ParentTurnId != "parent" || p.RootTurnId != "root" {
		t.Fatal(p, err)
	}
	data, err := json.Marshal(TurnStartParams{ThreadId: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for _, field := range []string{"parentTurnId", "rootTurnId"} {
		if _, sent := fields[field]; sent {
			t.Fatalf("unneeded ancestry sent: %s", data)
		}
	}
}
