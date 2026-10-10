package brain

import (
	"strings"
	"testing"
)

func TestPackRetainsDistinctProvenance(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"a.md": "# Alpha\nalpha evidence", "b.md": "# Alpha\nalpha evidence"}, "context")
	hits, err := e.Search(SearchRequest{Query: "alpha"})
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// Repeated hits of one chunk must still be deduplicated.
	hits = append(hits, hits[0])
	pack, err := e.PackHits(PackRequest{BudgetTokens: 1500}, hits)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Chunks) != 2 || len(pack.Citations) != 2 {
		t.Fatalf("lost provenance: chunks=%d citations=%d", len(pack.Chunks), len(pack.Citations))
	}
	for _, path := range []string{"a.md", "b.md"} {
		if !strings.Contains(pack.Text, "[notes: "+path+" › Alpha]") {
			t.Fatalf("missing citation for %s", path)
		}
	}
	// The second provenance remains subject to the current path authorization.
	scoped, err := e.PackHits(PackRequest{BudgetTokens: 1500, PathPrefixes: map[string][]string{"notes": {"a.md"}}}, hits)
	if err != nil || len(scoped.Chunks) != 1 || strings.Contains(scoped.Text, "b.md") {
		t.Fatalf("scope: %+v err=%v", scoped, err)
	}
}

func TestPackRetainsDistinctHeadingProvenance(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"sections.md": "# First\n## Alpha\nalpha evidence\n# Second\n## Alpha\nalpha evidence"}, "context")
	pack, err := e.Pack(PackRequest{Query: "alpha", BudgetTokens: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Chunks) != 2 || !strings.Contains(pack.Text, "sections.md › First › Alpha]") || !strings.Contains(pack.Text, "sections.md › Second › Alpha]") {
		t.Fatalf("lost heading provenance: %+v", pack)
	}
}
