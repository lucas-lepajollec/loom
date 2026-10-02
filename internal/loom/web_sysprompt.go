// web_sysprompt.go — prompt système personnalisé de l'utilisateur, persisté
// CÔTÉ SERVEUR (en base) et partagé entre appareils, comme la conversation
// elle-même. Historique : avant la conversation serveur (v0.4.x),
// l'UI envoyait son prompt système dans chaque requête /api/chat ; depuis,
// /api/chat/send ne porte que le message → le champ de l'UI n'avait plus aucun
// effet. Il est maintenant lu ici par la génération (chat_conversation.go), et
// InjectSkills (llm_client.go) le fusionne avec le préambule agent.
package loom

import (
	"encoding/json"
	"net/http"
	"strings"
)

// readSysPrompt renvoie le prompt système personnalisé ("" si absent).
func readSysPrompt() string { return getStr(bkState, "sysprompt") }

func saveSysPrompt(text string) error {
	return putStr(bkState, "sysprompt", strings.TrimSpace(text))
}

// effectiveSysPrompt : prompt du modèle/preset s'il est défini, sinon le
// prompt global des Réglages. Les deux vides = aucun message system.
func effectiveSysPrompt() string {
	if sp := strings.TrimSpace(ReadConfig()["SYSPROMPT"]); sp != "" {
		return sp
	}
	return strings.TrimSpace(readSysPrompt())
}

// handleModelSysPrompt pose SYSPROMPT sur la config live, sans redémarrer le
// moteur : le prompt est relu à chaque tour (chat_conversation.go).
func handleModelSysPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := SetConfigKey("SYSPROMPT", strings.TrimSpace(body.Text)); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "text": strings.TrimSpace(ReadConfig()["SYSPROMPT"])})
}

// handleSysPrompt :
//
//	GET  → {ok, text}
//	POST {text} → enregistre ("" = efface) puis renvoie {ok, text}
func handleSysPrompt(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendJSON(w, 200, map[string]any{"ok": true, "text": readSysPrompt()})
	case http.MethodPost:
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := saveSysPrompt(body.Text); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "text": readSysPrompt()})
	default:
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
	}
}
