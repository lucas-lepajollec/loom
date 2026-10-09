package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capabilityJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := decodeSchema([]byte(text), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestCapabilityNormalizationStability(t *testing.T) {
	a := capabilityJSON(t, `{"initialize":{"protocolVersion":1,"agentCapabilities":{"future":{"enabled":true}},"agentInfo":{"name":"test","version":"1"},"_meta":{"extension":{"secret":"ignored"}},"authMethods":[{"id":"login","name":"Login"}]},"session/new":{"sessionId":"random1","modes":{"currentModeId":"a","availableModes":[{"id":"b"},{"id":"a"}]},"configOptions":[{"id":"effort","currentValue":"low"}],"models":{"currentModelId":"m1","availableModels":[{"modelId":"m1","name":"One","reasoning":true},{"modelId":"m2","name":"Two","reasoning":true}]}},"timestamp":"today","modelCount":2,"initializePath":"/tmp/first","codexHome":"/tmp/home1"}`)
	b := capabilityJSON(t, `{"initializePath":"/tmp/second","codexHome":"/tmp/home2","modelCount":1,"timestamp":"tomorrow","session/new":{"models":{"availableModels":[{"modelId":"other","name":"Other","reasoning":true}],"currentModelId":"other"},"modes":{"availableModes":[{"id":"a"},{"id":"b"}],"currentModeId":"b"},"sessionId":"random2","configOptions":[{"currentValue":"high","id":"effort"}]},"initialize":{"_meta":{"extension":false},"authMethods":[{"name":"Login","id":"login"}],"agentInfo":{"version":"2","name":"test"},"agentCapabilities":{"future":{"enabled":true}},"protocolVersion":1}}`)
	x, err := normalizeCapabilities(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := normalizeCapabilities(b)
	if err != nil || !bytes.Equal(x, y) {
		t.Fatalf("unstable:\n%s\n%s\n%v", x, y, err)
	}

	if !strings.Contains(string(x), `"future"`) || !strings.Contains(string(x), `"login"`) || !strings.Contains(string(x), `"effort"`) || strings.Contains(string(x), "random") || strings.Contains(string(x), "ignored") {
		t.Fatal(string(x))
	}
}
func TestModelCapabilityFlagsAndEfforts(t *testing.T) {
	base := capabilityJSON(t, `{"model/list":{"models":[{"id":"one","supportedReasoningEfforts":[{"reasoningEffort":"low","description":"Low"}],"supportsImages":false}]}}`)
	next := capabilityJSON(t, `{"model/list":{"models":[{"id":"two","supportedReasoningEfforts":[{"reasoningEffort":"high","description":"High"}],"supportsImages":true,"futureFlag":true}]}}`)
	a, _ := normalizeCapabilities(base)
	b, _ := normalizeCapabilities(next)
	diff, err := capabilityDiff(a, b)
	if err != nil || !strings.Contains(diff, "futureFlag") || !strings.Contains(diff, "supportsImages") || !strings.Contains(diff, "reasoningEffort") {
		t.Fatal(diff, err)
	}
}
func TestCapabilityDiffKindsAndPointers(t *testing.T) {
	before := []byte(`{"same":true,"removed":null,"nested":{"a/b":false,"~x":1}}`)
	after := []byte(`{"added":null,"same":true,"nested":{"a/b":true,"~x":2}}`)
	got, err := capabilityDiff(before, after)
	want := "- added `/added`\n- changed `/nested/a~1b`\n- changed `/nested/~0x`\n- removed `/removed`"
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	if got, err := capabilityDiff(after, after); err != nil || got != "unchanged" {
		t.Fatal(got, err)
	}
}
func TestCapabilityBaselineAndExplicitRefresh(t *testing.T) {
	baseline, out := filepath.Join(t.TempDir(), "capabilities"), t.TempDir()
	raw := capabilityJSON(t, `{"initialize":{"agentCapabilities":{"loadSession":true}}}`)
	summary, snapshot, err := checkCapabilities("agent", raw, baseline, out, false)
	if err != nil || summary != "baseline created" || !json.Valid(snapshot) {
		t.Fatal(summary, err)
	}
	if _, err := os.Stat(filepath.Join(baseline, "agent.json")); !os.IsNotExist(err) {
		t.Fatal("implicit checkout mutation", err)
	}
	plan := planReview([]candidate{{ID: "agent", Capabilities: summary, Snapshot: snapshot}}, nil)
	if len(plan.Passing) != 1 || len(plan.Attention) != 0 {
		t.Fatal(plan)
	}
	// This is also the exact write path used in the publication worktree.
	if err := saveCapabilitySnapshot(baseline, "agent", snapshot); err != nil {
		t.Fatal(err)
	}
	if summary, _, err := checkCapabilities("agent", raw, baseline, out, false); err != nil || summary != "unchanged" {
		t.Fatal(summary, err)
	}
	changed := capabilityJSON(t, `{"initialize":{"agentCapabilities":{"loadSession":false,"future":true}}}`)
	summary, _, err = checkCapabilities("agent", changed, baseline, out, true)
	if err != nil || !strings.Contains(summary, "added") || !strings.Contains(summary, "changed") {
		t.Fatal(summary, err)
	}
	if summary, _, err := checkCapabilities("agent", changed, baseline, out, false); err != nil || summary != "unchanged" {
		t.Fatal(summary, err)
	}
	plan = planReview([]candidate{{ID: "agent", Version: "2", CapabilityChanged: true}}, nil)
	if len(plan.Passing) != 1 || len(plan.Attention) != 1 {
		t.Fatal("capability changes need both review channels", plan)
	}
	if summary, data, err := checkCapabilities("agent", nil, baseline, out, true); err != nil || data != nil || !strings.Contains(summary, "unavailable") {
		t.Fatal(summary, err)
	}
}

func TestCapabilityNamedListsAndAccountModelOptions(t *testing.T) {
	before := capabilityJSON(t, `{"commands":[],"configOptions":[{"id":"model","category":"model","options":[{"name":"One","value":"account-model-1"}]}]}`)
	after := capabilityJSON(t, `{"commands":[{"name":"compact","input":null}],"configOptions":[{"id":"model","category":"model","options":[{"name":"Two","value":"account-model-2"},{"name":"Three","value":"account-model-3"}]}]}`)
	a, _ := normalizeCapabilities(before)
	b, _ := normalizeCapabilities(after)
	diff, err := capabilityDiff(a, b)
	if err != nil || diff != "- added `/commands/compact`" || strings.Contains(string(b), "account-model") {
		t.Fatal(diff, string(b), err)
	}
}
func TestModelCapabilityModalitiesAndEmptyExtensions(t *testing.T) {
	before := capabilityJSON(t, `{"models":[{"id":"one","input":["text"]}]}`)
	after := capabilityJSON(t, `{"models":[{"id":"two","input":["text","image"],"futureExtension":{}}]}`)
	a, _ := normalizeCapabilities(before)
	b, _ := normalizeCapabilities(after)
	diff, err := capabilityDiff(a, b)
	if err != nil || !strings.Contains(diff, "input") || !strings.Contains(diff, "futureExtension") {
		t.Fatal(diff, err)
	}
}

func TestModelNamedCapabilityBooleansAreNotCatalogs(t *testing.T) {
	a, _ := normalizeCapabilities(capabilityJSON(t, `{"agentCapabilities":{"models":false,"model":false}}`))
	b, _ := normalizeCapabilities(capabilityJSON(t, `{"agentCapabilities":{"models":true,"model":true}}`))
	diff, err := capabilityDiff(a, b)
	if err != nil || !strings.Contains(diff, "/agentCapabilities/models") || !strings.Contains(diff, "/agentCapabilities/model") {
		t.Fatal(diff, err)
	}
}

func TestCapabilityProseIgnored(t *testing.T) {
	a, err := normalizeCapabilities(map[string]any{"commands": []any{map[string]any{"name": "review", "description": "Review the diff", "hint": "[pr]"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalizeCapabilities(map[string]any{"commands": []any{map[string]any{"name": "review", "description": "Review the current diff carefully", "hint": "[pr#]"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("rewording changed the snapshot:\n%s\n%s", a, b)
	}
	c, _ := normalizeCapabilities(map[string]any{"commands": []any{map[string]any{"name": "review"}, map[string]any{"name": "doctor"}}})
	if string(a) == string(c) {
		t.Fatal("a new command must change the snapshot")
	}
}
