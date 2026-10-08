package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func markdownFixture(t *testing.T) (*MarkdownStore, string) {
	t.Helper()
	dir := t.TempDir()
	return NewMarkdownStore(MarkdownOptions{Dir: dir}), dir
}
func fileRequest(scope, name, text string) MemoryWrite {
	return MemoryWrite{Scope: scope, Name: name, Description: "Useful retrieval hook", Type: "reference", Text: text}
}
func writeMemory(t *testing.T, s *MarkdownStore, r MemoryWrite) MemoryFile {
	t.Helper()
	m, err := s.Write(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestMarkdownLayoutStableSlugsAndConfinement(t *testing.T) {
	s, dir := markdownFixture(t)
	projectName := "My Project"
	s.opts.ProjectName = func(id string) (string, error) { return projectName, nil }
	first := writeMemory(t, s, fileRequest("project:one", "Release", "Keep releases careful."))
	projectName = "Renamed"
	writeMemory(t, s, fileRequest("project:one", "Tests", "Tests matter."))
	list, err := s.List("project:one")
	if err != nil || filepath.Base(filepath.Dir(list.Path)) != "my-project" {
		t.Fatal(list, err)
	}
	projectName = "My Project"
	writeMemory(t, s, fileRequest("project:two", "Other", "Different project."))
	other, _ := s.List("project:two")
	if other.Path == list.Path {
		t.Fatal("slug collision")
	}
	if first.File != "release.md" {
		t.Fatal(first)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".loom", "brain.json"))
	if err != nil || !strings.Contains(string(b), `"one": "my-project"`) {
		t.Fatal(string(b), err)
	}
	for _, file := range []string{"../escape.md", "/escape.md", "MEMORY.md", "memory.md", "nested/a.md", ".loom/a.md", "a\\b.md"} {
		r := fileRequest("global", "Unsafe", "x")
		r.File = file
		if _, err = s.Write(r, nil); err == nil {
			t.Fatal("accepted", file)
		}
	}
	if _, err = s.Write(fileRequest("task:one", "Unsafe", "x"), nil); err == nil {
		t.Fatal("task scope accepted")
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(dir, "Memory")); err != nil {
		t.Skip(err)
	}
	if _, err = s.Write(fileRequest("global", "Escape", "x"), nil); err == nil {
		t.Fatal("symlink escape")
	}
}
func TestMarkdownIndexAndHumanEdits(t *testing.T) {
	s, dir := markdownFixture(t)
	first := writeMemory(t, s, fileRequest("global", "One", "Original."))
	r := fileRequest("global", "Renamed", "Changed.")
	r.File = first.File
	writeMemory(t, s, r)
	path := filepath.Join(dir, "Memory", first.File)
	if err := os.WriteFile(path, []byte("---\r\nname: Human title\r\ndescription: Edited hook\r\ntype: feedback\r\ncustom: preserved\r\nmetadata: {editor: human}\r\n---\r\nHuman edit"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := s.Read(MemoryRead{Scope: "global", File: first.File})
	if err != nil || m.Name != "Human title" || m.Text != "Human edit" || m.Malformed {
		t.Fatal(m, err)
	}
	r.Name, r.Text = "Updated", "Updated body"
	writeMemory(t, s, r)
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "custom: preserved") || !strings.Contains(string(b), "editor: human") {
		t.Fatal(string(b))
	}
	list, _ := s.List("global")
	if !strings.Contains(list.Index, "[Updated](one.md)") || strings.Contains(list.Index, "[One]") {
		t.Fatal(list.Index)
	}
	duplicate := writeMemory(t, s, fileRequest("global", "Updated", "New revision"))
	if duplicate.File != first.File {
		t.Fatal("near duplicate created", duplicate)
	}
	if err = s.Delete(MemoryRead{Scope: "global", File: first.File}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List("global")
	if len(list.Items) != 0 || strings.Contains(list.Index, "one.md") {
		t.Fatal(list)
	}
}
func TestMarkdownLenientMalformedAndIndexBounds(t *testing.T) {
	s, dir := markdownFixture(t)
	os.MkdirAll(filepath.Join(dir, "Memory"), 0700)
	for file, text := range map[string]string{"plain.md": "No frontmatter; keep me.", "broken.md": "---\nname: [\n---\nKeep me too."} {
		os.WriteFile(filepath.Join(dir, "Memory", file), []byte(text), 0600)
	}
	for i := 0; i < 205; i++ {
		text := fmt.Sprintf("---\nname: Memory %d\ndescription: Hook\ntype: reference\n---\nBody", i)
		os.WriteFile(filepath.Join(dir, "Memory", fmt.Sprintf("file-%03d.md", i)), []byte(text), 0600)
	}
	writeMemory(t, s, fileRequest("global", "Final", "Final memory"))
	list, err := s.List("global")
	if err != nil || len(list.Items) != 208 || len(list.Index) > MemoryIndexBytes || strings.Count(list.Index, "\n") > MemoryIndexLines || list.Warning == "" {
		t.Fatal(len(list.Items), len(list.Index), list.Warning, err)
	}
	for _, file := range []string{"plain.md", "broken.md"} {
		m, _ := s.Read(MemoryRead{Scope: "global", File: file})
		if !m.Malformed || m.Text == "" {
			t.Fatal(m)
		}
		if s.Delete(MemoryRead{Scope: "global", File: file}) == nil {
			t.Fatal("deleted malformed")
		}
		req := fileRequest("global", "Replace", "x")
		req.File = file
		if _, err = s.Write(req, nil); err == nil {
			t.Fatal("overwrote malformed")
		}
	}
	index, warning := BoundMemoryIndex(strings.Repeat("é", 26000))
	if len(index) > MemoryIndexBytes || warning == "" {
		t.Fatal(len(index), warning)
	}
	os.WriteFile(filepath.Join(dir, "Memory", "MEMORY.md"), []byte(strings.Repeat("- [Human](plain.md) — hook\n", 10000)), 0600)
	list, err = s.List("global")
	if err != nil || list.Warning == "" || strings.Count(list.Index, "\n") > 200 {
		t.Fatal("long human index", err, list.Warning)
	}
}
func TestMarkdownCustomLayoutAndLockedAccess(t *testing.T) {
	s, dir := markdownFixture(t)
	os.MkdirAll(filepath.Join(dir, ".loom"), 0700)
	os.WriteFile(filepath.Join(dir, ".loom", "brain.json"), []byte(`{"memory_folder":"Knowledge","projects_folder":"Work","project_memory_folder":"notes/memory","discussions_folder":"History","keep":{"x":1}}`), 0600)
	writeMemory(t, s, fileRequest("project:p", "Fact", "fact"))
	b, _ := os.ReadFile(filepath.Join(dir, ".loom", "brain.json"))
	if !strings.Contains(string(b), `"keep"`) {
		t.Fatal("unrelated config lost")
	}
	list, _ := s.List("project:p")
	if !strings.HasSuffix(filepath.ToSlash(list.Path), "Work/p/notes/memory") {
		t.Fatal(list.Path)
	}
	s.opts.Available = func() error { return fmt.Errorf("locked") }
	if _, err := s.List("global"); err == nil {
		t.Fatal("locked read")
	}
}
func TestMarkdownContextIndexesProfileBM25Bounds(t *testing.T) {
	global := MemoryFiles{Path: "Memory", Index: strings.Repeat("- [A](a.md) — hook\n", 250), Items: []MemoryFile{{File: "user.md", Name: "Profile", Type: "user", Text: strings.Repeat("é", 2000)}}}
	project := MemoryFiles{Path: "Projects/work/memory", Index: "- [B](b.md) — hook\n", Items: []MemoryFile{{File: "p.md", Type: "user", Text: strings.Repeat("x", 2000)}}}
	for i := 0; i < 5; i++ {
		global.Items = append(global.Items, MemoryFile{File: fmt.Sprintf("topic-%d.md", i), Name: "quartz", Type: "reference", Text: "quartz mineral specimen"})
	}
	parts := FileMemoryContext(global, &project, "quartz", true)
	profiles, topics := 0, 0
	for _, part := range parts {
		if part.Reason == "user profile" {
			profiles += len([]rune(part.Text))
		}
		if strings.Contains(part.Reason, "BM25") {
			topics++
			if !strings.Contains(part.Text, "quartz") {
				t.Fatal(part)
			}
		}
		if part.File == "MEMORY.md" && strings.Count(part.Text, "\n") > 201 {
			t.Fatal("long index")
		}
	}
	if profiles > 1500 || topics != 3 {
		t.Fatal(profiles, topics)
	}
	for _, part := range FileMemoryContext(global, &project, "quartz", false) {
		if strings.Contains(part.Reason, "BM25") {
			t.Fatal("harness topic injection")
		}
	}
	many := MemoryFiles{}
	for i := 0; i < 2000; i++ {
		many.Items = append(many.Items, MemoryFile{Name: "Preference", Type: "user"})
	}
	profiles = 0
	for _, part := range FileMemoryContext(many, nil, "", false) {
		if part.Reason == "user profile" {
			profiles += len([]rune(part.Text))
		}
	}
	if profiles > 1500 {
		t.Fatal("profile framing exceeded budget", profiles)
	}
}
func TestMarkdownTranscriptWriteMoveAndSearch(t *testing.T) {
	s, dir := markdownFixture(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC).UnixMilli()
	transcript := Transcript{ID: "discussion/id", Title: "A useful title", Executor: "codex", Model: "native", CreatedAt: now, UpdatedAt: now, Entries: []TranscriptEntry{{Role: "user", Text: "Verbatim quartz question"}, {Tool: "read_file"}, {Role: "assistant", Text: "Verbatim answer"}, {Role: "system", Text: "PRIVATE"}}}
	path, err := s.WriteTranscript(transcript)
	if err != nil || !strings.HasPrefix(path, "Discussions/_/2026-10-08-a-useful-title-") {
		t.Fatal(path, err)
	}
	transcript.ProjectID = "p"
	moved, err := s.WriteTranscript(transcript)
	if err != nil || moved == path {
		t.Fatal(moved, err)
	}
	if _, err = os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
		t.Fatal("old transcript remains", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, moved))
	if !strings.Contains(string(b), "Verbatim quartz question") || !strings.Contains(string(b), "- Tool: read_file") || strings.Contains(string(b), "PRIVATE") {
		t.Fatal(string(b))
	}
	engine, err := New(Options{Conversations: func(ctx context.Context, emit func(Document) bool) error { return s.Documents(ctx.Done(), true, emit) }})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Search(SearchRequest{Query: "quartz", Sources: []string{"conversations"}})
	if err != nil || len(hits) != 1 || hits[0].Path != moved {
		t.Fatal(hits, err)
	}
}
func TestMarkdownMigrationReportAndIdempotence(t *testing.T) {
	legacy, opts := memoryFixture(t)
	profile := memoryRequest("User likes concise replies.")
	profile.Tags = []string{ProfileTag}
	first := mustRemember(t, legacy, profile)
	pending := memoryRequest("Pending preference")
	pending.Status = "candidate"
	mustRemember(t, legacy, pending)
	handoff := memoryRequest("Old state")
	handoff.Class = "working"
	handoff.Tags = []string{"handoff"}
	mustRemember(t, legacy, handoff)
	project := memoryRequest("Project convention")
	project.Scope = "project:work"
	project.Tags = []string{ProjectNotesTag}
	mustRemember(t, legacy, project)
	s := NewMarkdownStore(MarkdownOptions{Dir: opts.Dir})
	if err := s.Migrate(opts, nil); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List("global")
	if len(list.Items) != 1 || list.Items[0].Name != "Profile" || list.Items[0].Type != "user" || list.Items[0].File != "legacy-"+first.ID+".md" {
		t.Fatal(list)
	}
	projectFiles, _ := s.List("project:work")
	if len(projectFiles.Items) != 1 || projectFiles.Items[0].Type != "project" {
		t.Fatal(projectFiles)
	}
	report, _ := os.ReadFile(filepath.Join(opts.Dir, ".loom", "migration-memory.md"))
	if !strings.Contains(string(report), "Skipped") || !strings.Contains(string(report), "Imported") {
		t.Fatal(string(report))
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, ".loom", "memory.legacy")); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(opts, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(opts.Dir, ".loom", "migration-memory.md"))
	if string(after) != string(report) {
		t.Fatal("report rewritten")
	}
	// Simulate interruption after archiving/import but before the completion
	// marker: a retry must retain a later human edit and recover the audit.
	request := fileRequest("global", "Profile", "Human-edited profile")
	request.File, request.Type = list.Items[0].File, "user"
	writeMemory(t, s, request)
	if err := os.Remove(filepath.Join(opts.Dir, ".loom", "migration-memory.md")); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(opts, nil); err != nil {
		t.Fatal(err)
	}
	retained, err := s.Read(MemoryRead{Scope: "global", File: request.File})
	if err != nil || retained.Text != request.Text {
		t.Fatal("retry replaced human edit", retained, err)
	}
	resumed, _ := os.ReadFile(filepath.Join(opts.Dir, ".loom", "migration-memory.md"))
	if !strings.Contains(string(resumed), "Already imported") {
		t.Fatal("missing resumed audit", string(resumed))
	}
}
func TestMemoryOperationsStrictValidation(t *testing.T) {
	valid := `[{"op":"create","scope":"global","name":"Useful","description":"Hook","type":"feedback","text":"Rule\n\n**Why:** Because\n\n**How to apply:** Always"}]`
	for _, text := range []string{"[]", valid} {
		if _, err := ParseMemoryOperations(text); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{"null", "{}", "[] {}", "[null]", `[{"op":"delete","scope":"global","file":"../x.md"}]`, strings.Replace(valid, `"op":"create"`, `"extra":true,"op":"create"`, 1), strings.Replace(valid, "**Why:**", "Why", 1)} {
		if _, err := ParseMemoryOperations(text); err == nil {
			t.Fatal("accepted", text)
		}
	}
	if !strings.Contains(ConsolidationInstructions, "Prefer updating an existing file to creating a near-duplicate") || !json.Valid([]byte(valid)) {
		t.Fatal("duplicate avoidance instruction missing")
	}
}
