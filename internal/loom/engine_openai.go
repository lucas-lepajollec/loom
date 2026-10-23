package loom

import (
	"fmt"
	"net/http"
)

// Ce qu'une application tierce (Tradoc, Open WebUI, Continue…) doit savoir pour
// utiliser les modèles de Loom par son API compatible OpenAI : l'adresse, si
// une clé est exigée, et les noms de modèles exactement comme /v1/models les
// annonce. La route appartient à la machine du moteur : avec un moteur lié, la
// machine distante répond pour ses propres modèles et sa propre adresse.
func handleEngineOpenAI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	models := []map[string]string{}
	for _, e := range oaiCatalog() {
		models = append(models, map[string]string{"id": e.ID, "kind": e.Kind})
	}
	engineKeysMu.Lock()
	keys, _ := loadEngineKeysLocked()
	engineKeysMu.Unlock()
	st := networkStatus()
	out := map[string]any{
		"ok":           true,
		"models":       models,
		"exposed":      st.Exposed,
		"port":         st.Port,
		"firewall":     st.Firewall,
		"hint":         st.Hint,
		"url_local":    fmt.Sprintf("http://127.0.0.1:%d/v1", st.Port),
		"url_lan":      st.URL,
		"key_required": apiKeyRequired() || engineAPIKey() != "" || len(keys) > 0,
		"worker":       isEngineWorker(),
	}
	// Un nœud moteur sert /v1 sur son propre port de contrôle.
	if isEngineWorker() {
		out["url_lan"] = engineClientEndpoint(r)
		out["url_local"] = out["url_lan"]
	}
	sendJSON(w, 200, out)
}
