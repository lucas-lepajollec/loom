package brain

import (
	"container/heap"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// allowed requires both the opt-in and an explicitly named personal source.
// Its corpus is also used for BM25 statistics, keeping personal documents out
// of default ranking as well as out of results.
func (e *Engine) allowed(ids []string, personal bool) (map[string]bool, error) {
	if len(ids) > MaxSources+3 {
		return nil, errors.New("too many source filters")
	}
	explicit := map[string]bool{}
	for _, id := range ids {
		explicit[id] = true
	}
	allowed := map[string]bool{}
	for _, s := range e.sources {
		named := explicit[s.ID]
		delete(explicit, s.ID)
		if s.Kind == "personal" && (!personal || !named) {
			continue
		}
		if len(ids) == 0 || named {
			allowed[s.ID] = true
		}
	}
	if len(explicit) > 0 {
		return nil, errors.New("unknown source filter")
	}
	return allowed, nil
}
func (e *Engine) Search(r SearchRequest) ([]Hit, error) {
	if err := e.available(); err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.searchLocked(r)
}
func (e *Engine) searchLocked(r SearchRequest) ([]Hit, error) {
	if err := validateScope(r.PathPrefixes); err != nil {
		return nil, err
	}
	if len(r.Query) > 4096 {
		return nil, errors.New("query exceeds 4096 bytes")
	}
	allowed, err := e.allowed(r.Sources, r.Personal)
	if err != nil {
		return nil, err
	}
	if r.Limit <= 0 {
		r.Limit = 10
	}
	r.Limit = min(r.Limit, 100)
	query := queryTerms(r.Query)
	hits := []Hit{}
	if len(query) == 0 {
		return hits, nil
	}
	n, total := 0, 0
	for _, d := range e.docs {
		if allowed[d.chunk.Source] && allowsPath(r.PathPrefixes, d.chunk.Source, d.chunk.Path) {
			n++
			total += d.length
		}
	}
	if n == 0 {
		return hits, nil
	}
	average := float64(total) / float64(n)
	// Dense document IDs avoid a large hash map for frequent query terms.
	scores := make([]float64, len(e.docs))
	for _, term := range query {
		postings := e.postings[term]
		df := 0
		for _, p := range postings {
			if allowed[e.docs[p.doc].chunk.Source] && allowsPath(r.PathPrefixes, e.docs[p.doc].chunk.Source, e.docs[p.doc].chunk.Path) {
				df++
			}
		}
		idf := math.Log(1 + (float64(n-df)+0.5)/(float64(df)+0.5))
		for _, p := range postings {
			d := e.docs[p.doc]
			if !allowed[d.chunk.Source] || !allowsPath(r.PathPrefixes, d.chunk.Source, d.chunk.Path) {
				continue
			}
			tf := float64(p.frequency)
			scores[p.doc] += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(d.length)/average))
		}
	}
	quoted := phrases(r.Query)
	// Retain only the requested best results, rather than sorting every match.
	ranks := make(rankHeap, 0, r.Limit)
	for doc, score := range scores {
		if score == 0 {
			continue
		}
		c := e.docs[doc].chunk
		folded := ""
		if len(quoted) > 0 {
			folded = fold(c.Text)
		}
		for _, p := range quoted {
			if strings.Contains(folded, p) {
				score += 2
			}
		}
		candidate := ranked{doc, score}
		if len(ranks) < r.Limit {
			heap.Push(&ranks, candidate)
		} else if betterRank(candidate, ranks[0]) {
			ranks[0] = candidate
			heap.Fix(&ranks, 0)
		}
	}
	sort.Slice(ranks, func(i, j int) bool {
		return betterRank(ranks[i], ranks[j])
	})
	for _, rank := range ranks[:min(r.Limit, len(ranks))] {
		c := e.docs[rank.doc].chunk
		snippet, ranges := snippet(c.Text, query)
		hits = append(hits, Hit{c.Source, c.Path, append([]string{}, c.Heading...), snippet, ranges, rank.score, c.ID})
	}
	return hits, nil
}

type ranked struct {
	doc   int
	score float64
}

func betterRank(a, b ranked) bool { return a.score > b.score || a.score == b.score && a.doc < b.doc }

// The heap root is the worst retained hit; stable document order breaks ties.
type rankHeap []ranked

