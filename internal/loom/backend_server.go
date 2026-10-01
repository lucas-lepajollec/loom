package loom

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// backend_server.go — surface « Serveur » : llama-server expose déjà /v1
// (OpenAI), /slots et /health. Loom observe, il ne réimplémente pas
// l'inférence. Un llama-server suffit ; le dépôt llama.cpp n'est pas requis.
//
// GET  /api/server  → état (écoute, modèle, slots live, NP, stats, récentes)
// POST /api/server  {np} → slots parallèles (NP), puis redémarrage

func llamaGET(path string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, engineBase()+path, nil)
	if err != nil {
		return nil, 0, err
	}
	authHeader(req)
	resp, err := healthClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

func serverNP() int {
	if v := oaiRuntimeGet("NP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	n, _ := strconv.Atoi(strings.TrimSpace(ReadConfig()["NP"]))
	if n < 1 {
		return 4
	}
	return n
}

func setServerNP(n int) error {
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	clearOAIRuntime()
	return SetConfigKey("NP", strconv.Itoa(n))
}

func handleServer(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			NP *int `json:"np"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if req.NP != nil {
			if err := setServerNP(*req.NP); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if serviceIsActive() {
				go func() {
					if err := restartLlamaEngine(); err != nil {
						fmt.Printf("%s redémarrage après NP: %v\n", red("[ERREUR]"), err)
					}
				}()
			}
		}
	}

	cfg := ReadConfig()
	net := networkStatus()
	port := LLMPort()
	loop := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	url := loop
	if net.Exposed {
		url = net.URL
	}
	model := strings.TrimSpace(cfg["MODEL"])
	if ownedLlamaManaged() && !ownedLlamaRunning() {
		model = ""
	}
	modelName := ""
	if model != "" {
		modelName = filepath.Base(model)
	}
	presetID := getStr(bkState, "active_preset")
	if model == "" {
		presetID = ""
	}
	presetName := ""
	if presetID != "" {
		if body, err := ReadPreset(presetID); err == nil {
			presetName = presetDisplayName(body, presetID)
		}
	}
	key := readAPIKey()

	out := map[string]any{
		"ok":           true,
		"running":      serviceIsActive(),
		"health":       false,
		"port":         port,
		"host":         net.Host,
		"lan":          net.Exposed,
		"url":          url,
		"url_local":    loop,
		"np":           serverNP(),
		"model":        model,
		"model_name":   modelName,
		"preset_id":    presetID,
		"preset_name":  presetName,
		"key_set":      key != "",
		"key_required": apiKeyRequired(),
		"slots":        map[string]any{"n": 0, "busy": 0, "items": []any{}, "ok": false},
		"recent":       []srvRecent{},
		"stats":        serverStats(0, 0),
	}
	if !serviceIsActive() {
		sendJSON(w, 200, out)
		return
	}
	healthy := healthCheck()
	out["health"] = healthy
	if !healthy {
		sendJSON(w, 200, out)
		return
	}
	raw, code, err := llamaGET("/slots")
	if err != nil || code != 200 {
		out["slots"] = map[string]any{"n": 0, "busy": 0, "items": []any{}, "ok": false}
		if code > 0 {
			out["slots_http"] = code
		}
		sendJSON(w, 200, out)
		return
	}
	slots := parseLlamaSlots(raw)
	recent, items := noteServerSlots(slots)
	busy := 0
	liveTokS := 0.0
	for _, it := range items {
		if b, _ := it["busy"].(bool); b {
			busy++
			if v, ok := it["toks"].(float64); ok {
				liveTokS += v
			}
		}
	}
	out["slots"] = map[string]any{"n": len(slots), "busy": busy, "items": items, "ok": true}
	out["recent"] = recent
	out["stats"] = serverStats(busy, liveTokS)
	if n := len(slots); n > 0 && serverNP() != n {
		out["np_live"] = n
	}
	sendJSON(w, 200, out)
}
