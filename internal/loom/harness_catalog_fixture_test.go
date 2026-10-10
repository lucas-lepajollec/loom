package loom

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// Frozen on the pre-extraction checkout: public inspect metadata and all
// executable ACP/remote recipes must remain identical, including ordering.
func TestHarnessCatalogCanonicalFixture(t *testing.T) {
	harnessInspectSpec("")
	type recipe struct {
		ID, Name, Logo string
		Needs, Launch  []string
	}
	recipes := []recipe{}
	for _, d := range remoteHarnessDefs {
		recipes = append(recipes, recipe{d.ID, d.Name, d.Logo, d.Needs, d.Launch})
	}
	got, err := json.Marshal(struct {
		ACP     []acpAgent
		Inspect map[string]inspectSpec
		Remote  []recipe
	}{builtinACPAgents(), inspectSpecs, recipes})
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/agents/harness-catalog.json"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("launch/inspect catalogs changed byte-for-byte")
	}
}
