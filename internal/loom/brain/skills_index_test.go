package brain

import (
	"context"
	"path/filepath"
	"testing"
)

func TestIndexerExcludesActiveSkillsIncludingCachedFiles(t *testing.T) {
	e, dir, storage := fixture(t, map[string]string{"skills/review/SKILL.md": "skillneedle", "skills/review/help.md": "helpneedle", "skills-notes.md": "noteneedle"}, "context")
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	opts := Options{Storage: storage, ExcludedDirectories: func() []string { return []string{filepath.Join(dir, "skills")} }}
	e, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"cached", "refreshed"} {
		if phase == "refreshed" {
			if err = e.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		for q, want := range map[string]int{"skillneedle": 0, "helpneedle": 0, "noteneedle": 1} {
			hits, err := e.Search(SearchRequest{Query: q})
			if err != nil || len(hits) != want {
				t.Fatalf("%s %s: %v %v", phase, q, hits, err)
			}
		}
	}
}
