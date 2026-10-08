package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func skillHomeFile(t *testing.T, root, path, text string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
func skillHomePrimary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "vault", Label: "Vault", Path: dir, Kind: "context", Primary: true, Permission: "write"}); err != nil {
		t.Fatal(err)
	}
	return dir
}
func skillHomeRequest(t *testing.T, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/skills/home", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleSkillsHome(w, r)
	return w
}
func skillHomeSwitch(t *testing.T, body string) (skillsHomeStatus, []string, []string) {
	t.Helper()
	w := skillHomeRequest(t, "POST", body)
	if w.Code != 200 {
		t.Fatalf("switch: %d %s", w.Code, w.Body)
	}
	var result struct {
		skillsHomeStatus
		Copied    []string `json:"copied"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	skillSinkJobs.Wait()
	return result.skillsHomeStatus, result.Copied, result.Conflicts
}

func TestSkillsHomeDetectionDepthAndSkips(t *testing.T) {
	testHome(t)
	dir := skillHomePrimary(t)
	for _, path := range []string{"skills/a/SKILL.md", "skills/b/SKILL.md", "one/two/three/c/SKILL.md", "one/two/three/four/d/SKILL.md", ".git/skills/x/SKILL.md", "node_modules/x/SKILL.md", ".loom/skills/x/SKILL.md", ".obsidian/skills/x/SKILL.md"} {
		skillHomeFile(t, dir, path, "instructions")
	}
	outside := t.TempDir()
	skillHomeFile(t, outside, "x/SKILL.md", "external")
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	w := skillHomeRequest(t, "GET", "")
	var result skillsHomeStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := []detectedSkillsHome{{"one/two/three", 1}, {"skills", 2}}
	if !reflect.DeepEqual(result.Detected, want) || result.Mode != "loom" || result.Fallback || result.Relative != "skills" || result.BrainSource != "vault" {
		t.Fatalf("status: %+v", result)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable skills status")
	}
	var shape map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &shape)
	if len(shape) != 8 {
		t.Fatalf("GET shape: %v", shape)
	}
}

func TestSkillsHomeRejectsEscapes(t *testing.T) {
	testHome(t)
	dir := skillHomePrimary(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{outside, "..", "a/../skills", "escape/new", "escape", ".", ".loom/skills", "a\\..\\skills"} {
		b, _ := json.Marshal(skillsHomeSetting{Mode: "brain", Relative: rel})
		w := skillHomeRequest(t, "POST", string(b))
		if w.Code != 400 {
			t.Fatalf("accepted %q: %d %s", rel, w.Code, w.Body)
		}
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("escape wrote outside: %v %v", entries, err)
	}
	if skillsHomeConfig().Mode != "loom" {
		t.Fatal("rejected switch changed setting")
	}
	skillHomeFile(t, dir, "actual/review/SKILL.md", "safe")
	if err := os.Symlink("actual", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	status, _, _ := skillHomeSwitch(t, `{"mode":"brain","relative":"alias"}`)
	if status.Fallback || status.Count != 1 {
		t.Fatalf("internal alias: %+v", status)
	}
	if w := skillHomeRequest(t, "POST", `{"mode":"other"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := skillHomeRequest(t, "POST", `{"mode":"loom","unknown":1}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestSkillsHomeCopyConflictsAndRepointSinks(t *testing.T) {
	home := testHome(t)
	t.Setenv("HOME", t.TempDir())
	dir := skillHomePrimary(t)
	old := filepath.Join(home, "skills")
	for _, name := range []string{"review", "conflict", "foreign"} {
		skillHomeFile(t, old, name+"/SKILL.md", "---\nname: "+name+"\n---\nold instructions")
	}
	skillHomeFile(t, old, "review/scripts/run.sh", "run")
	skillHomeFile(t, dir, "skills/conflict/SKILL.md", "---\nname: conflict\n---\nkeep target")
	_ = putStr(bkState, "skills_migrated", "1")
	targets := loadSkillSinks()
	targets[0].Enabled = true
	if err := saveSkillSinks(targets); err != nil {
		t.Fatal(err)
	}
	syncSkillSinks()
	// A replaced link must not be touched, even though its name is in our manifest.
	foreign := filepath.Join(targets[0].Dir, "foreign")
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	// Older installations only recorded the names.
	targets = loadSkillSinks()
	targets[0].Links = nil
	if err := saveSkillSinks(targets); err != nil {
		t.Fatal(err)
	}
	status, copied, conflicts := skillHomeSwitch(t, `{"mode":"brain"}`)
	if status.Fallback || status.Dir != filepath.Join(dir, "skills") || status.Count != 3 || !reflect.DeepEqual(copied, []string{"foreign", "review"}) || !reflect.DeepEqual(conflicts, []string{"conflict"}) {
		t.Fatalf("switch: %+v %v %v", status, copied, conflicts)
	}
	for _, name := range []string{"review", "conflict"} {
		link := filepath.Join(targets[0].Dir, name)
		if target, err := os.Readlink(link); err != nil || target != filepath.Join(dir, "skills", name) {
			t.Fatalf("link %s: %q %v", name, target, err)
		}
	}
	if info, err := os.Stat(foreign); err != nil || !info.IsDir() {
		t.Fatalf("foreign replacement changed: %v", err)
	}
	for _, check := range [][3]string{{old, "review/SKILL.md", "old instructions"}, {dir, "skills/conflict/SKILL.md", "keep target"}, {dir, "skills/review/scripts/run.sh", "run"}} {
		b, err := os.ReadFile(filepath.Join(check[0], check[1]))
		if err != nil || !strings.Contains(string(b), check[2]) {
			t.Fatalf("copy/backup: %q %v", b, err)
		}
	}
	skillHomeFile(t, dir, "skills/new/SKILL.md", "new")
	status, copied, conflicts = skillHomeSwitch(t, `{"mode":"loom"}`)
	if status.Dir != old || len(copied)+len(conflicts) != 0 {
		t.Fatalf("back: %+v %v %v", status, copied, conflicts)
	}
	if _, err := os.Stat(filepath.Join(old, "new")); !os.IsNotExist(err) {
		t.Fatal("switch back copied a skill")
	}
	if target, err := os.Readlink(filepath.Join(targets[0].Dir, "review")); err != nil || target != filepath.Join(old, "review") {
		t.Fatalf("back link: %q %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(targets[0].Dir, "new")); !os.IsNotExist(err) {
		t.Fatal("old manifest link retained")
	}
}

func TestSkillsHomeCreatesFolderFallbackAndIndexSkip(t *testing.T) {
	home := testHome(t)
	dir := skillHomePrimary(t)
	skillHomeFile(t, home, "skills/review/SKILL.md", "skillonlyword")
	skillHomeFile(t, dir, "notes.md", "noteword")
	status, _, _ := skillHomeSwitch(t, `{"mode":"brain","relative":"procedures/skills"}`)
	if status.Fallback || status.Dir != loomSkillsDir() {
		t.Fatalf("created: %+v", status)
	}
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"skillonlyword": 0, "noteword": 1} {
		hits, err := e.Search(brain.SearchRequest{Query: q, Sources: []string{"vault"}})
		if err != nil || len(hits) != want {
			t.Fatalf("index %s: %v %v", q, hits, err)
		}
	}
	if err = os.RemoveAll(filepath.Join(dir, "procedures")); err != nil {
		t.Fatal(err)
	}
	status = skillsHomeInfo(theBrain().storage)
	if !status.Fallback || status.Reason == "" || status.Dir != filepath.Join(home, "skills") || status.Mode != "brain" {
		t.Fatalf("missing skills: %+v", status)
	}
	if err = os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	status = skillsHomeInfo(theBrain().storage)
	if !status.Fallback || status.BrainSource != "vault" {
		t.Fatalf("missing brain: %+v", status)
	}
	if err = e.Remove("vault"); err != nil {
		t.Fatal(err)
	}
	status = skillsHomeInfo(theBrain().storage)
	if !status.Fallback || status.BrainSource != "" {
		t.Fatalf("no primary: %+v", status)
	}
	if err = SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	t.Cleanup(clearMemDEK)
	status = skillsHomeInfo(theBrain().storage)
	if !status.Fallback || status.Reason != errMemLocked.Error() {
		t.Fatalf("locked: %+v", status)
	}
}

func TestBrainMCPSkills(t *testing.T) {
	home := testHome(t)
	if err := storeWebKey("skills-test-key"); err != nil {
		t.Fatal(err)
	}
	skillHomeFile(t, home, "skills/review/SKILL.md", "---\nname: review\ndescription: Find bugs\nmetadata:\n  title: Review\n---\nInstructions")
	for i := 0; i < 205; i++ {
		skillHomeFile(t, home, fmt.Sprintf("skills/review/files/%03d.txt", i), "asset")
	}
	outside := t.TempDir()
	skillHomeFile(t, outside, "secret.md", "secret")
	if err := os.Symlink(outside, filepath.Join(home, "skills/review/escape")); err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	skillHomeFile(t, linked, "other/SKILL.md", "---\nname: other\n---\nLinked")
	_ = putStoreJSON(bkState, skillSourcesState, []SkillSource{{ID: "external", Path: linked, Label: "External"}})
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "skills-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://localhost/mcp/brain", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, "skills-test-key"}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, tool := range tools.Tools {
		if tool.Name == "list_skills" || tool.Name == "read_skill" {
			found++
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatal("skills tool is not read-only")
			}
		}
	}
	if found != 2 {
		t.Fatalf("missing skills tools: %v", tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_skills", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("list: %v %v", result, err)
	}
	var skills []brain.Skill
	text := result.Content[0].(*mcp.TextContent).Text
	if err = json.Unmarshal([]byte(text), &skills); err != nil || len(skills) != 2 || skills[1].Name != "review" || skills[1].Title != "Review" || skills[1].Description != "Find bugs" {
		t.Fatalf("skills: %s %v", text, err)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "read_skill", Arguments: brain.ReadSkillRequest{Name: "review"}})
	if err != nil || result.IsError {
		t.Fatalf("read: %v %v", result, err)
	}
	var content brain.SkillContent
	text = result.Content[0].(*mcp.TextContent).Text
	if err = json.Unmarshal([]byte(text), &content); err != nil || len(content.Files) != 200 || content.Files[0] != "SKILL.md" || !strings.Contains(content.Text, "Instructions") || strings.Contains(text, "secret.md") {
		t.Fatalf("content: %+v %v", content, err)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "read_skill", Arguments: brain.ReadSkillRequest{Name: "../secret"}})
	if err != nil || !result.IsError {
		t.Fatalf("missing skill: %v %v", result, err)
	}
	if err = SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	t.Cleanup(clearMemDEK)
	for _, name := range []string{"list_skills", "read_skill"} {
		args := map[string]any{}
		if name == "read_skill" {
			args["name"] = "review"
		}
		result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || !result.IsError {
			t.Fatalf("locked %s: %v %v", name, result, err)
		}
	}
}

func TestSkillsHomeCopyRejectsCredentialFilesAndOverlaps(t *testing.T) {
	home := testHome(t)
	dir := skillHomePrimary(t)
	skillHomeFile(t, home, "skills/review/SKILL.md", "instructions")
	skillHomeFile(t, home, "skills/review/.env", "synthetic fixture")
	w := skillHomeRequest(t, "POST", `{"mode":"brain"}`)
	if w.Code != 400 || skillsHomeConfig().Mode != "loom" {
		t.Fatalf("credentials copied: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills/review")); !os.IsNotExist(err) {
		t.Fatal("partial skill retained")
	}
	if err := os.Remove(filepath.Join(home, "skills/review/.env")); err != nil {
		t.Fatal(err)
	}
	skillHomeSwitch(t, `{"mode":"brain"}`)
	w = skillHomeRequest(t, "POST", `{"mode":"brain","relative":"skills/review/nested"}`)
	if w.Code != 400 || skillsHomeConfig().Relative != "skills" {
		t.Fatalf("overlap: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills/review/nested")); !os.IsNotExist(err) {
		t.Fatal("overlap mutated old home")
	}
	status, copied, conflicts := skillHomeSwitch(t, `{"mode":"brain","relative":"skills"}`)
	if status.Fallback || len(copied)+len(conflicts) != 0 {
		t.Fatalf("repeat switch: %+v %v %v", status, copied, conflicts)
	}
}

func TestSkillsHomeHTTPAuthenticationAndMethods(t *testing.T) {
	testHome(t)
	if err := storeWebKey("skills-home-key"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	webAPI(mux)("/api/skills/home", handleSkillsHome)
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "/api/skills/home", strings.NewReader(`{"mode":"loom"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
		r.Header.Set("Authorization", "Bearer skills-home-key")
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("authenticated %s: %d %s", method, w.Code, w.Body)
		}
	}
	w := skillHomeRequest(t, "DELETE", "")
	if w.Code != 405 || w.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("method: %d %s", w.Code, w.Header())
	}
}

func TestSkillsHomeMigratesLegacySkillsBeforeSwitch(t *testing.T) {
	home := testHome(t)
	skillHomePrimary(t)
	skillMigrateOnce = sync.Once{}
	legacy := Capability{ID: "legacy", Name: "Legacy", Instructions: "old database skill"}
	if err := putStoreJSON(bkCapabilities, legacy.ID, legacy); err != nil {
		t.Fatal(err)
	}
	_, copied, conflicts := skillHomeSwitch(t, `{"mode":"brain"}`)
	if !reflect.DeepEqual(copied, []string{"legacy"}) || len(conflicts) != 0 {
		t.Fatalf("legacy copy: %v %v", copied, conflicts)
	}
	if c, ok := getCapability("legacy"); !ok || c.Dir != filepath.Join(loomSkillsDir(), "legacy") {
		t.Fatalf("legacy identity: %+v %v", c, ok)
	}
	if _, err := os.Stat(filepath.Join(home, "skills/legacy/SKILL.md")); err != nil {
		t.Fatal("legacy backup missing", err)
	}
}

func TestBrainMCPSkillsQualifiesDuplicateNames(t *testing.T) {
	home := testHome(t)
	linked := t.TempDir()
	skillHomeFile(t, home, "skills/review/SKILL.md", "owned")
	skillHomeFile(t, linked, "review/SKILL.md", "linked")
	if err := putStoreJSON(bkState, skillSourcesState, []SkillSource{{ID: "external", Path: linked}}); err != nil {
		t.Fatal(err)
	}
	skills, err := theBrain().ListSkills()
	if err != nil || len(skills) != 2 || skills[0].Name != "external:review" || skills[1].Name != "loom:review" {
		t.Fatalf("duplicates: %v %v", skills, err)
	}
	for _, skill := range skills {
		if _, err := theBrain().ReadSkill(brain.ReadSkillRequest{Name: skill.Name}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillsHomeLoomFolderInsideBrainIsNotIndexed(t *testing.T) {
	home := testHome(t)
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "vault", Label: "Vault", Path: home, Kind: "context", Primary: true, Permission: "write"}); err != nil {
		t.Fatal(err)
	}
	skillHomeFile(t, home, "skills/review/SKILL.md", "skillneedle")
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits, err := e.Search(brain.SearchRequest{Query: "skillneedle", Sources: []string{"vault"}})
	if err != nil || len(hits) != 0 {
		t.Fatalf("Loom skills indexed: %v %v", hits, err)
	}
}

func TestBrainMCPSkillsReadsLinkedSkillSymlink(t *testing.T) {
	testHome(t)
	linked, target := t.TempDir(), t.TempDir()
	skillHomeFile(t, target, "SKILL.md", "linked symlink instructions")
	if err := os.Symlink(target, filepath.Join(linked, "linked-skill")); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, skillSourcesState, []SkillSource{{ID: "external", Path: linked}}); err != nil {
		t.Fatal(err)
	}
	skills, err := theBrain().ListSkills()
	if err != nil || len(skills) != 1 || skills[0].Name != "linked-skill" {
		t.Fatalf("linked skill: %v %v", skills, err)
	}
	content, err := theBrain().ReadSkill(brain.ReadSkillRequest{Name: "linked-skill"})
	if err != nil || content.Text != "linked symlink instructions" {
		t.Fatalf("linked skill read: %v %v", content, err)
	}
}
