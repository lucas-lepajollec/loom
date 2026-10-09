package loom

import (
	"net/http"
	"regexp"
)

// Reprendre une discussion de harness dans un terminal : la CLI native du
// harness rouvre la même session (même identifiant, même dossier), sur cette
// machine ou sur la machine connectée qui l'héberge.

var nativeSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// nativeResumeCommand renvoie la commande qui reprend la session native, ou ""
// quand la CLI de ce harness n'a pas de reprise par identifiant connue.
func nativeResumeCommand(harness, id string) string {
	if !nativeSessionIDPattern.MatchString(id) {
		return ""
	}
	switch harness {
	case "claude-code":
		return "claude --resume " + id
	case "codex":
		return "codex resume " + id
	case "opencode":
		return "opencode --session " + id
	case "pi":
		return "pi --session " + id
	case "antigravity":
		return "agy --conversation " + id
	case "hermes":
		return "hermes --resume " + id
	}
	return ""
}

// GET /api/runtime/sessions/terminal?id=<discussion> → {ok, target, dir, command, title}
func handleRuntimeSessionTerminal(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	s, ok := workspaceSessions.get(r.URL.Query().Get("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion not found or locked"})
		return
	}
	runtimeID := s.NativeRuntimeID
	if runtimeID == "" {
		runtimeID = s.RuntimeID
	}
	adapter, found := registeredRuntimes.lookup(runtimeID)
	acp, isACP := adapter.(*acpAdapter)
	if !found || !isACP || s.NativeSessionID == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "this discussion has no native session to resume"})
		return
	}
	command := nativeResumeCommand(usageHarnessID(acp.agent), s.NativeSessionID)
	if command == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "terminal resume is not available for this harness"})
		return
	}
	target := "local"
	if acp.agent.Remote && acp.agent.Machine != "" {
		target = acp.agent.Machine
	}
	sendJSON(w, 200, map[string]any{"ok": true, "target": target, "dir": s.Workdir, "command": command, "title": acp.agent.Name})
}