func (h rankHeap) Len() int           { return len(h) }
func (h rankHeap) Less(i, j int) bool { return betterRank(h[j], h[i]) }
func (h rankHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *rankHeap) Push(x any)        { *h = append(*h, x.(ranked)) }
func (h *rankHeap) Pop() any          { old := *h; x := old[len(old)-1]; *h = old[:len(old)-1]; return x }
func (e *Engine) Read(r ReadRequest) (Chunk, error) {
	if err := validateScope(r.PathPrefixes); err != nil {
		return Chunk{}, err
	}
	if err := e.available(); err != nil {
		return Chunk{}, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if r.ChunkID == "" && r.Path == "" {
		return Chunk{}, errors.New("chunk_id or path required")
	}
	ids := append([]string{}, r.Sources...)
	if r.Source != "" {
		ids = append(ids, r.Source)
	}
	allowed, err := e.allowed(ids, r.Personal)
	if err != nil {
		return Chunk{}, err
	}
	var found *Chunk
	for _, d := range e.docs {
		c := d.chunk
		if !allowed[c.Source] || !allowsPath(r.PathPrefixes, c.Source, c.Path) || (r.Source != "" && c.Source != r.Source) {
			continue
		}
		match := c.ID == r.ChunkID
		if r.ChunkID == "" {
			match = c.Path == r.Path && (r.Heading == nil || equalHeading(c.Heading, r.Heading))
		}
		if match {
			if found != nil {
				return Chunk{}, errors.New("ambiguous path/heading: use a chunk_id from search")
			}
			found = c
		}
	}
	if found == nil {
		return Chunk{}, errors.New("chunk not found or source not authorized")
	}
	c := *found
	c.Heading = append([]string{}, c.Heading...)
	return c, nil
}
func equalHeading(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func Tokens(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 }
func (e *Engine) Pack(r PackRequest) (Pack, error) {
	out := Pack{Citations: []Citation{}, Chunks: []Chunk{}}
	if err := e.available(); err != nil {
		return out, err
	}
	if r.BudgetTokens == 0 {
		r.BudgetTokens = 1500
	}
	if r.BudgetTokens < 1 || r.BudgetTokens > 8000 {
		return out, errors.New("budget_tokens must be between 1 and 8000")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	hits, err := e.searchLocked(SearchRequest{Query: r.Query, Sources: r.Sources, Personal: r.Personal, Limit: 100, PathPrefixes: r.PathPrefixes})
	if err != nil {
		return out, err
	}
	return e.packHitsLocked(r, hits), nil
}

// PackHits rechecks source authorization against the current snapshot.
func (e *Engine) PackHits(r PackRequest, hits []Hit) (Pack, error) {
	if err := validateScope(r.PathPrefixes); err != nil {
		return Pack{}, err
	}
	if err := e.available(); err != nil {
		return Pack{}, err
	}
	if r.BudgetTokens == 0 {
		r.BudgetTokens = 1500
	}
	if r.BudgetTokens < 1 || r.BudgetTokens > 8000 {
		return Pack{}, errors.New("budget_tokens must be between 1 and 8000")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	allowed, err := e.allowed(r.Sources, r.Personal)
	if err != nil {
		return Pack{}, err
	}
	filtered := []Hit{}
	for _, h := range hits {
		if allowed[h.Source] && allowsPath(r.PathPrefixes, h.Source, h.Path) {
			filtered = append(filtered, h)
		}
	}
	return e.packHitsLocked(r, filtered), nil
}
func (e *Engine) packHitsLocked(r PackRequest, hits []Hit) Pack {
	out := Pack{Citations: []Citation{}, Chunks: []Chunk{}}
	byID := map[string]*Chunk{}
	for _, d := range e.docs {
		byID[d.chunk.ID] = d.chunk
	}
	seen := map[string]bool{}
	text := "Context from the user's Brain:\n"
	for _, hit := range hits {
		c := byID[hit.ChunkID]
		if c == nil || c.Source != hit.Source || !allowsPath(r.PathPrefixes, c.Source, c.Path) {
			continue
		}
		normalized := strings.Join(strings.Fields(c.Text), " ")
		if seen[normalized] {
			continue
		}
		citation := c.Path
		for _, h := range c.Heading {
			if h != "" {
				citation += " › " + h
			}
		}
		section := "\n[" + c.Source + ": " + citation + "]\n" + c.Text + "\n"
		if Tokens(text+section) > r.BudgetTokens {
			continue
		}
		text += section
		seen[normalized] = true
		out.Citations = append(out.Citations, Citation{c.ID, c.Source, citation})
		copy := *c
		copy.Heading = append([]string{}, c.Heading...)
		out.Chunks = append(out.Chunks, copy)
	}
	if len(out.Chunks) > 0 {
		out.Text = text
		out.TokensUsed = Tokens(text)
	}
	return out
}
