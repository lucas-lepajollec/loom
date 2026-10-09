package loom

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

func forwardEngineServiceControl(w http.ResponseWriter, r *http.Request) bool {
	// Le front d'inférence possède le compteur live, même lorsque l'interface
	// web ou le node tourne dans un autre processus.
	if !ownedLlamaManaged() && strings.HasPrefix(r.URL.Path, "/api/engine/") && currentEngineNode() == nil {
		var body []byte
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			if err != nil || len(body) > 1<<20 {
				sendJSON(w, 400, map[string]any{"error": "invalid control request"})
				return true
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, engineBase()+strings.Replace(r.URL.Path, "/api/", "/loom/", 1), bytes.NewReader(body))
		if err == nil {
			req.Host = r.Host
			req.Header.Set("Authorization", "Bearer "+loomInferenceSecret())
			req.Header.Set("Content-Type", "application/json")
			if resp, err := nodeClient.Do(req); err == nil {
				defer resp.Body.Close()
				// An engine process older than this interface has no control
				// endpoint: say so instead of passing its plain-text 404 on.
				if resp.StatusCode == http.StatusNotFound && !strings.Contains(resp.Header.Get("Content-Type"), "json") {
					sendJSON(w, http.StatusConflict, map[string]any{"ok": false, "error_code": "engine_outdated", "error": "the engine process is older than this Loom; restart or update it"})
					return true
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				_, _ = io.Copy(w, resp.Body)
				return true
			}
		}
	}
	return false
}

func handleEngineService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if forwardEngineServiceControl(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		sendJSON(w, 200, currentEngineService().snapshot())
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Idle  *int `json:"idle_unload_minutes"`
		Wait  *int `json:"swap_wait_seconds"`
		Grace *int `json:"interactive_grace_minutes"`
		Max   *int `json:"models_max"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	updates := map[string]string{}
	for _, field := range []struct {
		key      string
		value    *int
		min, max int
	}{
		{"ENGINE_IDLE_UNLOAD", req.Idle, 0, 10080}, {"ENGINE_SWAP_WAIT", req.Wait, 1, 3600}, {"ENGINE_INTERACTIVE_GRACE", req.Grace, 0, 10080}, {"MODELS_MAX", req.Max, 0, 64},
	} {
		if field.value == nil {
			continue
		}
		if *field.value < field.min || *field.value > field.max {
			sendJSON(w, 400, map[string]any{"error": fmt.Sprintf("%s must be between %d and %d", field.key, field.min, field.max)})
			return
		}
		updates[field.key] = strconv.Itoa(*field.value)
	}
	// Une seule écriture, après validation complète. Changer --models-max est une
	// reconfiguration explicite du moteur, toujours après le drain des streams.
	cfg := ReadConfig()
	previousMax := routerModelsMax()
	for key, value := range updates {
		cfg[key] = value
	}
	if err := WriteConfig(cfg); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if req.Max != nil && *req.Max != previousMax && ownedLlamaManaged() && serviceRouterMode() {
		if err := currentEngineService().maintain(r.Context(), routerCurrentName(), serviceApplyCapacity); err != nil {
			code := "model_unavailable"
			if errors.Is(err, errEngineBusy) {
				code = "model_busy"
				w.Header().Set("Retry-After", "1")
			}
			oaiError(w, 503, "api_error", err.Error(), "models_max", code)
			return
		}
	}
	sendJSON(w, 200, currentEngineService().snapshot())
}

type engineKeyUpdate struct {
	ID          string    `json:"id"`
	Name        *string   `json:"name"`
	Allowed     *[]string `json:"allowed_models"`
	Concurrency *int      `json:"max_concurrency"`
	RPM         *int      `json:"requests_per_minute"`
	Priority    *string   `json:"priority"`
}

func applyEngineKeyUpdate(key *engineAPIIdentity, req engineKeyUpdate) error {
	if req.Name != nil {
		key.Name = strings.TrimSpace(*req.Name)
	}
	if key.Name == "" || len(key.Name) > 128 {
		return fmt.Errorf("name must contain 1 to 128 bytes")
	}
	if req.Allowed != nil {
		if len(*req.Allowed) > 256 {
			return fmt.Errorf("too many allowed_models")
		}
		key.Allowed = []string{}
		for _, m := range *req.Allowed {
			m = strings.TrimSpace(m)
			if m == "" || len(m) > 4096 {
				return fmt.Errorf("invalid allowed model")
			}
			key.Allowed = append(key.Allowed, m)
		}
	}
	if req.Concurrency != nil {
		key.Concurrency = *req.Concurrency
	}
	if req.RPM != nil {
		key.RPM = *req.RPM
	}
	if key.Concurrency < 0 || key.Concurrency > 10000 || key.RPM < 0 || key.RPM > 1000000 {
		return fmt.Errorf("invalid key limits")
	}
	if req.Priority != nil {
		key.Priority = *req.Priority
	}
	if key.Priority != "interactive" && key.Priority != "background" {
		return fmt.Errorf("priority must be interactive or background")
	}
	return nil
}

func engineClientEndpoint(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if isEngineWorker() {
		return scheme + "://" + r.Host + "/v1"
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(LLMPort())) + "/v1"
}

func handleEngineKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if forwardEngineServiceControl(w, r) {
		return
	}
	if r.URL.Path != "/api/engine/keys" && r.URL.Path != "/loom/engine/keys" && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if r.Method != http.MethodGet && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	engineKeysMu.Lock()
	defer engineKeysMu.Unlock()
	keys, err := loadEngineKeysLocked()
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": "key store unavailable"})
		return
	}
	if r.Method == http.MethodGet {
		out := []engineAPIIdentity{}
		foundDefault := false
		for _, k := range keys {
			out = append(out, k.engineAPIIdentity)
			foundDefault = foundDefault || k.ID == "default"
		}
		if !foundDefault {
			out = append(out, defaultEngineIdentity())
		}
		sendJSON(w, 200, map[string]any{"keys": out, "endpoint": engineClientEndpoint(r)})
		return
	}
	var req engineKeyUpdate
	if !workspaceDecode(w, r, &req) {
		return
	}
	action := strings.TrimPrefix(strings.Replace(r.URL.Path, "/loom/", "/api/", 1), "/api/engine/keys")
	index := -1
	for i := range keys {
		if keys[i].ID == req.ID {
			index = i
			break
		}
	}
	if req.ID == "default" && index < 0 {
		keys = append(keys, engineKeyRecord{engineAPIIdentity: defaultEngineIdentity()})
		index = len(keys) - 1
	}
	var secret string
	switch action {
	case "":
		key := engineKeyRecord{engineAPIIdentity: engineAPIIdentity{Name: "", Allowed: []string{}, Priority: "interactive", Created: engineKeyNow()}}
		if err := applyEngineKeyUpdate(&key.engineAPIIdentity, req); err != nil {
			sendJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		secret, err = newEngineSecret()
		if err == nil {
			key.ID = strings.TrimPrefix(secret, "sk-loom-")[:16]
			key.Hash = hashWebKey(secret)
			keys = append(keys, key)
			index = len(keys) - 1
		}
	case "/update", "/delete", "/rotate":
		if index < 0 {
			sendJSON(w, 404, map[string]any{"error": "key not found"})
			return
		}
		switch action {
		case "/update":
			err = applyEngineKeyUpdate(&keys[index].engineAPIIdentity, req)
		case "/delete":
			if req.ID == "default" {
				err = writeAPIKey("")
			}
			if err == nil {
				keys = append(keys[:index], keys[index+1:]...)
			}
		case "/rotate":
			secret, err = newEngineSecret()
			if err == nil {
				keys[index].Hash = hashWebKey(secret)
				if req.ID == "default" {
					err = writeAPIKey(secret)
				}
			}
		}
	default:
		sendJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if err := saveEngineKeysLocked(keys, action == ""); err != nil {
		sendJSON(w, 500, map[string]any{"error": "could not save key"})
		return
	}
	switch action {
	case "":
		sendJSON(w, 200, map[string]any{"key": keys[index].engineAPIIdentity, "secret": secret})
	case "/rotate":
		sendJSON(w, 200, map[string]any{"secret": secret})
	case "/update":
		sendJSON(w, 200, map[string]any{"key": keys[index].engineAPIIdentity})
	default:
		sendJSON(w, 200, map[string]any{"ok": true})
	}
}
