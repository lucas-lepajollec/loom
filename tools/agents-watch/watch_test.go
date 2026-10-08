package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

func TestSchemaComparisonRetainsUnionAndDetectsDrift(t *testing.T) {
	before := []byte(`{"definitions":{"Message":{"oneOf":[{"type":"string"},{"type":"object"}]}}}`)
	same := []byte(`{ "definitions": { "Message": { "oneOf": [{"type":"string"}, {"type":"object"}] } } }`)
	if summary, err := schemaSummary(before, same); err != nil || summary != "unchanged" {
		t.Fatal(summary, err)
	}
	after := []byte(`{"definitions":{"Message":{"oneOf":[{"type":"string"}]}}}`)
	if summary, err := schemaSummary(before, after); err != nil || !strings.Contains(summary, "/definitions/Message/oneOf") {
		t.Fatal(summary, err)
	}
}

func TestPublishDecisions(t *testing.T) {
	passing := candidate{ID: "pi", Version: "1.1.0"}
	attention := candidate{ID: "codex", Version: "codex-cli 0.162.0", Problems: []string{"schema changed"}}
	registryErr := errors.New("registry unavailable")
	for _, tc := range []struct {
		name     string
		checks   []candidate
		registry error
		want     reviewPlan
	}{
		{"passing", []candidate{passing}, nil, reviewPlan{Passing: []candidate{passing}, UpdateRegistry: true}},
		{"mixed", []candidate{attention, passing}, nil, reviewPlan{Passing: []candidate{passing}, Attention: []string{"Agents watch: codex codex-cli 0.162.0 needs attention"}, UpdateRegistry: true}},
		{"all attention", []candidate{attention}, registryErr, reviewPlan{Attention: []string{"Agents watch: codex codex-cli 0.162.0 needs attention", "Agents watch: registry latest needs attention"}}},
		{"registry only failure", []candidate{passing}, registryErr, reviewPlan{Passing: []candidate{passing}, Attention: []string{"Agents watch: registry latest needs attention"}}},
		{"registry only update", []candidate{attention}, nil, reviewPlan{Attention: []string{"Agents watch: codex codex-cli 0.162.0 needs attention"}, UpdateRegistry: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := planReview(tc.checks, tc.registry)
			if !reflect.DeepEqual(p, tc.want) {
				t.Fatalf("got %+v, want %+v", p, tc.want)
			}
			calls := []string{}
			err := publishPlan(p, func(checks []candidate, registry bool) error {
				if !reflect.DeepEqual(checks, tc.want.Passing) || registry != tc.want.UpdateRegistry {
					t.Fatal("publishing failed candidates or invalid registry")
				}
				calls = append(calls, "pr")
				return nil
			}, func(title string) error { calls = append(calls, title); return nil })
			want := []string{}
			if len(tc.want.Passing) > 0 || tc.want.UpdateRegistry {
				want = append(want, "pr")
			}
			want = append(want, tc.want.Attention...)
			if err != nil || !reflect.DeepEqual(calls, want) {
				t.Fatalf("publication order: %v, %v", calls, err)
			}
		})
	}
}

func TestPublishAttemptsAllReviewsOnFailure(t *testing.T) {
	prErr, issueErr := errors.New("PR failed"), errors.New("issue failed")
	calls := []string{}
	err := publishPlan(reviewPlan{UpdateRegistry: true, Attention: []string{"first", "second"}}, func([]candidate, bool) error {
		calls = append(calls, "pr")
		return prErr
	}, func(title string) error {
		calls = append(calls, title)
		return issueErr
	})
	if !reflect.DeepEqual(calls, []string{"pr", "first", "second"}) || !errors.Is(err, prErr) || !errors.Is(err, issueErr) {
		t.Fatal(calls, err)
	}
}
func TestSchemaLargeIntegerDrift(t *testing.T) {
	before := []byte(`{"maximum":18446744073709551615}`)
	after := []byte(`{"maximum":18446744073709551614}`)
	if summary, err := schemaSummary(before, after); err != nil || summary == "unchanged" {
		t.Fatal(summary, err)
	}
}
func TestCodexSchemaMergeRequiresAllConsumedMessages(t *testing.T) {
	dir := t.TempDir()
	types := filepath.Join(dir, "types.go")
	if err := os.WriteFile(types, []byte("package fixture\ntype Message struct{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeCodexSchema(dir, types); err == nil {
		t.Fatal("missing message accepted")
	}
	schema := `{"definitions":{"Part":{"type":"string"}},"oneOf":[{"$ref":"#/definitions/Part"},{"type":"object"}]}`
	if err := os.WriteFile(filepath.Join(dir, "Message.json"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := mergeCodexSchema(dir, types)
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatal(err)
	}
	defs := merged["definitions"].(map[string]any)
	if defs["Part"] == nil || defs["Message"].(map[string]any)["oneOf"] == nil {
		t.Fatal(merged)
	}
}
func TestRegistryRefreshValidationAndAuthSkips(t *testing.T) {
	for _, url := range []string{"http://example.com/a", "file:///a"} {
		if err := validateRegistry([]byte(`{"agents":[{"id":"agent","distribution":{"binary":{"linux-x86_64":{"archive":"` + url + `"}}}}]}`)); err == nil {
			t.Fatal(url)
		}
	}
	if err := validateRegistry([]byte(`{"agents":[]}`)); err == nil {
		t.Fatal("empty registry")
	}
	if !authRequired(&acp.RPCError{Code: -32000, Message: "Authentication required"}) || authRequired(&acp.RPCError{Code: -32000, Message: "Internal error"}) || authRequired(errors.New("process exited")) {
		t.Fatal("auth classification")
	}
}
