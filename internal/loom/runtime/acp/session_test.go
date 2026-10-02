package acp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSessionResponseLegacyModelsAndNestedConfig(t *testing.T) {
	var r SessionResponse
	raw := `{"sessionId":"native","models":{"currentModelId":"b","availableModels":[{"modelId":"a","name":"","description":"first"},{"modelId":"b","name":"Bee"}]}}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	option := ModelOption(r.Options())
	b, _ := json.Marshal(option)
	want := `{"category":"model","currentValue":"b","id":"model","loomLegacyModel":true,"name":"Model","options":[{"description":"first","name":"a","value":"a"},{"description":"","name":"Bee","value":"b"}],"type":"select"}`
	if string(b) != want {
		t.Fatalf("%s", b)
	}
	if !ConfigValueAllowed(option, "b") || ConfigValueAllowed(option, "other") || ConfigValueAllowed(option, true) {
		t.Fatal(option)
	}
	nested := map[string]any{"id": "MODEL", "type": "select", "options": []any{map[string]any{"name": "group", "options": option["options"]}}}
	if !ConfigValueAllowed(nested, "a") || !reflect.DeepEqual(OptionValues(nested), []OptionValue{{"a", "a", "first"}, {"b", "Bee", ""}}) {
		t.Fatal(nested)
	}
	r.Config = []map[string]any{nested}
	if !reflect.DeepEqual(r.Options(), r.Config) {
		t.Fatal("legacy option duplicated")
	}
	primary := map[string]any{"id": "primary", "category": "model"}
	if ModelOption([]map[string]any{nested, primary})["id"] != "primary" {
		t.Fatal("category precedence changed")
	}
	if !ConfigValueAllowed(map[string]any{"type": "boolean"}, false) || ConfigValueAllowed(map[string]any{"type": "boolean"}, "false") {
		t.Fatal("boolean shape changed")
	}
	if (SessionResponse{}).Options() != nil {
		t.Fatal("absent config must stay nil")
	}
}
