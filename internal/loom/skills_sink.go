package loom

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Skill distribution: Loom owns the skills and writes them, on explicit opt-in,
// into the native skills folder of each harness family. Loom only ever touches
// folders named loom-<slug> that its manifest says it wrote.

const skillSinkState = "skill_sinks"

var skillSinkMu sync.Mutex
var skillSinkJobs sync.WaitGroup

// Register before returning to the caller, so lifecycle owners can wait for
// background writes before releasing their data directory.
func syncSkillSinksAsync() {
	skillSinkJobs.Go(func() { syncSkillSinks() })
}

// Folder conventions: Claude Code reads ~/.claude/skills; Codex, Pi and other
// Agent Skills readers use the shared ~/.agents/skills.
func skillSinkDefs() []skillSinkTarget {
	home, _ := os.UserHomeDir()
	return []skillSinkTarget{
		{ID: "claude", Name: "Claude Code", Harnesses: []string{"claude-code"}, Dir: filepath.Join(home, ".claude", "skills")},
		{ID: "agents", Name: "Agent Skills", Harnesses: []string{"codex", "pi", "gemini"}, Dir: filepath.Join(home, ".agents", "skills")},
	}
}

func loadSkillSinks() []skillSinkTarget {
	saved := map[string]skillSinkTarget{}
	_ = getStoreJSON(bkState, skillSinkState, &saved)
	out := skillSinkDefs()
	for i := range out {
		if s, ok := saved[out[i].ID]; ok {
			out[i].Enabled, out[i].Written = s.Enabled, s.Written
		}
	}
	return out
}

func saveSkillSinks(list []skillSinkTarget) error {
	m := map[string]skillSinkTarget{}
	for _, t := range list {
		m[t.ID] = skillSinkTarget{ID: t.ID, Enabled: t.Enabled, Written: t.Written}
	}
	return putStoreJSON(bkState, skillSinkState, m)
}

// syncSkillSinks makes every enabled target contain Loom's bound skills, as
// links to their folders (a marked copy on Windows), and removes what Loom put
// there before and no longer distributes. Loom never touches a folder it did
// not create: legacy generated loom-* folders, its links, its marked copies.
func syncSkillSinks() []skillSinkTarget {
	skillSinkMu.Lock()
	defer skillSinkMu.Unlock()
	list := loadSkillSinks()
	skills := listCapabilities()
	bindings := skillBindings()
	list = resourceLibrary().SyncSkillSinks(list, skills, bindings, home)
	_ = saveSkillSinks(list)
	return list
}

// GET: targets with state. POST {id, enabled}: toggle one target and sync.
func handleSkillSinks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "targets": loadSkillSinks(), "bindings": skillBindings()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	list := loadSkillSinks()
	found := false
	for i := range list {
		if list[i].ID == req.ID {
			list[i].Enabled, found = req.Enabled, true
		}
	}
	if !found {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "unknown target"})
		return
	}
	skillSinkMu.Lock()
	err := saveSkillSinks(list)
	skillSinkMu.Unlock()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "targets": syncSkillSinks()})
}
