// web_chat.go — endpoints de chat du serveur web local : envoi/stop/reset,
// flux SSE d'abonnement à la conversation serveur (voir chat_conversation.go).
package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

// capsFromBody dérive les capacités d'un tour à partir de la configuration
// machine et des surcharges portées par la requête.
//
// ⚠️ Une surcharge ne peut que RESTREINDRE. Elle pouvait auparavant rallumer le
// mode agent : un simple {"agent":true} redonnait bash, write, edit et les
// outils MCP alors que l'interrupteur de la machine était sur OFF. Comme l'API
// n'est pas protégée par défaut et écoute sur 0.0.0.0, l'interrupteur ne
// garantissait donc rien. Il redevient une vraie fermeture : ce qui est éteint
// sur la machine ne peut pas être rallumé par un client.
func capsFromBody(body chatReq) Caps {
	caps := globalCaps()
	caps.Agent = false
	// Internet / MCP : opt-in par tour. Sans champ, c'est off.
	if body.Internet != nil {
		caps.Internet = caps.Internet && *body.Internet
	} else {
		caps.Internet = false
	}
	if body.MCP != nil {
		caps.MCP = *body.MCP
	}
	return caps
}

// runChatStream est désormais un pur ABONNÉ au journal de la conversation serveur :
// il rejoue Log[body.From:] puis suit le direct, jusqu'à ce que la connexion (ctx)
// se ferme. La GÉNÉRATION est lancée séparément par /api/chat/send dans une
// goroutine détachée — fermer le navigateur n'arrête donc plus rien. Partagé par
// handleChat (clair) et handleE2EChat (chiffré).
func runChatStream(ctx context.Context, body chatReq, emit func(map[string]any) bool) {
	conv.Subscribe(ctx, body.From, emit)
}

// handleChatSend ajoute un message et lance la génération en arrière-plan. Réponse
// req/resp (les événements arrivent par le flux d'abonnement). Passe par le proxy
// tunnel /api/e2e/req pour app.loom.local — aucun code E2E spécifique requis.
func handleChatSend(w http.ResponseWriter, r *http.Request) {
	var body chatReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Un envoi SANS texte mais AVEC pièce jointe est légitime (« tiens, regarde »).
	files := attachFiles(body.Files)
	if strings.TrimSpace(body.Message) == "" && len(files) == 0 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "empty message"})
		return
	}
	if err := conv.StartTurn(body.Message, files, capsFromBody(body), body.Temperature); err != nil {
		// 409 = occupé (génération en cours) ; 503 = modèle pas prêt.
		code := 503
		if err == ErrBusy {
			code = 409
		}
		sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleChatStop(w http.ResponseWriter, r *http.Request) {
	conv.Stop()
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatReset : « clear chat ». La conversation courante n'est pas jetée mais
// ARCHIVÉE dans l'historique (récupérable dans le modal Historique), puis une
// conversation vierge démarre.
func handleChatReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"project_id"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	id := conv.NewSession()
	if pid := strings.TrimSpace(body.ProjectID); pid != "" {
		if _, ok := getProject(pid); ok {
			conv.setActiveProject(pid)
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "active": id, "project_id": conv.currentProject()})
}

// handleChatHistory (GET) : liste des sessions + id de la session active (pour
// que l'UI marque « en cours »).
func handleChatHistory(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, 200, map[string]any{
		"ok":            true,
		"conversations": listArchives(),
		"active":        conv.currentID(),
		"project_id":    conv.currentProject(),
		"projects":      listProjects(),
	})
}

// handleChatHistoryRestore (POST {id}) : ouvre une session comme conversation
// active (la courante est d'abord sauvegardée dans SA session).
func handleChatHistoryRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := conv.OpenSession(body.ID); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatHistoryDelete (POST {id}) : supprime DÉFINITIVEMENT une conversation
// archivée.
func handleChatHistoryDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := conv.DeleteSession(body.ID); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatHistoryRename (POST {id, title}) : renomme une conversation
// archivée. Titre vide = re-dérive le titre automatique.
func handleChatHistoryRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := renameArchive(body.ID, body.Title); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Si c'est la session active, garder son nom en phase (sinon le prochain
	// upsertSession réécraserait l'archive avec l'ancien nom).
	conv.setActiveTitleIfMatch(body.ID, body.Title)
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatHistoryFav (POST {id, fav}) : épingle/dépingle une conversation
// archivée en favori.
func handleChatHistoryFav(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID  string `json:"id"`
		Fav bool   `json:"fav"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := setArchiveFav(body.ID, body.Fav); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	conv.setActiveFavIfMatch(body.ID, body.Fav)
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatHistoryClear (POST) : supprime toutes les conversations archivées
// SAUF les favoris.
func handleChatHistoryClear(w http.ResponseWriter, r *http.Request) {
	n := deleteNonFavArchives(conv.currentID())
	sendJSON(w, 200, map[string]any{"ok": true, "deleted": n})
}

func handleChatHistoryMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := setArchiveProject(body.ID, strings.TrimSpace(body.ProjectID)); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if archive, ok := loadArchive(body.ID); ok {
		workspaceSessions.syncNativeArchive(archive)
	}
	queueDiscussionTranscript(body.ID)
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "projects": listProjects()})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	p, err := createProject(body.Name)
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "project": p})
}

func handleProjectsRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := renameProject(body.ID, body.Name); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleProjectsDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !legacyControlDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "id manquant"})
		return
	}
	if err := deleteProject(body.ID); err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleChatCompact lance une compaction manuelle du contexte (bouton UI). La
// progression est diffusée via le flux d'abonnement (compacting/compacted).
func handleChatCompact(w http.ResponseWriter, r *http.Request) {
	if err := conv.CompactNow(); err != nil {
		code := 503
		if err == ErrBusy {
			code = 409
		}
		sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleChatState(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, 200, conv.state())
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var body chatReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	web.SSEHeaders(w, "no-cache, no-transform")
	flusher, _ := w.(http.Flusher)
	mu, stop := sseHeartbeat(w, flusher)
	defer stop()
	emit := func(obj map[string]any) bool {
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": obj}}})
		return web.WriteSSE(w, flusher, mu, b) == nil
	}
	runChatStream(r.Context(), body, emit)
}
