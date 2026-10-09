package loom

import (
	"errors"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// User-defined ACP harnesses: any agent speaking ACP on stdio, launched by a
// command the user chose (for example `ssh host hermes acp` for an agent on
// another machine). They join the runtime registry live, with id custom-<slug>.

const acpCustomState = "acp_custom_agents"

var acpCustomIDRe = regexp.MustCompile(`[^a-z0-9]+`)

func loadCustomACPAgents() []acpAgent {
	list := []acpAgent{}
	_ = getStoreJSON(bkState, acpCustomState, &list)
	return list
}

// registerCustomACPAgents (re)publishes the saved custom agents in the registry.
func registerCustomACPAgents() {
	for _, a := range loadCustomACPAgents() {
		a.Custom = true
		if geminiCustomAgent(a) {
			continue
		}
		registeredRuntimes.upsert(&acpAdapter{agent: a})
	}
}

func validCustomACPAgent(a acpAgent) (acpAgent, error) {
	if geminiCustomAgent(a) {
		return a, errors.New("Gemini CLI is not offered; use Antigravity")
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Command = strings.TrimSpace(a.Command)
	if a.Name == "" || len([]rune(a.Name)) > 40 || a.Command == "" || len(a.Command) > 512 || strings.ContainsAny(a.Command, "\n\r\x00") {
		return a, errors.New("name (40 characters) and command required")
	}
	if len(a.Args) > 32 {
		return a, errors.New("maximum 32 arguments")
	}
	for _, arg := range a.Args {
		if len(arg) > 512 || strings.ContainsAny(arg, "\n\r\x00") {
			return a, errors.New("invalid argument")
		}
	}
	if a.ID == "" {
		slug := strings.Trim(acpCustomIDRe.ReplaceAllString(strings.ToLower(a.Name), "-"), "-")
		if slug == "" {
			slug = "agent"
		}
		a.ID = "custom-" + slug
	}
	if !strings.HasPrefix(a.ID, "custom-") {
		return a, errors.New("reserved ID")
	}
	a.RegistryID, a.RegistryVersion, a.RegistryPackage, a.RegistryKind = "", "", "", ""
	a.Logo = logoForCustom(a)
	a.Detect, a.Docs, a.Custom = nil, "", true
	return a, nil
}

func logoForCustom(a acpAgent) string {
	text := strings.ToLower(a.Name + " " + a.Command + " " + strings.Join(a.Args, " "))
	for _, known := range []string{"hermes", "claude", "codex", "antigravity", "deepseek", "pi", "kimi", "qwen", "cursor", "goose", "opencode"} {
		if strings.Contains(text, known) {
			return known
		}
	}
	return ""
}

// GET: list. POST {agent} saves (create or update). POST /delete {id}.
func handleCustomACP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "agents": offeredCustomACPAgents()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req acpAgent
	if !workspaceDecode(w, r, &req) {
		return
	}
	a, err := validCustomACPAgent(req)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if existing, ok := registeredRuntimes.lookup(a.ID); ok {
		if ad, isACP := existing.(*acpAdapter); !isACP || !ad.agent.Custom {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "a harness already has this name"})
			return
		}
	}
	list := loadCustomACPAgents()
	replaced := false
	for i := range list {
		if list[i].ID == a.ID {
			list[i], replaced = a, true
		}
	}
	if !replaced {
		if len(list) >= 32 {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "maximum 32 custom harnesses"})
			return
		}
		list = append(list, a)
	}
	if err := putStoreJSON(bkState, acpCustomState, list); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	registeredRuntimes.upsert(&acpAdapter{agent: a})
	sendJSON(w, 200, map[string]any{"ok": true, "agent": a})
}

func handleCustomACPDelete(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	list := loadCustomACPAgents()
	kept := []acpAgent{}
	for _, a := range list {
		if a.ID != req.ID {
			kept = append(kept, a)
		}
	}
	if len(kept) == len(list) {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness not found"})
		return
	}
	if err := putStoreJSON(bkState, acpCustomState, kept); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	registeredRuntimes.remove(req.ID)
	sendJSON(w, 200, map[string]any{"ok": true})
}

// remoteWorkdir validates a folder that lives on the agent's machine: Loom
// cannot inspect it, so it only requires a clean absolute path.
func remoteWorkdir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.ContainsAny(p, "\n\r\x00") || len(p) > 1024 {
		return "", errors.New("absolute path required on the remote machine")
	}
	if regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(p) {
		return strings.ToUpper(p[:1]) + path.Clean(strings.ReplaceAll(p[1:], `\`, "/")), nil
	}
	if !strings.HasPrefix(p, "/") {
		return "", errors.New("absolute path required on the remote machine")
	}
	return path.Clean(p), nil
}

func acpAgentFor(runtimeID string) (acpAgent, bool) {
	a, ok := registeredRuntimes.lookup(runtimeID)
	if !ok {
		return acpAgent{}, false
	}
	ad, ok := a.(*acpAdapter)
	if !ok {
		return acpAgent{}, false
	}
	return ad.agent, true
}

// Recognize old user configurations without offering Gemini CLI as an agent.
func geminiCustomAgent(a acpAgent) bool {
	text := strings.ToLower(a.Name + " " + a.Command + " " + strings.Join(a.Args, " "))
	for _, arg := range a.Args {
		if arg == "gemini" || strings.HasPrefix(arg, "@google/gemini-cli") {
			return true
		}
	}
	return strings.Contains(text, "gemini cli") || strings.Contains(text, "gemini-cli") || path.Base(a.Command) == "gemini" || strings.HasSuffix(a.ID, "-gemini")
}
func offeredCustomACPAgents() []acpAgent {
	out := []acpAgent{}
	for _, a := range loadCustomACPAgents() {
		if !geminiCustomAgent(a) {
			out = append(out, a)
		}
	}
	return out
}
