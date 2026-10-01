package loom

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
)

// Resource bindings: which Loom MCP servers each harness receives, and which
// Loom skills go to each skills folder. Loom owns the definitions and the
// bindings; the harness owns execution (architecture: definition / binding /
// runtime). nil means "all enabled", the historical behavior.

const (
	harnessMCPState    = "harness_mcp_bindings"  // map[harnessID][]string
	skillBindingsState = "skill_target_bindings" // map[skillID]map[targetID]bool (false = excluded)
)

func harnessMCPBinding(id string) *[]string {
	m := map[string][]string{}
	if !getStoreJSON(bkState, harnessMCPState, &m) {
		return nil
	}
	if names, ok := m[id]; ok {
		return &names
	}
	return nil
}

func setHarnessMCPBinding(id string, names *[]string) error {
	m := map[string][]string{}
	_ = getStoreJSON(bkState, harnessMCPState, &m)
	if names == nil {
		delete(m, id)
	} else {
		if err := validateACPMCPSelection(*names); err != nil {
			return err
		}
		m[id] = append([]string{}, *names...)
	}
	return putStoreJSON(bkState, harnessMCPState, m)
}

func skillBindings() map[string]map[string]bool {
	m := map[string]map[string]bool{}
	_ = getStoreJSON(bkState, skillBindingsState, &m)
	return m
}

func skillBound(b map[string]map[string]bool, skillID, target string) bool {
	if t, ok := b[skillID]; ok {
		if v, set := t[target]; set {
			return v
		}
	}
	return true
}

// GET ?id=harness: Loom MCP servers with their binding for this harness.
// POST {id, mcp: [...] | null}: set the harness binding (null = all enabled).
func handleHarnessBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		defs, _ := LoadMCPConfig()
		binding := harnessMCPBinding(id)
		type row struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"` // enabled in Loom
			Bound   bool   `json:"bound"`   // sent to this harness
		}
		rows := []row{}
		for name, d := range defs {
			bound := d.Enabled
			if binding != nil {
				bound = false
				for _, n := range *binding {
					if n == name {
						bound = true
					}
				}
			}
			rows = append(rows, row{name, d.Enabled, bound})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		sendJSON(w, 200, map[string]any{"ok": true, "mcp": rows, "custom": binding != nil})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID  string    `json:"id"`
		MCP *[]string `json:"mcp"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if _, ok := registeredRuntimes.lookup(req.ID); !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness introuvable"})
		return
	}
	if err := setHarnessMCPBinding(req.ID, req.MCP); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// POST {skill_id, target, enabled}: include or exclude one skill from one folder.
func handleSkillBinding(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		SkillID string `json:"skill_id"`
		Target  string `json:"target"`
		Enabled bool   `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	known := false
	for _, t := range skillSinkDefs() {
		known = known || t.ID == req.Target
	}
	if !known || req.SkillID == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "skill ou cible inconnue"})
		return
	}
	m := skillBindings()
	if m[req.SkillID] == nil {
		m[req.SkillID] = map[string]bool{}
	}
	m[req.SkillID][req.Target] = req.Enabled
	if err := putStoreJSON(bkState, skillBindingsState, m); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "targets": syncSkillSinks(), "bindings": m})
}

var mcpNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// POST {name}: copy one of the harness's own MCP servers into Loom's
// registry. Environment variable values are not copied: their names are
// created empty so the user fills them in Loom.
func handleHarnessMCPAdopt(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	insp, err := inspectHarness(r.Context(), r.PathValue("id"))
	if err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var found *HarnessMCP
	for i := range insp.MCP {
		if insp.MCP[i].Name == req.Name {
			found = &insp.MCP[i]
		}
	}
	if found == nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "serveur MCP introuvable dans ce harness"})
		return
	}
	cfg, err := adoptedMCP(*found)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	name, missing, err := storeAdoptedMCP(found.Name, cfg)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errMCPAdoptConflict) {
			code = http.StatusConflict
		}
		sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "name": name, "env_to_fill": missing})
}

func adoptedMCP(m HarnessMCP) (MCPServerConfig, error) {
	if m.URL != "" {
		return adoptedMCPDefinition(MCPServerConfig{URL: m.URL}, false)
	}
	if m.Command == "" {
		return MCPServerConfig{}, errors.New("définition incomplète : ce harness ne dit pas comment lancer ce serveur")
	}
	env := map[string]string{}
	for _, n := range m.EnvNames {
		if n != "PATH" && n != "HOME" {
			env[n] = ""
		}
	}
	// Disabled until the user checks it: an adopted server is not trusted blindly.
	return adoptedMCPDefinition(MCPServerConfig{Command: m.Command, Args: m.Args, Env: env}, false)
}
