package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capCall(t *testing.T, h http.HandlerFunc, target, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func capabilitiesFixture(t *testing.T) (folder, home string) {
	t.Helper()
	testHome(t)
	home = t.TempDir()
	t.Setenv("HOME", home)
	folder = filepath.Join(home, "capabilities")
	write := func(rel, content string) {
		path := filepath.Join(folder, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/network-map/SKILL.md", "---\nname: network-map\ndescription: Use when mapping the local network.\n---\n\n# Map\n")
	write("skills/network-map/scripts/collect.sh", "#!/bin/sh\n")
	write("skills/review/SKILL.md", "---\nname: review\ndescription: Use when reviewing a change.\n---\n")
	write("skills/design/README.md", "Vendored design skills.\n")
	write("skills/design/tokens/SKILL.md", "---\nname: tokens\ndescription: Use when extracting design tokens.\n---\n")
	write("tasks/README.md", "# Tasks\n")
	write("tasks/nightly.md", "---\nname: nightly\ndescription: Collects the map.\nschedule: \"0 4 * * *\"\nagent: shell\nrun: bash skills/network-map/scripts/collect.sh\nenabled: true\n---\n")
	write("tasks/broken.md", "---\nname: broken\ndescription: No schedule.\nagent: claude\nenabled: false\n---\n")
	return folder, home
}

func TestCapabilitiesFolderListsSkillsTasksAndClients(t *testing.T) {
	folder, _ := capabilitiesFixture(t)
	if code, out := capCall(t, handleCapabilitiesFolder, "/api/capabilities/folder", `{"path":"relative/path"}`); code != 400 {
		t.Fatalf("relative path accepted: %d %v", code, out)
	}
	if code, out := capCall(t, handleCapabilitiesFolder, "/api/capabilities/folder", `{"path":"`+t.TempDir()+`"}`); code != 400 {
		t.Fatalf("folder outside home accepted: %d %v", code, out)
	}
	code, out := capCall(t, handleCapabilitiesFolder, "/api/capabilities/folder", `{"path":"`+folder+`"}`)
	if code != 200 || out["dir"] != folder {
		t.Fatalf("set folder %d %v", code, out)
	}
	skills := out["skills"].([]any)
	if len(skills) != 3 || skills[1].(map[string]any)["name"] != "network-map" || skills[1].(map[string]any)["scripts"].(float64) != 1 {
		t.Fatalf("skills %v", skills)
	}
	if fam := skills[0].(map[string]any); fam["name"] != "design-tokens" || fam["family"] != "design" {
		t.Fatalf("skills %v", skills)
	}
	tasks := out["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks %v", tasks)
	}
	broken, nightly := tasks[0].(map[string]any), tasks[1].(map[string]any)
	if broken["error"] == nil || broken["error"] == "" {
		t.Fatalf("broken task not flagged: %v", broken)
	}
	if nightly["schedule"] != "0 4 * * *" || nightly["enabled"] != true || nightly["file"] != "tasks/nightly.md" || nightly["error"] != nil {
		t.Fatalf("nightly %v", nightly)
	}
	clients := out["clients"].([]any)
	if len(clients) != 2 || len(clients[0].(map[string]any)["missing"].([]any)) != 3 {
		t.Fatalf("clients %v", clients)
	}
	// The folder's skills reach the library and the MCP gateway as a read-only source.
	found := false
	for _, c := range listCapabilities() {
		found = found || (c.Source == "folder" && c.ReadOnly)
	}
	if !found {
		t.Fatal("folder skills missing from the library")
	}
}

// Linking creates symbolic links, never replaces what is already there, and
// removes only stale links that point into the folder.
func TestCapabilitiesLinkNeverOverwrites(t *testing.T) {
	folder, home := capabilitiesFixture(t)
	if err := SetConfigKey(capabilitiesDirKey, folder); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(claude, "review"), 0o755); err != nil { // a real folder of the user
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(folder, "skills", "gone"), filepath.Join(claude, "gone")); err != nil {
		t.Fatal(err)
	}
	// design-tokens used to live at skills/tokens: its old link is stale and gets replaced.
	if err := os.Symlink(filepath.Join(folder, "skills", "tokens"), filepath.Join(claude, "design-tokens")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(claude, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	before := clientStatuses(readFolderSkills(filepath.Join(folder, "skills")), filepath.Join(folder, "skills"))[0]
	if len(before.Missing) != 2 || len(before.Blocked) != 1 || len(before.Stale) != 2 {
		t.Fatalf("status before linking %+v", before)
	}
	code, out := capCall(t, handleCapabilitiesLink, "/api/capabilities/link", `{"client":"claude"}`)
	if code != 200 {
		t.Fatalf("link %d %v", code, out)
	}
	if target, err := os.Readlink(filepath.Join(claude, "network-map")); err != nil || target != filepath.Join(folder, "skills", "network-map") {
		t.Fatalf("network-map link %q %v", target, err)
	}
	if target, err := os.Readlink(filepath.Join(claude, "design-tokens")); err != nil || target != filepath.Join(folder, "skills", "design", "tokens") {
		t.Fatalf("family skill link %q %v", target, err)
	}
	if info, err := os.Lstat(filepath.Join(claude, "review")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("a real folder was replaced")
	}
	if _, err := os.Lstat(filepath.Join(claude, "gone")); !os.IsNotExist(err) {
		t.Fatal("stale link into the folder kept")
	}
	if _, err := os.Lstat(filepath.Join(claude, "elsewhere")); err != nil {
		t.Fatal("unrelated link removed")
	}
	st := out["clients"].([]any)[0].(map[string]any)
	if len(st["linked"].([]any)) != 2 || len(st["blocked"].([]any)) != 1 {
		t.Fatalf("status %v", st)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", "network-map")); !os.IsNotExist(err) {
		t.Fatal("another client was linked")
	}
	if code, _ := capCall(t, handleCapabilitiesLink, "/api/capabilities/link", `{"client":"nope"}`); code != 400 {
		t.Fatalf("unknown client %d", code)
	}
}
