package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
