package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type acpConfiguration struct {
	MCPServers     json.RawMessage `json:"mcp_servers"`
	Workdir        *string         `json:"workdir"`
	AdditionalDirs *[]string       `json:"additional_dirs"`
	Permission     *string         `json:"permission"`
	Mode           *string         `json:"mode"`
	Config         map[string]any  `json:"config"`
}

func (c acpConfiguration) present() bool {
	return c.MCPServers != nil || c.Workdir != nil || c.AdditionalDirs != nil || c.Permission != nil || c.Mode != nil || c.Config != nil
}

// Called under the session lock while no turn is running. No prompt is sent.
func (m *runtimeSessions) configureACPLocked(s *RuntimeSession, c acpConfiguration, consent bool) error {
	old := cloneACPState(s.ACPState)
	if c.MCPServers != nil {
		var names *[]string
		if json.Unmarshal(c.MCPServers, &names) != nil {
			return errors.New("liste de serveurs MCP invalide")
		}
		if names != nil {
			if err := validateACPMCPSelection(*names); err != nil {
				return err
			}
		}
		s.MCPServers = names
	}
	agent, _ := acpAgentFor(s.RuntimeID)
	if c.Workdir != nil {
		check := acpDirectory
		if agent.Remote {
			check = remoteWorkdir
		}
		path, err := check(*c.Workdir)
		if err != nil {
			return err
		}
		s.Workdir = path
	}
	if c.AdditionalDirs != nil && agent.Remote && len(*c.AdditionalDirs) > 0 {
		return errors.New("dossiers supplémentaires indisponibles pour un harness distant")
	}
	if c.AdditionalDirs != nil {
		if len(*c.AdditionalDirs) > 16 {
			return errors.New("16 dossiers supplémentaires maximum")
		}
		s.AdditionalDirs = []string{}
		for _, path := range *c.AdditionalDirs {
			dir, err := acpDirectory(path)
			if err != nil {
				return err
			}
			s.AdditionalDirs = append(s.AdditionalDirs, dir)
		}
	}
	if c.Permission != nil {
		level := *c.Permission
		if level != "ask" && level != "edits" && level != "full" {
			return errors.New("permission invalide : ask, edits ou full")
		}
		if level == "full" && old.Permission != "full" && !consent {
			return errors.New("confirmez explicitement le niveau full avec consent:true")
		}
		s.Permission = level
	}
	if c.Mode != nil {
		if len(*c.Mode) > 200 {
			return errors.New("mode invalide")
		}
		s.Mode = *c.Mode
	}
	if c.Config != nil {
		if len(c.Config) > 64 {
			return errors.New("trop d’options ACP")
		}
		if s.ConfigOptions == nil {
			s.ConfigOptions = map[string]any{}
		}
		for key, value := range c.Config {
			if key == "" || len(key) > 200 {
				return errors.New("identifiant d’option invalide")
			}
			switch v := value.(type) {
			case string:
				if len(v) > 4096 {
					return errors.New("valeur d’option trop longue")
				}
			case bool:
			default:
				return errors.New("valeur d’option ACP : chaîne ou booléen requis")
			}
			s.ConfigOptions[key] = value
		}
	}
	rootsChanged := old.Workdir != s.Workdir || !reflect.DeepEqual(old.AdditionalDirs, s.AdditionalDirs)
	if rootsChanged {
		m.closeACP(s.ID)
		s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
		s.Files = nil
		s.FileBaselines = nil
		s.AvailableModes = nil
		s.AvailableConfigOptions = nil
		s.ACPUsage = nil
		s.Commands = nil
		s.AgentCapabilities = nil
	} else if c.Mode != nil || c.Config != nil {
		m.acpMu.Lock()
		p := m.acp[s.ID]
		m.acpMu.Unlock()
		if p != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			mode := ""
			if c.Mode != nil {
				mode = *c.Mode
			}
			if err := p.configure(ctx, mode, c.Config); err != nil {
				m.closeACP(s.ID)
				return err
			}
			scope := s.MCPServers
			p.mu.Lock()
			s.ACPState = cloneACPState(p.state)
			s.MCPServers = scope
			p.mu.Unlock()
			// Policy is local, distinct from agent mode/config responses.
			s.Permission = old.Permission
			if c.Permission != nil {
				s.Permission = *c.Permission
			}
		}
	}
	return nil
}
func handleACPRuntimes(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "runtimes": runtimeCatalog()})
}
func handleACPApproval(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		ID         string `json:"id"`
		ApprovalID string `json:"approval_id"`
		OptionID   string `json:"option_id"`
		Cancel     bool   `json:"cancel"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := workspaceSessions.answerACP(req.ID, req.ApprovalID, req.OptionID, req.Cancel); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
func handleACPFiles(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	s, ok := workspaceSessions.get(r.URL.Query().Get("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion introuvable"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "files": append([]ACPChangedFile{}, s.Files...)})
}
func handleACPDiff(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	s, ok := workspaceSessions.get(r.URL.Query().Get("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion introuvable"})
		return
	}
	path := r.URL.Query().Get("path")
	before, observed := s.FileBaselines[path]
	if !observed {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "fichier non suivi"})
		return
	}
	if agent, _ := acpAgentFor(s.RuntimeID); agent.Remote {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "fichier sur la machine distante : le diff est dans le fil"})
		return
	}
	roots := []*os.Root{}
	defer func() {
		for _, root := range roots {
			_ = root.Close()
		}
	}()
	for _, dir := range append([]string{s.Workdir}, s.AdditionalDirs...) {
		canonical, err := acpDirectory(dir)
		if err != nil || canonical != dir {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "dossier inaccessible"})
			return
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "dossier inaccessible"})
			return
		}
		roots = append(roots, root)
	}
	root, rel, _, err := acpScopedPath(roots, path)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	after, err := acpReadFile(root, rel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "lecture refusée"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "diff": acpUnifiedDiff(path, before, after)})
}
func handleACPDirs(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.UserHomeDir()
	}
	path, err := acpDirectory(path)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "dossier inaccessible"})
		return
	}
	dirs := []map[string]any{}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Stat(child)
		if err != nil || !info.IsDir() {
			continue
		}
		_, gitErr := os.Stat(filepath.Join(child, ".git"))
		dirs = append(dirs, map[string]any{"name": entry.Name(), "path": child, "is_git": gitErr == nil})
	}
	sendJSON(w, 200, map[string]any{"ok": true, "path": path, "dirs": dirs})
}
