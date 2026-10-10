// Package retrieval contains offline fixture/measurement helpers used only by
// tests. Sharing these keeps the evidence definition identical at every stage;
// Loom's binary does not import this package. It owns no runtime or model path.
package retrieval

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

const Repetitions = 30

var Budgets = []int{512, 1500, 3000}
var Ks = []int{1, 5, 10, 20}
var Clock = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type Document struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Text   string `json:"text"`
}
type Case struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Query        string              `json:"query"`
	QueryTime    string              `json:"query_time,omitempty"`
	Documents    []Document          `json:"documents"`
	Replacements []Document          `json:"replacements,omitempty"`
	Sources      []string            `json:"sources,omitempty"`
	Paths        map[string][]string `json:"path_prefixes,omitempty"`
	MemoryBefore string              `json:"memory_before,omitempty"`
	MemoryAfter  string              `json:"memory_after,omitempty"`
}
type Span struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Text    string `json:"text"`
	Session string `json:"session"`
	Turn    int    `json:"turn,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
}
type Gold struct {
	Spans          []Span `json:"spans"`
	Sessions       []Span `json:"sessions"`
	Current        []Span `json:"current,omitempty"`
	Stale          []Span `json:"stale,omitempty"`
	QueryKind      string `json:"query_kind,omitempty"`
	TemporalReview string `json:"temporal_review,omitempty"`
}
type Corpus struct {
	Cases    []Case
	Evidence map[string]Gold
	Manifest json.RawMessage
	Digest   string
	Label    string
}

func Load(dir string) (Corpus, error) {
	c := Corpus{Label: "synthetic-only"}
	if mode := os.Getenv("LOOM_BRAIN_RETRIEVAL_MODE"); mode != "" && mode != "lexical" {
		return c, errors.New("only lexical mode is supported; no embeddings/model calls")
	}
	var err error
	c.Manifest, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return c, err
	}
	var manifest struct {
		Files map[string]string `json:"files_sha256"`
	}
	if err = json.Unmarshal(c.Manifest, &manifest); err != nil {
		return c, err
	}
	for name, digest := range manifest.Files {
		if name != "cases.jsonl" && name != "evidence.json" && name != "temporal.jsonl" {
			return c, errors.New("unexpected fixture hash target")
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return c, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != digest {
			return c, fmt.Errorf("fixture digest mismatch: %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "evidence.json"))
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c.Evidence); err != nil {
		return c, err
	}
	hash := sha256.New()
	hash.Write(c.Manifest)
	hash.Write(data)
	for _, name := range []string{"temporal.jsonl", "cases.jsonl"} {
		data, err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return c, err
		}
		hash.Write(data)
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		scanner.Buffer(make([]byte, 4096), 64<<20)
		for scanner.Scan() {
			var item Case
			if err = json.Unmarshal(scanner.Bytes(), &item); err != nil {
				return c, err
			}
			c.Cases = append(c.Cases, item)
			if name == "cases.jsonl" {
				c.Label = "synthetic+LongMemEval-cleaned-S"
			}
		}
		if err = scanner.Err(); err != nil {
			return c, err
		}
	}
	c.Digest = hex.EncodeToString(hash.Sum(nil))
	seen := map[string]bool{}
	for _, item := range c.Cases {
		if item.ID == "" || seen[item.ID] {
			return c, errors.New("empty/duplicate case ID")
		}
		seen[item.ID] = true
		g, ok := c.Evidence[item.ID]
		if !ok {
			return c, fmt.Errorf("missing sidecar for %s", item.ID)
		}
		if len(g.Spans) == 0 && item.Type != "abstention" && !strings.HasSuffix(item.ID, "_abs") {
			return c, fmt.Errorf("missing supporting spans: %s", item.ID)
		}
	}
	return c, nil
}

func FixtureDir(defaultDir string) string {
	if dir := os.Getenv("LOOM_BRAIN_RETRIEVAL_CASES"); dir != "" {
		return dir
	}
	return defaultDir
}

// Build uses the production folder/provider ingestion and refuses any silent
// limit truncation. Replacement is an actual file edit followed by Refresh.
func Build(root string, c Case) (*brain.Engine, []Document, time.Duration, error) {
	docs := append([]Document(nil), c.Documents...)
	provider := func(source string) brain.Provider {
		return func(_ context.Context, emit func(brain.Document) bool) error {
			for _, d := range docs {
				if d.Source == source && !emit(brain.Document{Path: d.Path, Text: d.Text}) {
					return errors.New("provider corpus truncated")
				}
			}
			return nil
		}
	}
	e, err := brain.New(brain.Options{Conversations: provider("conversations"), Memory: provider("memory"), Distilled: provider("distilled")})
	if err != nil {
		return nil, nil, 0, err
	}
	folders := map[string]string{}
	write := func(d Document, clock time.Time) error {
		if len(d.Text) > brain.MaxFileBytes {
			return errors.New("oversized document; no trimming")
		}
		if d.Source == "conversations" || d.Source == "memory" || d.Source == "distilled" {
			return nil
		}
		if d.Source != "vault" {
			return errors.New("fixture folder source must be vault")
		}
		if !filepath.IsLocal(d.Path) {
			return errors.New("non-local fixture path")
		}
		dir := filepath.Join(root, d.Source)
		folders[d.Source] = dir
		file := filepath.Join(dir, filepath.FromSlash(d.Path))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(d.Text), 0600); err != nil {
			return err
		}
		return os.Chtimes(file, clock, clock)
	}
	for _, d := range docs {
		if err = write(d, Clock); err != nil {
			return nil, nil, 0, err
		}
	}
	for source, dir := range folders {
		if err = e.Update(brain.Source{ID: source, Label: "Fixture", Kind: "context", Path: dir}); err != nil {
			return nil, nil, 0, err
		}
	}
	start := time.Now()
	if err = e.Refresh(context.Background()); err != nil {
		return nil, nil, 0, err
	}
	for _, replacement := range c.Replacements {
		found := false
		for i, d := range docs {
			if d.Source == replacement.Source && d.Path == replacement.Path {
				docs[i] = replacement
				found = true
			}
		}
		if !found {
			return nil, nil, 0, errors.New("replacement target missing")
		}
		if err = write(replacement, Clock.Add(time.Hour)); err != nil {
			return nil, nil, 0, err
		}
	}
	if len(c.Replacements) > 0 {
		if err = e.Refresh(context.Background()); err != nil {
			return nil, nil, 0, err
		}
	}
	elapsed := time.Since(start)
	chunks, err := e.Chunks(brain.SearchRequest{})
	if err != nil {
		return nil, nil, 0, err
	}
	expected := 0
	for _, d := range docs {
		expected += len(brain.ChunkText(d.Source, d.Path, d.Text))
	}
	if len(chunks) != expected {
		return nil, nil, 0, fmt.Errorf("corpus truncated: got %d chunks, expected %d", len(chunks), expected)
	}
	return e, docs, elapsed, nil
}

type Coverage struct {
	Covered  int      `json:"covered"`
	Total    int      `json:"total"`
	Recall   *float64 `json:"recall"` // null for abstention, never a fabricated zero.
	Complete *bool    `json:"complete"`
}

func coverage(covered, total int) Coverage {
	c := Coverage{Covered: covered, Total: total}
	if total > 0 {
		r := float64(covered) / float64(total)
		all := covered == total
		c.Recall = &r
		c.Complete = &all
	}
	return c
}

// Coverage maps production chunk texts onto original document byte intervals.
// Every non-whitespace byte of the full supporting turn must be covered. This
// handles overlapping chunks, repeated text and evidence split across chunks;
// matching a short answer string, snippet or citation is insufficient.
func Cover(spans []Span, docs []Document, chunks []brain.Chunk, sessions bool) Coverage {
	covered := 0
	for _, span := range spans {
		if sessions {
			found := false
			for _, chunk := range chunks {
				if chunk.Source == span.Source && chunk.Path == span.Path {
					found = true
				}
			}
			if found {
				covered++
			}
			continue
		}
		var text string
		for _, d := range docs {
			if d.Source == span.Source && d.Path == span.Path {
				text = d.Text
				break
			}
		}
		start, end := span.Start, span.End
		if end == 0 {
			start = strings.Index(text, span.Text)
			end = start + len(span.Text)
		}
		if start < 0 || end > len(text) || end <= start || text[start:end] != span.Text {
			continue
		}
		mask := make([]bool, len(text))
		selected := map[string]bool{}
		for _, chunk := range chunks {
			selected[chunk.ID] = true
		}
		position := 0
		for _, chunk := range brain.ChunkText(span.Source, span.Path, text) {
			at := strings.Index(text[position:], chunk.Text)
			if at < 0 {
				continue
			}
			at += position
			// The production chunker overlaps 100 runes, and trims edge
			// whitespace. Begin the next mapping in that overlap, rather than
			// searching from the previous start (ambiguous in repeated prose).
			endOfChunk := at + len(chunk.Text)
			prefixRunes := []rune(text[:endOfChunk])
			position = len(string(prefixRunes[:max(0, len(prefixRunes)-100)]))
			if selected[chunk.ID] {
				for i := at; i < at+len(chunk.Text); i++ {
					mask[i] = true
				}
			}
		}
		complete := true
		for i := start; i < end; i++ {
			if !mask[i] && !strings.ContainsRune(" \t\r\n", rune(text[i])) {
				complete = false
				break
			}
		}
		if complete {
			covered++
		}
	}
	return coverage(covered, len(spans))
}

func Present(spans []Span, docs []Document, all []brain.Chunk, text string) Coverage {
	present := []brain.Chunk{}
	pathsByText := map[string]map[string]bool{}
	for _, chunk := range all {
		if pathsByText[chunk.Text] == nil {
			pathsByText[chunk.Text] = map[string]bool{}
		}
		pathsByText[chunk.Text][chunk.Source+"/"+chunk.Path] = true
	}
	for _, chunk := range all {
		// Duplicate units need source provenance. A single retained duplicate
		// must not inflate session/span recall after PackHits deduplication.
		citation := "[" + chunk.Source + ": " + chunk.Path
		attributed := strings.Contains(text, citation+"]") || strings.Contains(text, citation+" › ")
		if strings.Contains(text, chunk.Text) && (len(pathsByText[chunk.Text]) == 1 || attributed) {
			present = append(present, chunk)
		}
	}
	return Cover(spans, docs, present, false)
}

type Latency struct {
	P50US   float64 `json:"p50_us"`
	P95US   float64 `json:"p95_us"`
	Samples int     `json:"samples"`
}

func Percentiles(samples []time.Duration) Latency {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	if len(samples) == 0 {
		return Latency{}
	}
	return Latency{float64(samples[(len(samples)-1)/2]) / 1000, float64(samples[(len(samples)*95+99)/100-1]) / 1000, len(samples)}
}

type Row struct {
	ID                   string              `json:"id"`
	Type                 string              `json:"question_type"`
	Route                string              `json:"route"`
	Budget               int                 `json:"budget"`
	Scope                brain.SearchRequest `json:"scope"`
	QueryTime            string              `json:"query_time,omitempty"`
	CorpusBytes          int                 `json:"corpus_bytes"`
	CorpusChunks         int                 `json:"corpus_chunks"`
	CorpusHash           string              `json:"corpus_sha256"`
	Recall               map[int]Coverage    `json:"span_recall_at"`
	Sessions             map[int]Coverage    `json:"session_recall_at"`
	Packed               Coverage            `json:"packed"`
	Retrieved            Coverage            `json:"retrieved_top100"`
	Assembled            *Coverage           `json:"assembled,omitempty"`
	Final                *Coverage           `json:"final,omitempty"`
	MissingStage         string              `json:"missing_stage,omitempty"`
	CurrentRank          int                 `json:"current_rank"`
	StaleRank            int                 `json:"stale_rank"`
	TemporalReview       string              `json:"temporal_review,omitempty"`
	QueryKind            string              `json:"query_kind,omitempty"`
	AsOfEvidenceCorrect  *bool               `json:"as_of_evidence_correct,omitempty"`
	Current              Coverage            `json:"current_support"`
	StaleTranscript      Coverage            `json:"stale_transcript"`
	StaleMemory          Coverage            `json:"stale_authoritative_memory"`
	UnsupportedRetrieved int                 `json:"unsupported_retrieved"`
	UnsupportedPacked    int                 `json:"unsupported_packed"`
	ScopeLeaks           int                 `json:"scope_leaks"`
	InjectionTokens      int                 `json:"injection_tokens"`
	PreparedTokens       int                 `json:"prepared_tokens"`
	IndexUS              float64             `json:"index_refresh_us"`
	SearchLatency        Latency             `json:"search_latency"`
	PackLatency          Latency             `json:"pack_latency"`
	AssemblyLatency      Latency             `json:"assembly_latency"`
}

func rank(spans []Span, docs []Document, chunks []brain.Chunk) int {
	if len(spans) == 0 {
		return 0
	}
	for i := range chunks {
		if Cover(spans, docs, chunks[:i+1], false).Covered > 0 {
			return i + 1
		}
	}
	return 0
}
func Measure(e *brain.Engine, c Case, g Gold, docs []Document, budget int, index time.Duration) (Row, []brain.Chunk, brain.Pack, error) {
	r := Row{ID: c.ID, Type: c.Type, Route: "retrieval", Budget: budget, Scope: brain.SearchRequest{Query: c.Query, Sources: c.Sources, PathPrefixes: c.Paths, Limit: 100}, QueryTime: c.QueryTime, Recall: map[int]Coverage{}, Sessions: map[int]Coverage{}, IndexUS: float64(index) / 1000, TemporalReview: g.TemporalReview, QueryKind: g.QueryKind}
	all, err := e.Chunks(brain.SearchRequest{})
	if err != nil {
		return r, nil, brain.Pack{}, err
	}
	r.CorpusChunks = len(all)
	hash := sha256.New()
	for _, d := range docs {
		r.CorpusBytes += len(d.Text)
		data, _ := json.Marshal(d)
		hash.Write(data)
	}
	r.CorpusHash = hex.EncodeToString(hash.Sum(nil))
	var hits []brain.Hit
	var pack brain.Pack
	searchTimes, packTimes := []time.Duration{}, []time.Duration{}
	request := brain.PackRequest{Query: c.Query, Sources: c.Sources, PathPrefixes: c.Paths, BudgetTokens: budget}
	for i := 0; i <= Repetitions; i++ { // First call warms caches and is excluded.
		start := time.Now()
		hits, err = e.Search(r.Scope)
		elapsed := time.Since(start)
		if err != nil {
			return r, nil, pack, err
		}
		start = time.Now()
		pack, err = e.PackHits(request, hits)
		packing := time.Since(start)
		if err != nil {
			return r, nil, pack, err
		}
		if i > 0 {
			searchTimes = append(searchTimes, elapsed)
			packTimes = append(packTimes, packing)
		}
	}
	r.SearchLatency = Percentiles(searchTimes)
	r.PackLatency = Percentiles(packTimes)
	r.InjectionTokens = brain.Tokens(pack.Text)
	byID := map[string]brain.Chunk{}
	for _, chunk := range all {
		byID[chunk.ID] = chunk
	}
	retrieved := []brain.Chunk{}
	for _, h := range hits {
		retrieved = append(retrieved, byID[h.ChunkID])
	}
	for _, k := range Ks {
		limit := min(k, len(retrieved))
		r.Recall[k] = Cover(g.Spans, docs, retrieved[:limit], false)
		r.Sessions[k] = Cover(g.Sessions, docs, retrieved[:limit], true)
	}
	r.Retrieved = Cover(g.Spans, docs, retrieved, false)
	r.Packed = Cover(g.Spans, docs, pack.Chunks, false)
	r.CurrentRank = rank(g.Current, docs, retrieved)
	r.StaleRank = rank(g.Stale, docs, retrieved)
	r.Current = Cover(g.Current, docs, pack.Chunks, false)
	transcript, memory := []Span{}, []Span{}
	for _, s := range g.Stale {
		if s.Source == "conversations" {
			transcript = append(transcript, s)
		} else {
			memory = append(memory, s)
		}
	}
	r.StaleTranscript = Cover(transcript, docs, pack.Chunks, false)
	r.StaleMemory = Cover(memory, docs, pack.Chunks, false)
	if g.QueryKind == "as-of" && strings.HasPrefix(g.TemporalReview, "reviewed") && len(g.Current) > 0 {
		correct := r.Current.Complete != nil && *r.Current.Complete && r.CurrentRank > 0 && (r.StaleRank == 0 || r.CurrentRank < r.StaleRank) && r.StaleMemory.Covered == 0
		r.AsOfEvidenceCorrect = &correct
	}
	if len(g.Spans) == 0 {
		r.UnsupportedRetrieved = len(retrieved)
		r.UnsupportedPacked = len(pack.Chunks)
	}
	for _, chunk := range append(append([]brain.Chunk{}, retrieved...), pack.Chunks...) {
		if prefixes, ok := c.Paths[chunk.Source]; ok {
			allowed := false
			for _, p := range prefixes {
				if chunk.Path == p || strings.HasPrefix(chunk.Path, strings.TrimSuffix(p, "/")+"/") {
					allowed = true
				}
			}
			if !allowed {
				r.ScopeLeaks++
			}
		}
	}
	return r, all, pack, nil
}

type Aggregate struct {
	Cases                    int             `json:"cases"`
	Runs                     int             `json:"runs,omitempty"`
	Recall5                  float64         `json:"recall5"`
	FinalPresence            *float64        `json:"final_presence"`
	SpanRecallAt             map[int]float64 `json:"span_recall_at,omitempty"`
	SessionRecallAt          map[int]float64 `json:"session_recall_at,omitempty"`
	CompleteAt               map[int]float64 `json:"complete_evidence_at,omitempty"`
	PackedRecall             *float64        `json:"packed_recall,omitempty"`
	MeanInjectionTokens      float64         `json:"mean_injection_tokens,omitempty"`
	MeanPreparedTokens       float64         `json:"mean_prepared_tokens,omitempty"`
	MeanUnsupportedRetrieved float64         `json:"mean_unsupported_retrieved,omitempty"`
	MeanUnsupportedPacked    float64         `json:"mean_unsupported_packed,omitempty"`
}

// Abstention summaries retain a null denominator as well as per-case rows.
func (a Aggregate) MarshalJSON() ([]byte, error) {
	type fields Aggregate
	var recall *float64
	if a.Cases > 0 {
		recall = &a.Recall5
	}
	return json.Marshal(struct {
		fields
		Recall *float64 `json:"recall5"`
	}{fields(a), recall})
}

type Report struct {
	Schema        int                  `json:"schema"`
	Mode          string               `json:"mode"`
	Dataset       string               `json:"dataset"`
	FixtureHash   string               `json:"fixture_sha256"`
	Manifest      json.RawMessage      `json:"manifest"`
	Commit        string               `json:"commit"`
	ModelHash     *string              `json:"embedding_model_sha256"`
	Rows          []Row                `json:"rows"`
	Macro         map[string]Aggregate `json:"macro"`
	FrozenUpdates []FrozenUpdate       `json:"frozen_updates,omitempty"`
	WindowFit     *WindowObservation   `json:"window_fit,omitempty"`
}

type WindowObservation struct {
	CaseID           string   `json:"case_id"`
	HistoryBeforeFit Coverage `json:"history_before_fit"`
	Final            Coverage `json:"final"`
	Omitted          int      `json:"omitted_messages"`
	PreparedTokens   int      `json:"prepared_tokens"`
	AssemblyLatency  Latency  `json:"assembly_latency"`
}

type FrozenUpdate struct {
	Route                    string  `json:"route"`
	Budget                   int     `json:"budget"`
	Stage                    string  `json:"stage"`
	CurrentPresent           bool    `json:"current_present"`
	StaleAuthoritativeMemory bool    `json:"stale_authoritative_memory"`
	PreparedTokens           int     `json:"prepared_tokens"`
	AssemblyLatency          Latency `json:"assembly_latency"`
}

func NewReport(c Corpus) Report {
	commit, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	return Report{Schema: 1, Mode: "lexical", Dataset: c.Label, FixtureHash: c.Digest, Manifest: c.Manifest, Commit: strings.TrimSpace(string(commit)), Rows: []Row{}, Macro: map[string]Aggregate{}}
}
func (r *Report) Summarize() {
	type total struct {
		n                                       int
		runs                                    int
		recall                                  float64
		final                                   float64
		finalN                                  int
		span, session, complete                 map[int]float64
		packed                                  float64
		injection, prepared                     float64
		unsupportedRetrieved, unsupportedPacked float64
	}
	totals := map[string]*total{}
	for _, row := range r.Rows {
		for _, typ := range []string{"all", row.Type} {
			key := fmt.Sprintf("%s/%d/%s", row.Route, row.Budget, typ)
			if totals[key] == nil {
				totals[key] = &total{span: map[int]float64{}, session: map[int]float64{}, complete: map[int]float64{}}
			}
			s := totals[key]
			s.runs++
			s.injection += float64(row.InjectionTokens)
			s.prepared += float64(row.PreparedTokens)
			s.unsupportedRetrieved += float64(row.UnsupportedRetrieved)
			s.unsupportedPacked += float64(row.UnsupportedPacked)
			if recall := row.Recall[5].Recall; recall != nil {
				s.n++
				s.recall += *recall
				for _, k := range Ks {
					if v := row.Recall[k].Recall; v != nil {
						s.span[k] += *v
					}
					if v := row.Sessions[k].Recall; v != nil {
						s.session[k] += *v
					}
					if v := row.Recall[k].Complete; v != nil && *v {
						s.complete[k]++
					}
				}
				if v := row.Packed.Recall; v != nil {
					s.packed += *v
				}
			}
			if row.Final != nil && row.Final.Complete != nil {
				s.finalN++
				if *row.Final.Complete {
					s.final++
				}
			}
		}
	}
	for key, s := range totals {
		a := Aggregate{Cases: s.n, Runs: s.runs, MeanInjectionTokens: s.injection / float64(s.runs), MeanPreparedTokens: s.prepared / float64(s.runs), MeanUnsupportedRetrieved: s.unsupportedRetrieved / float64(s.runs), MeanUnsupportedPacked: s.unsupportedPacked / float64(s.runs)}
		if s.n > 0 {
			a.Recall5 = s.recall / float64(s.n)
			a.SpanRecallAt = s.span
			a.SessionRecallAt = s.session
			a.CompleteAt = s.complete
			for _, k := range Ks {
				a.SpanRecallAt[k] /= float64(s.n)
				a.SessionRecallAt[k] /= float64(s.n)
				a.CompleteAt[k] /= float64(s.n)
			}
			packed := s.packed / float64(s.n)
			a.PackedRecall = &packed
		}
		if s.finalN > 0 {
			v := s.final / float64(s.finalN)
			a.FinalPresence = &v
		}
		r.Macro[key] = a
	}
}

func (r Report) Summary() string {
	keys := []string{}
	for key := range r.Macro {
		if strings.HasSuffix(key, "/1500/all") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := []string{r.Dataset + ", lexical, zero model calls"}
	for _, key := range keys {
		a := r.Macro[key]
		part := fmt.Sprintf("%s Recall@5=%.3f (%d evidence cases)", key, a.Recall5, a.Cases)
		if a.FinalPresence != nil {
			part += fmt.Sprintf(", final complete=%.3f", *a.FinalPresence)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}
func (r *Report) Write(path string) error {
	r.Summarize()
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

type Baseline struct {
	FixtureHash string               `json:"fixture_sha256"`
	Tolerance   float64              `json:"tolerance"`
	Rows        map[string]Aggregate `json:"rows"`
	Macro       map[string]Aggregate `json:"macro"`
}

func Key(r Row) string { return fmt.Sprintf("%s/%d/%s", r.Route, r.Budget, r.ID) }
func Gate(report Report, b Baseline) error {
	if len(report.Rows) == 0 {
		return errors.New("empty evidence run")
	}
	if b.FixtureHash != report.FixtureHash {
		return errors.New("fixture digest changed; baseline needs explicit review")
	}
	if b.Tolerance < 0 || b.Tolerance > .1 {
		return errors.New("invalid baseline tolerance")
	}
	seen := map[string]bool{}
	for _, row := range report.Rows {
		key := Key(row)
		seen[key] = true
		prior, ok := b.Rows[key]
		if !ok {
			return fmt.Errorf("baseline missing %s", key)
		}
		if row.ScopeLeaks > 0 {
			return fmt.Errorf("scope leakage: %s", key)
		}
		if prior.Cases > 0 && row.Recall[5].Recall == nil {
			return fmt.Errorf("missing Recall@5 denominator: %s", key)
		}
		if row.QueryKind == "current" && row.TemporalReview == "reviewed-synthetic" && row.StaleMemory.Covered > 0 {
			return fmt.Errorf("stale authoritative memory: %s", key)
		}
		if recall := row.Recall[5].Recall; recall != nil && *recall+b.Tolerance < prior.Recall5 {
			return fmt.Errorf("Recall@5 regression: %s", key)
		}
		if prior.FinalPresence != nil && (row.Final == nil || row.Final.Complete == nil || !*row.Final.Complete && *prior.FinalPresence > b.Tolerance) {
			return fmt.Errorf("final-context regression: %s", key)
		}
	}
	// Reports are stage-specific. Every expected row for their routes must exist.
	routes := map[string]bool{}
	for _, r := range report.Rows {
		routes[r.Route] = true
	}
	for key := range b.Rows {
		route, _, _ := strings.Cut(key, "/")
		if routes[route] && !seen[key] {
			return fmt.Errorf("missing baseline row: %s", key)
		}
	}
	for key, a := range report.Macro {
		prior, ok := b.Macro[key]
		if !ok {
			return fmt.Errorf("baseline missing category %s", key)
		}
		if a.Cases != prior.Cases || a.Recall5+b.Tolerance < prior.Recall5 {
			return fmt.Errorf("category regression: %s", key)
		}
		if prior.FinalPresence != nil && (a.FinalPresence == nil || *a.FinalPresence+b.Tolerance < *prior.FinalPresence) {
			return fmt.Errorf("category final regression: %s", key)
		}
	}
	for key := range b.Macro {
		route, _, _ := strings.Cut(key, "/")
		if routes[route] {
			if _, ok := report.Macro[key]; !ok {
				return fmt.Errorf("missing baseline category: %s", key)
			}
		}
	}
	return nil
}
