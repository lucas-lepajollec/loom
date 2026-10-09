package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluationTable(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	_ = os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling"))
	cap := 10.0
	low := 9.0
	high := 10.0
	base := []Rule{{ID: "global", Scope: "global", Subject: "agent.*", Decision: Confirm}, {ID: "edits", Scope: "agent", ScopeID: "a", Subject: "agent.file_write", Decision: Allow, Conditions: Conditions{InsideWorkdir: true}}, {ID: "commands", Scope: "project", ScopeID: "p", Subject: "agent.command", Decision: Allow, Conditions: Conditions{CommandPrefixes: []string{"go test"}}}, {ID: "cap", Scope: "global", Subject: "spend.provider", Decision: Allow, Conditions: Conditions{ProviderID: "provider", MaxCost: &cap}}}
	for _, test := range []struct {
		name   string
		in     Input
		want   Decision
		reason string
	}{
		{"default", Input{Subject: "node.terminal", Fallback: Allow}, Allow, "legacy_default"},
		{"global", Input{Subject: "agent.command"}, Confirm, "rule"},
		{"inside", Input{Subject: "agent.file_write", AgentID: "a", Workdir: root, Path: "new.md"}, Allow, "rule"},
		{"outside", Input{Subject: "agent.file_write", AgentID: "a", Workdir: root, Path: "../other.md"}, Confirm, "rule"},
		{"symlink", Input{Subject: "agent.file_write", AgentID: "a", Workdir: root, Path: "escape/new.md"}, Confirm, "rule"},
		{"dangling_symlink", Input{Subject: "agent.file_write", AgentID: "a", Workdir: root, Path: "dangling/new.md"}, Confirm, "rule"},
		{"prefix", Input{Subject: "agent.command", ProjectID: "p", Command: "go test ./..."}, Allow, "rule"},
		{"prefix_boundary", Input{Subject: "agent.command", ProjectID: "p", Command: "go testing"}, Confirm, "rule"},
		{"compound", Input{Subject: "agent.command", ProjectID: "p", Command: "go test; rm -rf /"}, Confirm, "rule"},
		{"substitution", Input{Subject: "agent.command", ProjectID: "p", Command: "go test $(bad)"}, Confirm, "rule"},
		{"unknown_cost", Input{Subject: "spend.provider", ProviderID: "provider", Fallback: Allow}, Allow, "legacy_default"},
		{"below_cap", Input{Subject: "spend.provider", ProviderID: "provider", Cost: &low}, Allow, "rule"},
		{"at_cap", Input{Subject: "spend.provider", ProviderID: "provider", Cost: &high}, Deny, "cost_limit"},
		{"other_provider", Input{Subject: "spend.provider", ProviderID: "other", Cost: &high, Fallback: Allow}, Allow, "legacy_default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Evaluate(base, test.in)
			if got.Decision != test.want || got.Reason != test.reason {
				t.Fatalf("%+v", got)
			}
		})
	}
}
func TestPrecedenceMigrationAndDestinationBinding(t *testing.T) {
	rules := []Rule{{ID: "legacy", Scope: "project", ScopeID: "p", Subject: "data.send_provider", Decision: Allow, Migrated: true, Conditions: Conditions{Endpoint: "https://one.test", Model: "model"}}, {ID: "global-deny", Scope: "global", Subject: "data.*", Decision: Deny}, {ID: "project-confirm", Scope: "project", ScopeID: "p", Subject: "data.send_provider", Decision: Confirm}}
	in := Input{Subject: "data.send_provider", ProjectID: "p", Endpoint: "https://one.test", Model: "model", Fallback: Confirm}
	if r := Evaluate(rules, in); r.RuleID != "project-confirm" {
		t.Fatal(r)
	}
	if r := Evaluate(rules[:2], in); r.RuleID != "global-deny" {
		t.Fatal(r)
	}
	in.Model = "changed"
	if r := Evaluate(rules[:1], in); r.Decision != Confirm || r.RuleID != "" {
		t.Fatal(r)
	}
	rules = []Rule{{ID: "mcp", Scope: "machine", ScopeID: "local", Subject: "mcp.tool:server/*", Decision: Deny}}
	if r := Evaluate(rules, Input{Subject: "mcp.tool:server/tool", MachineID: "local"}); r.Decision != Deny {
		t.Fatal(r)
	}
}
func TestBoundedAuditHasNoPrivateInputs(t *testing.T) {
	a := Audit{Capacity: 2}
	for i := 0; i < 10; i++ {
		in := Input{Subject: "agent.command", Command: "PRIVATE_PROMPT", Path: "/home/PRIVATE_PATH", Endpoint: "https://secret:token@example.com"}
		a.Record(in, Result{Decision: Allow, Reason: "rule"}, i%2 == 0)
	}
	raw, _ := json.Marshal(a.Snapshot())
	if len(a.Snapshot()) != 2 || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "token") {
		t.Fatal(string(raw))
	}
}
func TestInvalidRules(t *testing.T) {
	for _, r := range []Rule{{ID: "x", Scope: "invalid", Subject: "agent.command", Decision: Allow}, {ID: "x", Scope: "agent", Subject: "agent.command", Decision: Allow}, {ID: "x", Scope: "global", Subject: "agent.command", Decision: "invented"}, {ID: "x", Scope: "global", Subject: "mcp.*.tool", Decision: Allow}} {
		if Validate(Document{Version: 1, Rules: []Rule{r}}) == nil {
			t.Fatal(r)
		}
	}
}
