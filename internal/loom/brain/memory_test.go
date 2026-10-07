package brain

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func memoryFixture(t *testing.T) (*MemoryStore, MemoryStoreOptions) {
	t.Helper()
	opts := MemoryStoreOptions{Dir: t.TempDir(), Base: ".loom"}
	return NewMemoryStore(opts), opts
}
func memoryRequest(text string) RememberRequest {
	return RememberRequest{Class: "semantic", Scope: "global", Text: text, Provenance: MemoryProvenance{Kind: "user"}}
}
func mustRemember(t *testing.T, s *MemoryStore, req RememberRequest) MemoryItem {
	t.Helper()
	item, err := s.Remember(req)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func memoryFile(opts MemoryStoreOptions, item MemoryItem) string {
	return filepath.Join(opts.Dir, opts.Base, "memory", item.Class, item.ID+".md")
}
func mustList(t *testing.T, s *MemoryStore, filter MemoryFilter) MemoryList {
	t.Helper()
	out, err := s.List(filter)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMemoryFrontmatterRoundTrip(t *testing.T) {
	for _, class := range memoryClasses {
		t.Run(class, func(t *testing.T) {
			s, opts := memoryFixture(t)
			zero := 0.0
			index := 0
			req := memoryRequest("# Durable\n\nUTF-8: café\n---\n")
			req.Class, req.Scope, req.Tags = class, "project:alpha", []string{"first", "quoted: tag"}
			req.Importance, req.Confidence = &zero, &zero
			req.Provenance = MemoryProvenance{Kind: "discussion", DiscussionID: "discuss", MessageIndex: &index, Agent: "agent", Note: "why: true\ncontinued"}
			req.Supersedes = []string{"prior"}
			item := mustRemember(t, s, req)
			raw, err := os.ReadFile(memoryFile(opts, item))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := unmarshalMemory(raw)
			if err != nil || !reflect.DeepEqual(decoded, item) {
				t.Fatalf("roundtrip: %+v %+v %v", item, decoded, err)
			}
			header, _, _ := strings.Cut(string(raw)[4:], "\n---\n")
			var fields map[string]any
			if err := yaml.Unmarshal([]byte(header), &fields); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"id", "class", "scope", "tags", "importance", "confidence", "created_at", "updated_at", "last_used_at", "provenance", "supersedes", "status"} {
				if _, ok := fields[field]; !ok {
					t.Errorf("missing %s", field)
				}
			}
			if _, ok := fields["text"]; ok {
				t.Fatal("text belongs in body")
			}
			restarted := NewMemoryStore(opts)
			if got := mustList(t, restarted, MemoryFilter{}); len(got.Items) != 1 || !reflect.DeepEqual(got.Items[0], item) {
				t.Fatalf("restart: %+v", got)
			}
			raw, err = os.ReadFile(filepath.Join(opts.Dir, ".loom", "brain.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(raw, &fields); err != nil || fields["format"] != 1 || fields["created_at"] == nil || fields["imported_distilled"] != true {
				t.Fatalf("metadata: %s %v", raw, err)
			}
		})
	}
}
func TestMemoryValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*RememberRequest)
	}{
		{"class", func(r *RememberRequest) { r.Class = "invented" }},
		{"scope-prefix", func(r *RememberRequest) { r.Scope = "group:alpha" }},
		{"scope-empty-id", func(r *RememberRequest) { r.Scope = "project:" }},
		{"scope-newline", func(r *RememberRequest) { r.Scope = "task:x\ny" }},
		{"empty-text", func(r *RememberRequest) { r.Text = " \n" }},
		{"oversize", func(r *RememberRequest) { r.Text = strings.Repeat("x", (8<<10)+1) }},
		{"utf8", func(r *RememberRequest) { r.Text = "\xff" }},
		{"null", func(r *RememberRequest) { r.Text = "x\x00y" }},
		{"id", func(r *RememberRequest) { r.ID = "../../escape" }},
		{"importance", func(r *RememberRequest) { v := 1.1; r.Importance = &v }},
		{"confidence", func(r *RememberRequest) { v := -.1; r.Confidence = &v }},
		{"nan", func(r *RememberRequest) { v := math.NaN(); r.Importance = &v }},
		{"infinite", func(r *RememberRequest) { v := math.Inf(1); r.Confidence = &v }},
		{"status", func(r *RememberRequest) { r.Status = "deleted" }},
		{"provenance", func(r *RememberRequest) { r.Provenance.Kind = "model-guess" }},
		{"message-index", func(r *RememberRequest) { v := -1; r.Provenance.MessageIndex = &v }},
		{"supersedes", func(r *RememberRequest) { r.Supersedes = []string{"../unsafe"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := memoryFixture(t)
			req := memoryRequest("text")
			tc.mutate(&req)
			if _, err := s.Remember(req); err == nil {
				t.Fatal("accepted invalid memory")
			}
		})
	}
	for _, scope := range []string{"global", "project:x", "machine:x", "agent:x", "task:x"} {
		t.Run(scope, func(t *testing.T) {
			s, _ := memoryFixture(t)
			r := memoryRequest("text")
			r.Scope = scope
			mustRemember(t, s, r)
		})
	}
}
func TestMemoryDedupeAndHistory(t *testing.T) {
	s, opts := memoryFixture(t)
	first := mustRemember(t, s, memoryRequest("  A Durable\nFact "))
	if first.Importance != .5 || first.Confidence != .7 {
		t.Fatal("creation defaults")
	}
	req := memoryRequest("a durable fact")
	high := .9
	req.Importance = &high
	same := mustRemember(t, s, req)
	if same.ID != first.ID || same.UpdatedAt <= first.UpdatedAt || same.Importance != .9 || same.Text != first.Text || same.CreatedAt != first.CreatedAt {
		t.Fatalf("dedupe: %+v", same)
	}
	low := .1
	req.Importance = &low
	if item := mustRemember(t, s, req); item.Importance != .9 {
		t.Fatal("importance lowered on remember")
	}
	for _, change := range []struct {
		name   string
		mutate func(*RememberRequest)
	}{{"class", func(r *RememberRequest) { r.Class = "working" }}, {"scope", func(r *RememberRequest) { r.Scope = "project:x" }}} {
		t.Run(change.name, func(t *testing.T) {
			r := req
			change.mutate(&r)
			if got := mustRemember(t, s, r); got.ID == first.ID {
				t.Fatal("deduped another scope/class")
			}
		})
	}
	text := "a durable fact" // Normalized equality does not create history.
	same, err := s.Update(UpdateMemoryRequest{ID: first.ID, Patch: MemoryPatch{Text: &text}, Supersede: true})
	if err != nil || same.ID != first.ID {
		t.Fatalf("cosmetic: %+v %v", same, err)
	}
	text = "A different fact"
	next, err := s.Update(UpdateMemoryRequest{ID: first.ID, Patch: MemoryPatch{Text: &text}, Supersede: true})
	if err != nil || next.ID == first.ID || !contains(next.Supersedes, first.ID) {
		t.Fatalf("supersede: %+v %v", next, err)
	}
	raw, err := os.ReadFile(memoryFile(opts, first))
	if err != nil {
		t.Fatal(err)
	}
	old, err := unmarshalMemory(raw)
	if err != nil || old.Status != "superseded" || old.Text != "a durable fact" {
		t.Fatalf("old: %+v %v", old, err)
	}
	expired, err := s.Forget(next.ID)
	if err != nil || expired.Status != "expired" {
		t.Fatalf("forget: %+v %v", expired, err)
	}
	if _, err := os.Stat(memoryFile(opts, next)); err != nil {
		t.Fatal("forget removed file")
	}
	if got := mustList(t, s, MemoryFilter{Status: "expired"}); len(got.Items) != 1 || got.Items[0].ID != next.ID {
		t.Fatalf("expired list: %+v", got)
	}
	if err := s.Touch([]string{first.ID, next.ID, next.ID}); err != nil {
		t.Fatal(err)
	}
	history := mustList(t, s, MemoryFilter{Status: "superseded"})
	if history.Items[0].LastUsedAt <= 0 || history.Items[0].UpdatedAt != old.UpdatedAt {
		t.Fatal("touch must only update last_used_at")
	}
	// Returned slices/provenance cannot mutate a cached item.
	all := mustList(t, s, MemoryFilter{Status: "expired"})
	all.Items[0].Supersedes[0] = "changed"
	if got := mustList(t, s, MemoryFilter{Status: "expired"}); got.Items[0].Supersedes[0] != first.ID {
		t.Fatal("mutable cache leaked")
	}
	tags := []string{"patched"}
	confidence := .2
	scope := "task:next"
	patched, err := s.Update(UpdateMemoryRequest{ID: next.ID, Patch: MemoryPatch{Tags: &tags, Confidence: &confidence, Scope: &scope, Importance: &low}})
	if err != nil || patched.Scope != scope || patched.Confidence != confidence || patched.Importance != low || !reflect.DeepEqual(patched.Tags, tags) {
		t.Fatalf("patch: %+v %v", patched, err)
	}
	bad := "invalid"
	if _, err := s.Update(UpdateMemoryRequest{ID: next.ID, Patch: MemoryPatch{Status: &bad}}); err == nil {
		t.Fatal("invalid patch accepted")
	}
	if err := s.Touch([]string{first.ID, "missing"}); err == nil {
		t.Fatal("missing touch accepted")
	}
}
func TestMemoryFiltersAndOrdering(t *testing.T) {
	s, _ := memoryFixture(t)
	for _, scope := range []string{"global", "project:a", "project:b", "machine:a", "agent:a", "task:a"} {
		r := memoryRequest("MixedCase " + scope)
		r.Scope = scope
		mustRemember(t, s, r)
	}
	for _, tc := range []struct {
		name   string
		filter MemoryFilter
		count  int
	}{{"all", MemoryFilter{}, 6}, {"project-with-global", MemoryFilter{Scopes: []string{"project:a"}}, 2}, {"machine", MemoryFilter{Scopes: []string{"machine:a"}}, 1}, {"union", MemoryFilter{Scopes: []string{"project:a", "task:a"}}, 3}, {"query", MemoryFilter{Query: "MIXEDcase PROJECT:"}, 2}, {"class", MemoryFilter{Classes: []string{"working", "session"}}, 0}, {"limit", MemoryFilter{Limit: 2}, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustList(t, s, tc.filter); len(got.Items) != tc.count {
				t.Fatalf("filter: %+v", got)
			}
		})
	}
	high := .99
	r := memoryRequest("high older")
	r.Importance = &high
	older := mustRemember(t, s, r)
	newer := mustRemember(t, s, memoryRequest("low newer"))
	out := mustList(t, s, MemoryFilter{})
	if out.Items[0].ID != older.ID {
		t.Fatal("importance must sort first")
	}
	// Explicitly update to make recency ordering deterministic within a millisecond.
	status := "active"
	newer, err := s.Update(UpdateMemoryRequest{ID: newer.ID, Patch: MemoryPatch{Status: &status}})
	if err != nil {
		t.Fatal(err)
	}
	out = mustList(t, s, MemoryFilter{})
	if out.Items[1].ID != newer.ID {
		t.Fatal("recency must break importance ties")
	}
	for _, filter := range []MemoryFilter{{Limit: -1}, {Status: "bad"}, {Classes: []string{"bad"}}, {Scopes: []string{"project:"}}} {
		if _, err := s.List(filter); err == nil {
			t.Fatal("invalid filter accepted")
		}
	}
}
func TestMemoryMalformedAndConfinement(t *testing.T) {
	s, opts := memoryFixture(t)
	item := mustRemember(t, s, memoryRequest("good"))
	dir := filepath.Dir(memoryFile(opts, item))
	for name, content := range map[string]string{"bad.md": "no frontmatter", "broken.md": "---\nid: [\n---\nx", "wrong.md": "---\nid: other\nclass: working\n---\nx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	got := mustList(t, NewMemoryStore(opts), MemoryFilter{})
	if len(got.Items) != 1 || got.Malformed != 4 {
		t.Fatalf("malformed: %+v", got)
	}
	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".loom")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMemoryStore(MemoryStoreOptions{Dir: root, Base: ".loom"}).Remember(memoryRequest("must stay inside")); err == nil {
		t.Fatal("escaped vault through symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v %v", entries, err)
	}
}
func TestMemoryConcurrentRemember(t *testing.T) {
	s, _ := memoryFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			if _, err := s.Remember(memoryRequest("same fact")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := mustList(t, s, MemoryFilter{}); len(got.Items) != 1 {
		t.Fatalf("concurrent dedupe: %+v", got)
	}
}
func TestMemoryImportRetryAndMetadata(t *testing.T) {
	_, opts := memoryFixture(t)
	if err := os.MkdirAll(filepath.Join(opts.Dir, ".loom"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.Dir, ".loom", "brain.yaml"), []byte("format: 1\ncreated_at: 123\nprofile: keep-me\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fail := true
	opts.Encode = func(b []byte) ([]byte, error) {
		if fail && strings.Contains(string(b), "id: second") {
			return nil, errors.New("simulated failed import")
		}
		return b, nil
	}
	opts.ImportDistilled = func() ([]MemoryItem, error) {
		return []MemoryItem{{ID: "first", Class: "semantic", Scope: "global", Text: "first", Status: "active", Provenance: MemoryProvenance{Kind: "distilled"}}, {ID: "second", Class: "semantic", Scope: "global", Text: "second", Status: "uncertain", Provenance: MemoryProvenance{Kind: "distilled"}}}, nil
	}
	s := NewMemoryStore(opts)
	if _, err := s.List(MemoryFilter{}); err == nil {
		t.Fatal("expected import failure")
	}
	fail = false
	if got := mustList(t, s, MemoryFilter{Status: "all"}); len(got.Items) != 2 {
		t.Fatalf("retry duplicated: %+v", got)
	}
	opts.ImportDistilled = func() ([]MemoryItem, error) { t.Fatal("import repeated"); return nil, nil }
	mustList(t, NewMemoryStore(opts), MemoryFilter{})
	raw, err := os.ReadFile(filepath.Join(opts.Dir, ".loom", "brain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "profile: keep-me") || !strings.Contains(string(raw), "created_at: 123") || !strings.Contains(string(raw), "imported_distilled: true") {
		t.Fatalf("metadata: %s", raw)
	}
}

func TestMemoryFailedWritesPreserveHistory(t *testing.T) {
	for _, stage := range []string{"successor", "predecessor"} {
		t.Run(stage, func(t *testing.T) {
			s, opts := memoryFixture(t)
			old := mustRemember(t, s, memoryRequest("original"))
			original, err := os.ReadFile(memoryFile(opts, old))
			if err != nil {
				t.Fatal(err)
			}
			opts.Encode = func(data []byte) ([]byte, error) {
				isOld := strings.Contains(string(data), "id: "+old.ID+"\n")
				if (stage == "successor" && !isOld) || (stage == "predecessor" && isOld) {
					return nil, errors.New("simulated write failure")
				}
				return data, nil
			}
			s = NewMemoryStore(opts)
			text := "changed meaning"
			if _, err := s.Update(UpdateMemoryRequest{ID: old.ID, Patch: MemoryPatch{Text: &text}, Supersede: true}); err == nil {
				t.Fatal("expected failed write")
			}
			now, err := os.ReadFile(memoryFile(opts, old))
			if err != nil || string(now) != string(original) {
				t.Fatalf("old file changed on failure: %v", err)
			}
			got := mustList(t, s, MemoryFilter{Status: "all"})
			if len(got.Items) != 1 || got.Items[0].Status != "active" {
				t.Fatalf("partial history: %+v", got)
			}
		})
	}
}
func TestMemoryImportFirstWriteFailureRetries(t *testing.T) {
	_, opts := memoryFixture(t)
	fail := true
	calls := 0
	opts.ImportDistilled = func() ([]MemoryItem, error) {
		calls++
		return []MemoryItem{{ID: "first", Class: "semantic", Scope: "global", Text: "first", Status: "active", Provenance: MemoryProvenance{Kind: "distilled"}}}, nil
	}
	opts.Encode = func(data []byte) ([]byte, error) {
		if fail {
			return nil, errors.New("first write failed")
		}
		return data, nil
	}
	s := NewMemoryStore(opts)
	if _, err := s.List(MemoryFilter{}); err == nil {
		t.Fatal("expected failed import")
	}
	fail = false
	got := mustList(t, s, MemoryFilter{})
	if len(got.Items) != 1 || calls != 2 {
		t.Fatalf("import not retried: %+v, calls %d", got, calls)
	}
}
