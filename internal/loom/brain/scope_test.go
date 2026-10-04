package brain

import (
	"context"
	"testing"
)

func TestTranscriptScopeCannotLeakThroughLexicalSemanticReadOrPack(t *testing.T) {
	e, err := New(Options{Conversations: func(ctx context.Context, emit func(Document) bool) error {
		for _, path := range []string{"discussion/chosen/one.md", "discussion/chosen-extra/one.md", "discussion/other/one.md"} {
			emit(Document{Path: path, Text: "shared continuity evidence"})
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	scope := map[string][]string{"conversations": {"discussion/chosen"}}
	req := SearchRequest{Query: "continuity", Sources: []string{"conversations"}, PathPrefixes: scope}
	hits, err := e.Search(req)
	if err != nil || len(hits) != 1 || hits[0].Path != "discussion/chosen/one.md" {
		t.Fatalf("lexical scope: %v %v", hits, err)
	}
	all, err := e.Chunks(SearchRequest{Sources: []string{"conversations"}})
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string][]float32{}
	var excluded string
	for _, c := range all {
		vectors[c.ID] = []float32{1, 0}
		if c.Path == "discussion/other/one.md" {
			excluded = c.ID
		}
	}
	hits, err = e.HybridSearch(req, []float32{1, 0}, vectors)
	if err != nil || len(hits) != 1 {
		t.Fatalf("semantic scope leaked: %v %v", hits, err)
	}
	if _, err = e.Read(ReadRequest{ChunkID: excluded, Sources: req.Sources, PathPrefixes: scope}); err == nil {
		t.Fatal("known chunk bypassed transcript selection")
	}
	malicious, _ := e.Search(SearchRequest{Query: req.Query, Sources: req.Sources})
	pack, err := e.PackHits(PackRequest{Query: req.Query, Sources: req.Sources, PathPrefixes: scope, BudgetTokens: 1000}, malicious)
	if err != nil || len(pack.Chunks) != 1 {
		t.Fatalf("pack scope leaked: %v %v", pack, err)
	}
	req.PathPrefixes = map[string][]string{"conversations": {}}
	hits, err = e.Search(req)
	if err != nil || len(hits) != 0 {
		t.Fatal("empty explicit selection widened to all transcripts")
	}
}
