package loom

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

// A capabilities folder is a directory the user keeps under version control
// with two conventional children:
//
//	skills/<name>/SKILL.md   Agent Skills: methods, scripts and tool usage
//	tasks/<name>.md          scheduled tasks (front matter), applied as Codeman cron jobs
//
// The folder stays the source of truth. Loom lists it, exposes its skills to
// MCP clients like any linked skills folder, and on explicit request links
// each skill into the user skill folders that agent CLIs read. Linking only
// creates symbolic links: it never replaces a real folder or a link that
// points elsewhere, and it never edits a client's settings.

const capabilitiesDirKey = "CAPABILITIES_DIR"

func capabilitiesDir() string { return strings.TrimSpace(ReadConfig()[capabilitiesDirKey]) }

func capabilitiesSkillsDir() string {
	if dir := capabilitiesDir(); dir != "" {
		return filepath.Join(dir, "skills")
	}
	return ""
}

// folderSkillSource is the capabilities folder's skills/ as a read-only
// source, so the MCP gateway and the library see it without a manual link.
func folderSkillSource(linked []SkillSource) (SkillSource, bool) {
	dir := capabilitiesSkillsDir()
	if dir == "" {
		return SkillSource{}, false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return SkillSource{}, false
	}
	for _, s := range linked {
		if filepath.Clean(s.Path) == filepath.Clean(dir) {
			return SkillSource{}, false
		}
	}
	return SkillSource{ID: "folder", Path: dir, Label: filepath.Base(capabilitiesDir())}, true
}

type skillClient struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Dir   string `json:"dir"`
}

// User skill folders read by agent CLIs. ~/.agents/skills is the shared
// Agent Skills location (Codex, Gemini CLI, OpenCode, Pi and others).
func skillClients() []skillClient {
	h, _ := os.UserHomeDir()
	return []skillClient{
		{ID: "claude", Label: "Claude Code", Dir: filepath.Join(h, ".claude", "skills")},
		{ID: "agents", Label: "Codex, Gemini CLI, OpenCode, Pi", Dir: filepath.Join(h, ".agents", "skills")},
	}
}

type folderSkill struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Scripts     int    `json:"scripts"`
	Dir         string `json:"-"`
}

type clientStatus struct {
	skillClient
	Linked  []string `json:"linked"`
	Missing []string `json:"missing"`
	Blocked []string `json:"blocked"`
	Stale   []string `json:"stale"`
}

