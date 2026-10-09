package loom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func brainAgentsFixture(t *testing.T) (*brainService, string) {
	t.Helper()
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-profile"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	// A documented installed package fixture; never read or edit real configs.
	pkg := filepath.Join(home, "pi-package")
	brainAgentsWrite(t, filepath.Join(pkg, "README.md"), "Global instructions: ~/.pi/agent/AGENTS.md\nPI_CODING_AGENT_DIR overrides the config directory.\n")
	t.Setenv("PI_PACKAGE_DIR", pkg)
	// Linked agents are listed only when installed here: provide stand-ins so
	// the test does not depend on the CLIs of the machine running it.
	bin := filepath.Join(home, "fake-bin")
	for _, name := range []string{"claude", "codex", "opencode", "agy", "pi"} {
		brainAgentsWrite(t, filepath.Join(bin, name), "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := theBrain()
	dir := t.TempDir()
	e, err := s.get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "vault", Label: "Vault", Path: dir, Kind: "context", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}
func brainAgentsWrite(t *testing.T, file, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func brainAgentsRead(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func brainAgentsCall(s *brainService, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/brain/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.agentsHTTP(w, req)
	return w
}
func brainAgentsToggle(t *testing.T, s *brainService, id string, enabled bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "enabled": enabled})
	w := brainAgentsCall(s, "POST", string(body))
	if w.Code != 200 {
		t.Fatalf("toggle %s: %d %s", id, w.Code, w.Body)
	}
}
func brainAgentsSetting(t *testing.T, file string) map[string]json.RawMessage {
	t.Helper()
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(brainAgentsRead(t, file)), &top); err != nil {
		t.Fatal(err)
	}
	return top
}
func brainAgentsCheckDir(t *testing.T, file, dir string) {
	t.Helper()
	var value string
	if json.Unmarshal(brainAgentsSetting(t, file)["autoMemoryDirectory"], &value) != nil || value != dir {
		t.Fatalf("wrong autoMemoryDirectory in %s: %q", file, value)
	}
}

func TestBrainAgentsWritersPreserveBackupIdempotentAndRemove(t *testing.T) {
	for _, id := range []string{"claude-code", "codex", "opencode", "antigravity", "pi"} {
		t.Run(id, func(t *testing.T) {
			s, dir := brainAgentsFixture(t)
			var spec brainAgentSpec
			for _, item := range brainAgentSpecs() {
				if item.id == id {
					spec = item
				}
			}
			before := "# User instructions\nKeep my text exactly."
			if id == "claude-code" {
				before = `{"model":"native","permissions":{"allow":["Read"]},"autoMemoryDirectory":"/previous/memory"}`
			}
			brainAgentsWrite(t, spec.file, before)
			brainAgentsToggle(t, s, id, true)
			first := brainAgentsRead(t, spec.file)
			if got := brainAgentsRead(t, spec.file+".loom-backup"); got != before {
				t.Fatalf("backup: %q", got)
			}
			if id == "claude-code" {
				brainAgentsCheckDir(t, spec.file, filepath.Join(dir, "Memory"))
				top := brainAgentsSetting(t, spec.file)
				var permissions bytes.Buffer
				_ = json.Compact(&permissions, top["permissions"])
				if string(top["model"]) != `"native"` || permissions.String() != `{"allow":["Read"]}` {
					t.Fatal("other JSON keys changed")
				}
			} else {
				if !strings.HasPrefix(first, before+"\n") || strings.Count(first, brainAgentBegin) != 1 || strings.Count(first, brainAgentEnd) != 1 {
					t.Fatalf("markers/text: %s", first)
				}
				start, end, err := brainAgentMarkers(first, brainAgentBegin, brainAgentEnd)
				if err != nil || len(strings.Split(strings.TrimSuffix(first[start:end], "\n"), "\n")) >= 25 {
					t.Fatal("block too long or invalid")
				}
				for _, want := range []string{filepath.Join(dir, "Memory", "MEMORY.md"), "name, description, type (user|feedback|project|reference)", "Why:", "How to apply:", "Projects/<slug>/memory/", ".loom", "Never store secrets", "updating existing"} {
					if !strings.Contains(first, want) {
						t.Fatalf("missing %q", want)
					}
				}
			}
			brainAgentsToggle(t, s, id, true)
			if brainAgentsRead(t, spec.file) != first {
				t.Fatal("not idempotent")
			}
			// Edits outside ownership survive unlink.
			if id == "claude-code" {
				top := brainAgentsSetting(t, spec.file)
				top["theme"] = json.RawMessage(`"dark"`)
				data, _ := json.Marshal(top)
				brainAgentsWrite(t, spec.file, string(data))
			} else {
				brainAgentsWrite(t, spec.file, first+"\nAfter Loom\n")
			}
			brainAgentsToggle(t, s, id, false)
			if id == "claude-code" {
				brainAgentsCheckDir(t, spec.file, "/previous/memory")
				if string(brainAgentsSetting(t, spec.file)["theme"]) != `"dark"` {
					t.Fatal("user edit lost")
				}
			} else if got := brainAgentsRead(t, spec.file); got != before+"\nAfter Loom\n" {
				t.Fatalf("text outside markers changed: %q", got)
			}
			brainAgentsToggle(t, s, id, false)
			if brainAgentsRead(t, spec.file+".loom-backup") != before {
				t.Fatal("backup overwritten")
			}
			state, err := loadBrainAgentState()
			if err != nil || state[id].Enabled || len(state[id].Files) != 0 {
				t.Fatal("ownership not cleared")
			}
		})
	}
}
func TestBrainAgentsNewFilesAndClaudeLocalProjectSettings(t *testing.T) {
	s, dir := brainAgentsFixture(t)
	projectDir := t.TempDir()
	repoAgents := "# Repository instructions\nDo not edit.\n"
	repoSettings := `{"model":"shared"}`
	brainAgentsWrite(t, filepath.Join(projectDir, "AGENTS.md"), repoAgents)
	brainAgentsWrite(t, filepath.Join(projectDir, ".claude", "settings.json"), repoSettings)
	p, err := saveProjectContext(ChatProject{Name: "Project", Directory: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"claude-code", "codex", "opencode", "antigravity", "pi"} {
		brainAgentsToggle(t, s, id, true)
	}
	file := filepath.Join(projectDir, ".claude", "settings.local.json")
	brainAgentsCheckDir(t, file, filepath.Join(dir, "Projects", skillDirSlug(p.ID), "memory"))
	ignored := brainAgentsRead(t, filepath.Join(projectDir, ".claude", ".gitignore"))
	if !strings.Contains(ignored, "/settings.local.json\n") {
		t.Fatal("local settings not ignored")
	}
	if brainAgentsRead(t, filepath.Join(projectDir, "AGENTS.md")) != repoAgents || brainAgentsRead(t, filepath.Join(projectDir, ".claude", "settings.json")) != repoSettings {
		t.Fatal("repository instructions/settings edited")
	}
	var metadata struct {
		Projects map[string]brainAgentProject `json:"agent_projects"`
	}
	if json.Unmarshal([]byte(brainAgentsRead(t, filepath.Join(dir, ".loom", "brain.json"))), &metadata) != nil || metadata.Projects[p.ID].Directory != projectDir {
		t.Fatal("missing project map")
	}
	brainAgentsWrite(t, file, `{"autoMemoryDirectory":`+string(brainAgentsSetting(t, file)["autoMemoryDirectory"])+`,"user":true}`)
	for _, id := range []string{"claude-code", "codex", "opencode", "antigravity", "pi"} {
		brainAgentsToggle(t, s, id, false)
	}
	top := brainAgentsSetting(t, file)
	if _, ok := top["autoMemoryDirectory"]; ok || string(top["user"]) != "true" {
		t.Fatal("removal affected user keys")
	}
	for _, spec := range brainAgentSpecs() {
		if _, err := os.Stat(spec.file); !os.IsNotExist(err) {
			t.Fatalf("created global file remained: %s %v", spec.file, err)
		}
	}
	// Topic files and indexes are canonical and survive unlink.
	if brainAgentsRead(t, filepath.Join(dir, "Memory", "MEMORY.md")) != "# Memory\n\n" {
		t.Fatal("memory index removed")
	}
}
func TestBrainAgentsRepointSourceAndProjectChanges(t *testing.T) {
	s, dir := brainAgentsFixture(t)
	brainAgentsWrite(t, filepath.Join(dir, ".loom", "brain.json"), `{"foreign":{"keep":true}}`)
	for _, id := range []string{"claude-code", "codex", "opencode", "antigravity", "pi"} {
		brainAgentsToggle(t, s, id, true)
	}
	projectDir := t.TempDir()
	p, err := saveProjectContext(ChatProject{Name: "Work", Directory: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(projectDir, ".claude", "settings.local.json")
	brainAgentsCheckDir(t, local, filepath.Join(dir, "Projects", skillDirSlug(p.ID), "memory"))
	if !strings.Contains(brainAgentsRead(t, filepath.Join(dir, ".loom", "brain.json")), `"foreign"`) {
		t.Fatal("brain metadata lost")
	}
	// Source API repoints immediately, preserving topic files in the old brain.
	brainAgentsWrite(t, filepath.Join(dir, "Memory", "topic.md"), "durable topic")
	next := t.TempDir()
	body, _ := json.Marshal(map[string]any{"id": "vault", "label": "Vault", "path": next, "kind": "context", "permission": "write", "primary": true})
	req := httptest.NewRequest("POST", "/api/brain/sources", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.sources(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, spec := range brainAgentSpecs() {
		if spec.id == "claude-code" {
			brainAgentsCheckDir(t, spec.file, filepath.Join(next, "Memory"))
		} else if !strings.Contains(brainAgentsRead(t, spec.file), next) || strings.Contains(brainAgentsRead(t, spec.file), dir) {
			t.Fatal("global link not repointed")
		}
	}
	brainAgentsCheckDir(t, local, filepath.Join(next, "Projects", skillDirSlug(p.ID), "memory"))
	if brainAgentsRead(t, filepath.Join(dir, "Memory", "topic.md")) != "durable topic" {
		t.Fatal("old brain modified")
	}
	oldLocal := local
	p.Directory = t.TempDir()
	if _, err = saveProjectContext(p); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(oldLocal); !os.IsNotExist(err) {
		t.Fatal("old project link left behind")
	}
	local = filepath.Join(p.Directory, ".claude", "settings.local.json")
	brainAgentsCheckDir(t, local, filepath.Join(next, "Projects", skillDirSlug(p.ID), "memory"))
	if err = deleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("deleted project link left behind")
	}
	// No primary unlinks native settings but retains the explicit opt-in.
	req = httptest.NewRequest("POST", "/api/brain/sources", strings.NewReader(`{"action":"remove","id":"vault"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.sources(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	state, _ := loadBrainAgentState()
	for _, link := range state {
		if !link.Enabled || len(link.Files) != 0 {
			t.Fatal("primary removal lost opt-in or left stale files")
		}
	}
	e, _ := s.get()
	if err = e.Update(brain.Source{ID: "next", Label: "Next", Path: dir, Kind: "context", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.syncBrainAgents(); err != nil {
		t.Fatal(err)
	}
	if info := s.brainAgentsInfo(state); info.MemoryDir != filepath.Join(dir, "Memory") {
		t.Fatal("wrong new primary")
	}
}
func TestBrainAgentsRefuseChangedUnownedMalformedAndSymlinkFiles(t *testing.T) {
	for _, id := range []string{"claude-code", "codex", "opencode", "antigravity", "pi"} {
		t.Run(id, func(t *testing.T) {
			s, _ := brainAgentsFixture(t)
			brainAgentsToggle(t, s, id, true)
			var file string
			for _, spec := range brainAgentSpecs() {
				if spec.id == id {
					file = spec.file
				}
			}
			text := brainAgentsRead(t, file)
			changed := strings.Replace(text, "Never store secrets.", "Edited guidance.", 1)
			if id == "claude-code" {
				changed = `{"autoMemoryDirectory":"/user/changed","other":true}`
			}
			brainAgentsWrite(t, file, changed)
			for _, on := range []string{"true", "false"} {
				w := brainAgentsCall(s, "POST", `{"id":"`+id+`","enabled":`+on+`}`)
				if w.Code != 400 || !strings.Contains(w.Body.String(), "edited outside Loom") || brainAgentsRead(t, file) != changed {
					t.Fatal("modified an edited link")
				}
			}
		})
	}
	for _, text := range []string{brainAgentBegin + "\n", brainAgentEnd + "\n", brainAgentBegin + "\n" + brainAgentEnd + "\n", brainAgentBegin + "\n" + brainAgentBegin + "\n" + brainAgentEnd + "\n"} {
		s, _ := brainAgentsFixture(t)
		file := filepath.Join(os.Getenv("CODEX_HOME"), "AGENTS.md")
		brainAgentsWrite(t, file, text)
		if w := brainAgentsCall(s, "POST", `{"id":"codex","enabled":true}`); w.Code != 400 || brainAgentsRead(t, file) != text {
			t.Fatal("accepted foreign/malformed markers")
		}
	}
	s, _ := brainAgentsFixture(t)
	file := filepath.Join(os.Getenv("CODEX_HOME"), "AGENTS.md")
	outside := filepath.Join(t.TempDir(), "outside.md")
	brainAgentsWrite(t, outside, "untouched")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Skip(err)
	}
	if w := brainAgentsCall(s, "POST", `{"id":"codex","enabled":true}`); w.Code != 400 || brainAgentsRead(t, outside) != "untouched" {
		t.Fatal("symlink overwritten")
	}
}
func TestBrainAgentsHTTPShapeAuthUnsupportedAndReadOnly(t *testing.T) {
	s, dir := brainAgentsFixture(t)
	if err := storeWebKey("agents-key"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	req := httptest.NewRequest("GET", "/api/brain/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("unprotected agents route")
	}
	req.Header.Set("Authorization", "Bearer agents-key")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var info brainAgentsInfo
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || info.MemoryDir != filepath.Join(dir, "Memory") || len(info.Agents) < 5 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	for _, a := range info.Agents {
		data, _ := json.Marshal(a)
		var fields map[string]any
		_ = json.Unmarshal(data, &fields)
		if len(fields) != 6 || a.Linked {
			t.Fatal("unexpected agent shape/default")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Memory")); !os.IsNotExist(err) {
		t.Fatal("GET created memory")
	}
	for _, request := range []struct {
		method, body string
		code         int
	}{{"PUT", "", 405}, {"POST", `{"id":"codex"}`, 400}, {"POST", `{"id":"unknown","enabled":true}`, 400}, {"POST", `{"id":"custom-remote-codex","enabled":true}`, 400}, {"POST", `{"id":"codex","enabled":true,"consent":true}`, 400}} {
		if w := brainAgentsCall(s, request.method, request.body); w.Code != request.code {
			t.Fatal(w.Body.String())
		}
	}
	// Missing installed docs means Pi is unsupported, without a guessed path.
	t.Setenv("PI_PACKAGE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	for _, spec := range brainAgentSpecs() {
		if spec.id == "pi" && (spec.file != "" || spec.note == "") {
			t.Fatal("guessed undocumented Pi file")
		}
	}
	if w := brainAgentsCall(s, "POST", `{"id":"pi","enabled":true}`); w.Code != 400 {
		t.Fatal("undocumented Pi accepted")
	}
}

func TestBrainAgentsExistingProjectSettingsAndAtomicConflict(t *testing.T) {
	s, dir := brainAgentsFixture(t)
	folder := t.TempDir()
	local := filepath.Join(folder, ".claude", "settings.local.json")
	before := `{"autoMemoryDirectory":"/original/project","permissions":{"allow":["Read"]},"theme":"dark"}`
	brainAgentsWrite(t, local, before)
	ignore := filepath.Join(folder, ".claude", ".gitignore")
	ignoreBefore := "# User exclusions\ncache/\n"
	brainAgentsWrite(t, ignore, ignoreBefore)
	p, err := saveProjectContext(ChatProject{Name: "Existing", Directory: folder})
	if err != nil {
		t.Fatal(err)
	}
	brainAgentsToggle(t, s, "claude-code", true)
	brainAgentsCheckDir(t, local, filepath.Join(dir, "Projects", skillDirSlug(p.ID), "memory"))
	if brainAgentsRead(t, local+".loom-backup") != before || brainAgentsRead(t, ignore+".loom-backup") != ignoreBefore {
		t.Fatal("project backup missing")
	}
	global := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	globalBefore := brainAgentsRead(t, global)
	changed := `{"autoMemoryDirectory":"/user/edited","theme":"dark"}`
	brainAgentsWrite(t, local, changed)
	w := brainAgentsCall(s, "POST", `{"id":"claude-code","enabled":false}`)
	if w.Code != 400 || brainAgentsRead(t, global) != globalBefore || brainAgentsRead(t, local) != changed {
		t.Fatal("partially unlinked on conflict")
	}
	// Restore only Loom's value, then unlink restores the prior user setting.
	value, _ := json.Marshal(filepath.Join(dir, "Projects", skillDirSlug(p.ID), "memory"))
	brainAgentsWrite(t, local, `{"autoMemoryDirectory":`+string(value)+`,"theme":"new"}`)
	brainAgentsToggle(t, s, "claude-code", false)
	brainAgentsCheckDir(t, local, "/original/project")
	if string(brainAgentsSetting(t, local)["theme"]) != `"new"` || brainAgentsRead(t, ignore) != ignoreBefore || brainAgentsRead(t, local+".loom-backup") != before {
		t.Fatal("project user text or backup changed")
	}
}
func TestBrainAgentsRemoteOnlyAndPiRemovalWithoutDocs(t *testing.T) {
	s, _ := brainAgentsFixture(t)
	machines := []RemoteMachine{{ID: "ssh-box", Name: "SSH box", User: "user", Host: "example.invalid"}, {ID: "paired-box", Name: "Paired box", NodeID: "node", Modules: []string{"harness"}}}
	if err := putStoreJSON(bkState, remoteMachinesState, machines); err != nil {
		t.Fatal(err)
	}
	state, _ := loadBrainAgentState()
	info := s.brainAgentsInfo(state)
	for _, a := range info.Agents {
		if strings.HasPrefix(a.ID, "custom-") {
			t.Fatalf("remote duplicate: %+v", a)
		}
	}
	brainAgentsToggle(t, s, "pi", true)
	file := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "AGENTS.md")
	t.Setenv("PI_PACKAGE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	brainAgentsToggle(t, s, "pi", false)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("could not unlink after uninstall")
	}
}
func TestBrainAgentsPreserveEditedPrefixAndConfineProjectSettings(t *testing.T) {
	s, _ := brainAgentsFixture(t)
	file := filepath.Join(os.Getenv("CODEX_HOME"), "AGENTS.md")
	brainAgentsWrite(t, file, "original without newline")
	brainAgentsToggle(t, s, "codex", true)
	text := brainAgentsRead(t, file)
	prefix := "changed user text\nextra line\n"
	start, _, _ := brainAgentMarkers(text, brainAgentBegin, brainAgentEnd)
	brainAgentsWrite(t, file, prefix+text[start:])
	brainAgentsToggle(t, s, "codex", false)
	if brainAgentsRead(t, file) != prefix {
		t.Fatal("changed text outside markers")
	}
	folder := filepath.Join(os.Getenv("HOME"), "project")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(os.Getenv("HOME"), "other")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(folder, ".claude")); err != nil {
		t.Skip(err)
	}
	if _, err := saveProjectContext(ChatProject{Name: "Symlink", Directory: folder}); err != nil {
		t.Fatal(err)
	}
	if w := brainAgentsCall(s, "POST", `{"id":"claude-code","enabled":true}`); w.Code != 400 {
		t.Fatal("project settings escaped project root")
	}
	if _, err := os.Stat(filepath.Join(target, "settings.local.json")); !os.IsNotExist(err) {
		t.Fatal("outside settings written")
	}
}
