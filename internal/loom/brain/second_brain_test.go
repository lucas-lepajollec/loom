package brain

import (
	"context"
	"testing"
)

func TestSecondBrainsKeepProvenancePersonalOptInAndExclusions(t *testing.T) {
	e, dir, _ := fixture(t, map[string]string{"docs/decisions.md": "quartz operational decision", "private/notes.md": "quartz quarantined record", "privé/notes.md": "quartz unicode excluded record", "docs/.env": "quartz synthetic credential", "docs/auth.json": "quartz synthetic credential"}, "repo")
	source := Source{ID: "notes", Label: "Operational checkout", Path: dir, Kind: "repo", Connector: "git", Permission: "write", Primary: true, Exclude: []string{"private/**", "privé/**"}}
	if err := e.Update(source); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, got := range e.Sources() {
		if got.ID == source.ID {
			found = got.Primary && got.Permission == "write"
		}
	}
	if !found {
		t.Fatal("second brain permissions lost")
	}
	personal := t.TempDir()
	write(t, personal, "notes.md", "quartz personal vault")
	if err := e.Update(Source{ID: "vault", Label: "Knowledge", Path: personal, Kind: "personal", Connector: "obsidian", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	for _, got := range e.Sources() {
		if got.ID == "notes" && got.Primary {
			t.Fatal("setting a new primary did not clear the previous primary atomically")
		}
	}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits, err := e.Search(SearchRequest{Query: "quartz"})
	if err != nil || len(hits) != 1 || hits[0].Source != "notes" || hits[0].Path != "docs/decisions.md" {
		t.Fatalf("source scope: %+v %v", hits, err)
	}
	hits, err = e.Search(SearchRequest{Query: "quartz", Sources: []string{"vault"}, Personal: true})
	if err != nil || len(hits) != 1 || hits[0].Source != "vault" {
		t.Fatalf("explicit personal selection: %+v %v", hits, err)
	}
	source.Exclude = []string{"private/**", "privé/**", "docs/**"}
	if err := e.Update(source); err != nil {
		t.Fatal(err)
	}
	if hits, err = e.Search(SearchRequest{Query: "quartz"}); err != nil || len(hits) != 0 {
		t.Fatalf("excluded stale chunks remained: %+v %v", hits, err)
	}
	if err := e.Remove("vault"); err != nil {
		t.Fatal(err)
	}
	// The linked source is still the original directory after removal.
	if err := e.Update(Source{ID: "vault2", Label: "Same source", Path: personal, Kind: "personal", Connector: "obsidian"}); err != nil {
		t.Fatal(err)
	}
}
