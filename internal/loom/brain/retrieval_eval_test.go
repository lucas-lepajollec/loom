package brain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	retrieval "github.com/lucas-lepajollec/loom/tools/brain-retrieval"
)

func TestRetrievalEvidence(t *testing.T) {
	corpus, err := retrieval.Load(retrieval.FixtureDir("testdata/retrieval"))
	if err != nil {
		t.Fatal(err)
	}
	report := retrieval.NewReport(corpus)
	for _, c := range corpus.Cases {
		t.Run(c.ID, func(t *testing.T) {
			e, docs, index, err := retrieval.Build(t.TempDir(), c)
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range retrieval.Budgets {
				row, _, _, err := retrieval.Measure(e, c, corpus.Evidence[c.ID], docs, budget, index)
				if err != nil {
					t.Fatal(err)
				}
				report.Rows = append(report.Rows, row)
			}
			if c.ID == "vector-invalidation" {
				old := brain.ChunkText(c.Documents[0].Source, c.Documents[0].Path, c.Documents[0].Text)
				v := brain.Vectors{Identity: "precomputed-test-vector", Values: map[string][]float32{old[0].ID: {1, 0}}}
				chunks, err := e.Chunks(brain.SearchRequest{})
				if err != nil {
					t.Fatal(err)
				}
				v.Prune(chunks)
				if len(v.Values) != 0 {
					t.Fatal("edited chunk retained stale vector")
				}
				if _, err = e.HybridSearch(brain.SearchRequest{Query: c.Query}, []float32{1, 0}, v.Values); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	if err = report.Write(os.Getenv("LOOM_BRAIN_RETRIEVAL_OUT")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/retrieval/baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline retrieval.Baseline
	if err = json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LOOM_BRAIN_RETRIEVAL_CASES") == "" {
		if err = retrieval.Gate(report, baseline); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(report.Summary())
}

func BenchmarkRetrievalEvidence(b *testing.B) {
	corpus, err := retrieval.Load("testdata/retrieval")
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range corpus.Cases {
		b.Run(c.ID, func(b *testing.B) {
			e, _, _, err := retrieval.Build(filepath.Join(b.TempDir(), "corpus"), c)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				hits, err := e.Search(brain.SearchRequest{Query: c.Query, Sources: c.Sources, PathPrefixes: c.Paths, Limit: 20})
				if err != nil {
					b.Fatal(err)
				}
				if _, err = e.PackHits(brain.PackRequest{Query: c.Query, Sources: c.Sources, PathPrefixes: c.Paths, BudgetTokens: 1500}, hits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRetrievalEvidenceMetricsAndGate(t *testing.T) {
	text := "An entire supporting turn says that the helios code is 12345 and explains why."
	docs := []retrieval.Document{{Source: "vault", Path: "full.md", Text: text}}
	spans := []retrieval.Span{{Source: "vault", Path: "full.md", Text: text}}
	chunks := brain.ChunkText("vault", "full.md", text)
	if c := retrieval.Present(spans, docs, chunks, "[vault: full.md] 12345"); c.Covered != 0 {
		t.Fatal("citation/answer-only counted as full evidence")
	}
	if c := retrieval.Present(spans, docs, chunks, text); c.Covered != 1 {
		t.Fatal("full span not counted")
	}
	duplicateDocs := append(docs, retrieval.Document{Source: "vault", Path: "duplicate.md", Text: text})
	duplicateSpans := append(spans, retrieval.Span{Source: "vault", Path: "duplicate.md", Text: text})
	duplicateChunks := append(chunks, brain.ChunkText("vault", "duplicate.md", text)...)
	if c := retrieval.Present(duplicateSpans, duplicateDocs, duplicateChunks, "[vault: full.md]\n"+text); c.Covered != 1 {
		t.Fatal("one packed duplicate counted as two supporting units")
	}
	if c := retrieval.Cover(nil, docs, chunks, false); c.Recall != nil || c.Complete != nil {
		t.Fatal("abstention has fabricated denominator")
	}
	encoded, err := json.Marshal(retrieval.Aggregate{})
	if err != nil || !strings.Contains(string(encoded), `"recall5":null`) {
		t.Fatal("abstention summary has fabricated denominator")
	}
	one := 1.0
	yes := true
	row := retrieval.Row{ID: "gate", Type: "test", Route: "local", Budget: 512, Recall: map[int]retrieval.Coverage{5: {Recall: &one}}, Final: &retrieval.Coverage{Complete: &yes}}
	report := retrieval.Report{FixtureHash: "fixture", Rows: []retrieval.Row{row}, Macro: map[string]retrieval.Aggregate{}}
	baseline := retrieval.Baseline{FixtureHash: "fixture", Tolerance: .01, Rows: map[string]retrieval.Aggregate{retrieval.Key(row): {Recall5: 1, FinalPresence: &one}}}
	if err := retrieval.Gate(report, baseline); err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	report.Rows[0].Recall[5] = retrieval.Coverage{Recall: &zero}
	if err := retrieval.Gate(report, baseline); err == nil {
		t.Fatal("Recall@5 regression accepted")
	}
	report.Rows[0].Recall[5] = retrieval.Coverage{Recall: &one}
	no := false
	report.Rows[0].Final.Complete = &no
	if err := retrieval.Gate(report, baseline); err == nil {
		t.Fatal("final-context regression accepted")
	}
	// Oversized ingestion must fail, never silently reduce the history.
	_, _, _, err = retrieval.Build(t.TempDir(), retrieval.Case{Documents: []retrieval.Document{{Source: "conversations", Path: "big.md", Text: string(make([]byte, brain.MaxFileBytes+1))}}})
	if err == nil {
		t.Fatal("oversized corpus silently accepted")
	}
}
