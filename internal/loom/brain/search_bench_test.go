package brain

import (
	"fmt"
	"strings"
	"testing"
)

// Synthetic bounded corpora exercise frequent postings and quoted phrases;
// setup is excluded and no user files, embedding engines or network are used.
func benchmarkCorpus(b *testing.B, n int) *Engine {
	b.Helper()
	e, err := New(Options{})
	if err != nil {
		b.Fatal(err)
	}
	e.sources = append(e.sources, Source{ID: "fixture", Kind: "context"})
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("doc-%d.md", i)
		e.files[p] = File{Source: "fixture", Path: p, Chunks: []Chunk{{ID: p, Source: "fixture", Path: p, Text: "alpha beta " + strings.Repeat("ordinary context words ", 12)}}}
	}
	e.buildIndexLocked()
	return e
}

func BenchmarkBrainSearch(b *testing.B) {
	for _, n := range []int{1000, 20000, 60000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e := benchmarkCorpus(b, n)
			for _, query := range []string{"alpha", `"alpha beta"`} {
				b.Run(query, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if _, err := e.Search(SearchRequest{Query: query, Limit: 10}); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