type folderTask struct {
	File         string `json:"file"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Schedule     string `json:"schedule,omitempty"`
	EveryMinutes string `json:"every_minutes,omitempty"`
	Agent        string `json:"agent"`
	Run          string `json:"run,omitempty"`
	Enabled      bool   `json:"enabled"`
	Error        string `json:"error,omitempty"`
}

func readFolderSkills(dir string) []folderSkill {
	out := []folderSkill{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		raw, err := readSmall(filepath.Join(path, "SKILL.md"))
		if err != nil {
			continue
		}
		meta, _ := resources.ParseSkillMarkdown(raw)
		s := folderSkill{Name: e.Name(), Title: meta["name"], Description: meta["description"], Dir: path}
		if s.Title == s.Name {
			s.Title = ""
		}
		if scripts, err := os.ReadDir(filepath.Join(path, "scripts")); err == nil {
			for _, f := range scripts {
				if !f.IsDir() {
					s.Scripts++
				}
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func readSmall(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", errors.New("file too large")
	}
	return string(raw), nil
}

func readFolderTasks(dir string) []folderTask {
	out := []folderTask{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "README.md") || strings.HasPrefix(name, ".") {
			continue
		}
		t := folderTask{File: "tasks/" + name}
		raw, err := readSmall(filepath.Join(dir, name))
		if err != nil {
			t.Error = err.Error()
			out = append(out, t)
			continue
		}
		meta, _ := resources.ParseSkillMarkdown(raw)
		t.Name, t.Description = meta["name"], meta["description"]
		t.Schedule, t.EveryMinutes = meta["schedule"], meta["every_minutes"]
		t.Agent, t.Run, t.Enabled = meta["agent"], meta["run"], meta["enabled"] == "true"
		switch {
		case t.Name == "":
			t.Name, t.Error = strings.TrimSuffix(name, ".md"), "missing name"
		case t.Agent == "":
			t.Error = "missing agent"
		case (t.Schedule == "") == (t.EveryMinutes == ""):
			t.Error = "set exactly one of schedule or every_minutes"
		case t.Agent == "shell" && t.Run == "":
			t.Error = "a shell task needs run"
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

func sameSkillDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func clientStatuses(skills []folderSkill, skillsDir string) []clientStatus {
	out := []clientStatus{}
	for _, c := range skillClients() {
		st := clientStatus{skillClient: c, Linked: []string{}, Missing: []string{}, Blocked: []string{}, Stale: []string{}}
		for _, s := range skills {
			dest := filepath.Join(c.Dir, s.Name)
			if _, err := os.Lstat(dest); os.IsNotExist(err) {
				st.Missing = append(st.Missing, s.Name)
			} else if sameSkillDir(dest, s.Dir) {
				st.Linked = append(st.Linked, s.Name)
			} else {
				st.Blocked = append(st.Blocked, s.Name)
			}
		}
		st.Stale = staleLinks(c.Dir, skillsDir)
		out = append(out, st)
	}
	return out
}

// staleLinks: links into the folder's skills/ whose skill was removed.
func staleLinks(clientDir, skillsDir string) []string {
	out := []string{}
	entries, err := os.ReadDir(clientDir)
	if err != nil || skillsDir == "" {
		return out
	}
	for _, e := range entries {
		path := filepath.Join(clientDir, e.Name())
		target, err := os.Readlink(path)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(clientDir, target)
		}
		if rel, err := filepath.Rel(skillsDir, filepath.Clean(target)); err != nil || !filepath.IsLocal(rel) {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			out = append(out, e.Name())
		}
	}
	return out
}

func capabilitiesFolderState() map[string]any {
	dir := capabilitiesDir()
	state := map[string]any{"ok": true, "dir": dir, "skills": []folderSkill{}, "tasks": []folderTask{}, "clients": []clientStatus{}}
	if dir == "" {
		state["suggested"] = suggestedCapabilitiesDir()
		return state
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		state["error"] = "folder not found"
		return state
	}
	skills := readFolderSkills(filepath.Join(dir, "skills"))
	state["skills"] = skills
	state["tasks"] = readFolderTasks(filepath.Join(dir, "tasks"))
	state["clients"] = clientStatuses(skills, filepath.Join(dir, "skills"))
	return state
}

// The files folder is the natural default when it already follows the layout.
func suggestedCapabilitiesDir() string {
	dir := secondBrainDir()
	for _, child := range []string{"skills", "tasks"} {
		if info, err := os.Stat(filepath.Join(dir, child)); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func handleCapabilitiesFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Path string `json:"path"`
		}
		if !workspaceDecode(w, r, &body) {
			return
		}
		dir := strings.TrimSpace(body.Path)
		if strings.HasPrefix(dir, "~/") {
			h, _ := os.UserHomeDir()
			dir = filepath.Join(h, dir[2:])
		}
		if dir != "" {
			if !filepath.IsAbs(dir) {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "absolute folder path required"})
				return
			}
			dir = filepath.Clean(dir)
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "folder not found"})
				return
			}
		}
		if err := SetConfigKey(capabilitiesDirKey, dir); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	} else if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, capabilitiesFolderState())
}

// handleCapabilitiesLink links the folder's skills into one client's skills
// folder (or all of them) and removes the client's stale links into it.
func handleCapabilitiesLink(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Client string `json:"client"`
	}
	if !workspaceDecode(w, r, &body) {
		return
	}
	dir := capabilitiesDir()
	if dir == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "choose a capabilities folder first"})
		return
	}
	skillsDir := filepath.Join(dir, "skills")
	skills := readFolderSkills(skillsDir)
	found := false
	for _, c := range skillClients() {
		if body.Client != "" && body.Client != c.ID {
			continue
		}
		found = true
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		for _, s := range skills {
			dest := filepath.Join(c.Dir, s.Name)
			if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				continue // linked already, or something else lives there: leave it
			}
			if err := os.Symlink(s.Dir, dest); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		for _, name := range staleLinks(c.Dir, skillsDir) {
			_ = os.Remove(filepath.Join(c.Dir, name))
		}
	}
	if !found {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown client"})
		return
	}
	sendJSON(w, 200, capabilitiesFolderState())
}
