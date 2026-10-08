package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testStorage struct {
	sources []Source
	index   []byte
	fail    bool
}

func (s *testStorage) LoadSources() ([]Source, error) { return s.sources, nil }
func (s *testStorage) SaveSources(v []Source) error {
	if s.fail {
		return errors.New("disk unavailable")
	}
	s.sources = v
	return nil
}
func (s *testStorage) LoadIndex() (Snapshot, error) {
	var v Snapshot
	if s.index != nil {
		err := json.Unmarshal(s.index, &v)
		return v, err
	}
	return v, nil
}
func (s *testStorage) SaveIndex(v Snapshot) error {
	var err error
	s.index, err = json.Marshal(v)
	return err
}
func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, files map[string]string, kind string) (*Engine, string, *testStorage) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		write(t, dir, name, text)
	}
	storage := &testStorage{}
	e, err := New(Options{Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(Source{ID: "notes", Label: "Notes", Path: dir, Kind: kind}); err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e, dir, storage
}
func search(t *testing.T, e *Engine, r SearchRequest) []Hit {
	t.Helper()
	hits, err := e.Search(r)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestChunking(t *testing.T) {
	text := "# Root\nIntro\n\n## Child\n" + strings.Repeat("été hello ", 400) + "\n```md\n# Not a heading\n```\n\nOther\n=====\nend"
	chunks := ChunkText("notes", "doc.md", text)
	if len(chunks) < 5 || !reflect.DeepEqual(chunks[1].Heading, []string{"Root", "Child"}) {
		t.Fatalf("headings: %+v", chunks)
	}
	for _, c := range chunks {
		if len([]rune(c.Text)) > 1200 {
			t.Fatal("oversize chunk")
		}
		if strings.Join(c.Heading, " ") == "Root Child Not a heading" {
			t.Fatal("code interpreted as heading")
		}
	}
	giant := ChunkText("notes", "giant.md", "# "+strings.Repeat("x", 200000))
	for _, c := range giant {
		if len(c.Heading) != 0 {
			t.Fatal("unbounded heading metadata")
		}
	}
	if !reflect.DeepEqual(chunks, ChunkText("notes", "doc.md", text)) {
		t.Fatal("unstable chunk IDs")
	}
	if got := chunks[len(chunks)-1].Heading; !reflect.DeepEqual(got, []string{"Other"}) {
		t.Fatalf("setext: %v", got)
	}
	// Sections overlap without splitting Unicode code points.
	long := ChunkText("n", "p.md", strings.Repeat("é", 2300))
	if long[0].Text[len(long[0].Text)-200:] != long[1].Text[:200] {
		t.Fatal("missing overlap")
	}
	codeTitle := ChunkText("n", "p.md", "# C#\nbody\n\t# indented code\n## Child ###\nchild")
	if len(codeTitle) != 2 || !reflect.DeepEqual(codeTitle[0].Heading, []string{"C#"}) || !reflect.DeepEqual(codeTitle[1].Heading, []string{"C#", "Child"}) {
		t.Fatalf("literal hashes or indented code headings: %+v", codeTitle)
	}
}

func TestProviderFileLimitAndEarlyStop(t *testing.T) {
	emitted := 0
	e, err := New(Options{Memory: func(ctx context.Context, emit func(Document) bool) error {
		for i := 0; i < MaxFiles+2; i++ {
			emitted++
			if !emit(Document{Path: fmt.Sprintf("%d.md", i), Text: "limitword"}) {
				break
			}
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err == nil {
		t.Fatal("file limit not reported")
	}
	for _, s := range e.Sources() {
		if s.ID == "memory" && (s.Files != MaxFiles || s.Error == "") {
			t.Fatalf("limit status: %+v", s)
		}
	}
	if emitted != MaxFiles+1 {
		t.Fatalf("provider did not stop: %d", emitted)
	}
	if hits := search(t, e, SearchRequest{Query: "limitword", Limit: 1000}); len(hits) != 100 {
		t.Fatalf("search limit: %d", len(hits))
	}
}
func TestRankingFoldingAndHighlights(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"a.md": "# Déploiement\nLe déploiement du café sécurisé.", "b.txt": strings.Repeat("ordinary background words ", 50) + "déploiement", "c.md": "# Other\nsecure cafe deployment"}, "context")
	hits := search(t, e, SearchRequest{Query: "deploiement"})
	if len(hits) != 2 || hits[0].Path != "a.md" || hits[0].Score <= hits[1].Score {
		t.Fatalf("ranking: %+v", hits)
	}
	hits = search(t, e, SearchRequest{Query: "CAFE"})
	if len(hits) != 2 {
		t.Fatalf("accents: %+v", hits)
	}
	for _, h := range hits {
		if len(h.Highlights) == 0 {
			t.Fatal("missing highlights")
		}
		for _, r := range h.Highlights {
			if fold(string([]rune(h.Snippet)[r.Start:r.End])) != "cafe" {
				t.Fatal("invalid Unicode range")
			}
		}
	}
	if !reflect.DeepEqual(terms("cœur ÉCOLE e\u0301cole learning learned"), []string{"coeur", "ecole", "learning", "learned"}) {
		t.Fatal("folding or unintended stemming")
	}
	plain := search(t, e, SearchRequest{Query: "cafe securise"})
	quoted := search(t, e, SearchRequest{Query: `"café sécurisé"`})
	if plain[0].ChunkID != quoted[0].ChunkID || quoted[0].Score <= plain[0].Score {
		t.Fatal("phrase not boosted")
	}
}
func TestPersonalExclusionAndRead(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"private.md": "# Private\nsecret personal unicorn"}, "personal")
	for _, r := range []SearchRequest{{Query: "unicorn"}, {Query: "unicorn", Personal: true}, {Query: "unicorn", Sources: []string{"notes"}}} {
		if len(search(t, e, r)) != 0 {
			t.Fatalf("leaked: %+v", r)
		}
	}
	hits := search(t, e, SearchRequest{Query: "unicorn", Sources: []string{"notes"}, Personal: true})
	if len(hits) != 1 {
		t.Fatal("opt-in failed")
	}
	for _, r := range []ReadRequest{{ChunkID: hits[0].ChunkID}, {ChunkID: hits[0].ChunkID, Personal: true}, {ChunkID: hits[0].ChunkID, Sources: []string{"notes"}}} {
		if _, err := e.Read(r); err == nil {
			t.Fatalf("read leaked: %+v", r)
		}
	}
	for _, r := range []ReadRequest{{ChunkID: hits[0].ChunkID, Personal: true, Sources: []string{"notes"}}, {Source: "notes", Path: "private.md", Heading: []string{"Private"}, Personal: true}} {
		if c, err := e.Read(r); err != nil || !strings.Contains(c.Text, "unicorn") {
			t.Fatalf("read: %+v %v", c, err)
		}
	}
	pack, err := e.Pack(PackRequest{Query: "unicorn", Personal: true})
	if err != nil || len(pack.Chunks) != 0 {
		t.Fatal("pack leaked")
	}
	pack, err = e.Pack(PackRequest{Query: "unicorn", Personal: true, Sources: []string{"notes"}})
	if err != nil || len(pack.Chunks) != 1 {
		t.Fatal("pack opt-in failed")
	}
}
func TestBudgetPackDedupAndReadAmbiguity(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"a.md": "# Alpha\nalpha text", "duplicate.md": "# Alpha\nalpha text", "large.md": "# Alpha\n" + strings.Repeat("alpha ", 400)}, "context")
	for _, budget := range []int{1, 30, 80, 1500, 8000} {
		pack, err := e.Pack(PackRequest{Query: "alpha", BudgetTokens: budget})
		if err != nil {
			t.Fatal(err)
		}
		if pack.TokensUsed > budget || pack.TokensUsed != Tokens(pack.Text) || len(pack.Citations) != len(pack.Chunks) {
			t.Fatalf("budget: %+v", pack)
		}
		seen := map[string]bool{}
		for _, c := range pack.Chunks {
			if seen[c.Text] {
				t.Fatal("duplicate chunk")
			}
			seen[c.Text] = true
		}
		if len(pack.Chunks) > 0 && !strings.HasPrefix(pack.Text, "Context from the user's Brain:\n") {
			t.Fatal("missing prefix")
		}
	}
	for _, b := range []int{-1, 8001} {
		if _, err := e.Pack(PackRequest{Query: "alpha", BudgetTokens: b}); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	if _, err := e.Read(ReadRequest{Source: "notes", Path: "large.md", Heading: []string{"Alpha"}}); err == nil {
		t.Fatal("ambiguous read silently picked a chunk")
	}
}
func TestSymlinksGlobsAndFileLimits(t *testing.T) {
	e, dir, _ := fixture(t, map[string]string{"docs/a.md": "wanted", "docs/sub/b.txt": "wanted", "other.md": "excluded", "docs/binary.txt": "wanted\x00binary", "docs/large.md": strings.Repeat("a", MaxFileBytes+1), "docs/node_modules/x.md": "excluded", ".git/x.md": "excluded", "vendor/x.md": "excluded", "build/x.md": "excluded", "dist/x.md": "excluded"}, "repo")
	outside := t.TempDir()
	write(t, outside, "secret.md", "secret escape")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "docs", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docs", "outside")); err != nil {
		t.Fatal(err)
	}
	if err := e.Update(Source{ID: "notes", Label: "Notes", Kind: "repo", Path: dir, Include: []string{"docs/**"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(search(t, e, SearchRequest{Query: "wanted"})) != 2 || len(search(t, e, SearchRequest{Query: "secret excluded"})) != 0 {
		t.Fatal("file selection or confinement failed")
	}
	if _, err := globRegex("../**"); err == nil {
		t.Fatal("escape glob accepted")
	}
	for _, p := range []string{"a.md", "sub/a.md", "sub/deep/a.md"} {
		re, err := globRegex("**/*.md")
		if err != nil || !re.MatchString(p) {
			t.Fatalf("recursive glob: %s %v", p, err)
		}
	}
}
func TestIncrementalRestartChangedRemovedAndFailedMutation(t *testing.T) {
	e, dir, storage := fixture(t, map[string]string{"a.md": "alpha", "b.md": "beta"}, "context")
	info, err := os.Stat(filepath.Join(dir, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Same stamp and size simulate an unchanged file: a restart must use the
	// saved text rather than reading it again. The next changed stamp rereads it.
	write(t, dir, "a.md", "gamma")
	if err = os.Chtimes(filepath.Join(dir, "a.md"), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	e, err = New(Options{Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(search(t, e, SearchRequest{Query: "alpha"})) != 1 {
		t.Fatal("unchanged file reread")
	}
	stamp := info.ModTime().Add(time.Second)
	if err = os.Chtimes(filepath.Join(dir, "a.md"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "b.md")); err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(search(t, e, SearchRequest{Query: "alpha beta"})) != 0 || len(search(t, e, SearchRequest{Query: "gamma"})) != 1 {
		t.Fatal("changed/removed files not reflected")
	}
	storage.fail = true
	if err = e.Remove("notes"); err == nil {
		t.Fatal("failed save ignored")
	}
	if len(search(t, e, SearchRequest{Query: "gamma"})) != 1 {
		t.Fatal("failed mutation lost data")
	}
	storage.fail = false
	if err = e.Remove("notes"); err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	e, err = New(Options{Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	if len(search(t, e, SearchRequest{Query: "gamma"})) != 0 {
		t.Fatal("removed source resurrected")
	}
}
func TestBuiltinsNotPersistedAndAvailability(t *testing.T) {
	storage := &testStorage{}
	locked := false
	provider := func(ctx context.Context, emit func(Document) bool) error {
		emit(Document{Path: "page.md", Text: "unicorn private memory"})
		return nil
	}
	e, err := New(Options{Storage: storage, Memory: provider, Conversations: provider, Available: func() error {
		if locked {
			return errors.New("locked")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits := search(t, e, SearchRequest{Query: "unicorn"})
	if len(hits) != 2 {
		t.Fatal("builtins missing")
	}
	if strings.Contains(string(storage.index), "unicorn") {
		t.Fatal("builtin text persisted")
	}
	if e.Remove("memory") == nil || e.Update(Source{ID: "conversations", Label: "x", Kind: "context", Path: t.TempDir()}) == nil {
		t.Fatal("builtin mutated")
	}
	locked = true
	if _, err = e.Search(SearchRequest{Query: "unicorn"}); err == nil {
		t.Fatal("locked index leaked")
	}
	if _, err = e.Pack(PackRequest{Query: "unicorn"}); err == nil {
		t.Fatal("locked pack leaked")
	}
	if _, err = e.Read(ReadRequest{ChunkID: hits[0].ChunkID}); err == nil {
		t.Fatal("locked chunk leaked")
	}
}

func TestSourceScopeInvalidatesPersistedCache(t *testing.T) {
	e, dir, storage := fixture(t, map[string]string{"a.md": "oldcontext"}, "personal")
	if err := e.Update(Source{ID: "notes", Label: "Changed", Path: dir, Kind: "context", Include: []string{"docs/**"}}); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(Options{Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	if len(search(t, restarted, SearchRequest{Query: "oldcontext"})) != 0 {
		t.Fatal("old personal scope resurrected from cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = e.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
func TestMCPToolsInProcess(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"private.md": "# Private\nunicorn"}, "personal")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := MCPServer(e)
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 4 {
		t.Fatalf("tools: %v %v", list, err)
	}
	for _, tool := range list.Tools {
		if !tool.Annotations.ReadOnlyHint {
			t.Fatal("tool not marked read-only")
		}
	}
	call := func(name string, args any) *mcp.CallToolResult {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	decode := func(r *mcp.CallToolResult, target any) {
		t.Helper()
		if r.IsError {
			t.Fatalf("tool error: %+v", r)
		}
		b, err := json.Marshal(r.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, target); err != nil {
			t.Fatal(err)
		}
	}
	var result SearchResult
	decode(call("brain_search", SearchRequest{Query: "unicorn"}), &result)
	if len(result.Hits) != 0 {
		t.Fatal("MCP default leaked")
	}
	decode(call("brain_search", SearchRequest{Query: "unicorn", Personal: true, Sources: []string{"notes"}}), &result)
	if len(result.Hits) != 1 {
		t.Fatal("MCP opt-in failed")
	}
	if !call("brain_read", ReadRequest{ChunkID: result.Hits[0].ChunkID, Personal: true}).IsError {
		t.Fatal("MCP known ID leaked")
	}
	var chunk Chunk
	decode(call("brain_read", ReadRequest{ChunkID: result.Hits[0].ChunkID, Personal: true, Source: "notes"}), &chunk)
	var pack Pack
	decode(call("brain_pack", PackRequest{Query: "unicorn", Personal: true, Sources: []string{"notes"}, BudgetTokens: 100}), &pack)
	if len(pack.Citations) != 1 || pack.TokensUsed > 100 || !strings.Contains(chunk.Text, "unicorn") {
		t.Fatal("MCP pack/read mismatch")
	}
}
