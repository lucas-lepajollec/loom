package loom

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// llm_oai_router.go — front OpenAI public (HOST:PORT) devant llama-server.
//
// llama-server ne connaît qu'un GGUF. Les apps (Open WebUI, Continue…) appellent
// GET /v1/models puis POST /v1/chat/completions avec un `model`. On expose
// tous les presets et GGUF, et on ne charge le demandé qu'à la première
// requête d'inférence — lister ou « sélectionner » ne démarre rien.

type oaiEntry struct {
	ID     string
	Kind   string // "preset" | "gguf"
	Preset string // chemin du .env
	Model  string // chemin / valeur MODEL
}

func llamaBackendURL() *url.URL {
	return &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", llamaBackendPort())}
}

func oaiKeepCurrent(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "loom", "local", "default", "llama", "llama-server",
		"gpt-3.5-turbo", "gpt-4", "gpt-4o", "gpt-4-turbo", "gpt-4.1":
		return true
	}
	return false
}

func oaiNorm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.ToLower(filepath.Base(s))
}

func oaiSame(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	if oaiNorm(a) != "" && oaiNorm(a) == oaiNorm(b) {
		return true
	}
	if pa, err := resolveServeModelPath(a); err == nil {
		if pb, err2 := resolveServeModelPath(b); err2 == nil {
			return filepath.Clean(pa) == filepath.Clean(pb)
		}
	}
	return false
}

func listOAIWeights() []oaiEntry {
	var out []oaiEntry
	seen := map[string]bool{}
	used := map[string]bool{}
	home := LoomHome()
	for _, dir := range modelDirs() {
		isHome := normDir(dir) == normDir(home) || normDir(dir) == normDir(modelsDir())
		for _, g := range listGGUFFiles(dir) {
			if ggufIsMmproj(g.Name) || seen[normDir(g.Path)] {
				continue
			}
			seen[normDir(g.Path)] = true
			parent := filepath.Dir(g.Path)
			value := g.Path
			if isHome && normDir(parent) == normDir(dir) {
				value = g.Name
			}
			id := g.Name
			if used[strings.ToLower(id)] {
				id = g.Path
			}
			used[strings.ToLower(id)] = true
			out = append(out, oaiEntry{ID: id, Kind: "gguf", Model: value})
		}
	}
	return out
}

func oaiCatalog() []oaiEntry {
	used := map[string]bool{}
	var out []oaiEntry
	presets, err := ListPresets()
	if err == nil {
		for _, p := range presets {
			id := strings.TrimSpace(p.Name)
			if id == "" || used[strings.ToLower(id)] || oaiKeepCurrent(id) {
				id = p.ID
			}
			used[strings.ToLower(id)] = true
			model := ""
			if body, err := ReadPreset(p.ID); err == nil {
				model = strings.TrimSpace(parseEnv(body)["MODEL"])
			}
			out = append(out, oaiEntry{ID: id, Kind: "preset", Preset: p.Path, Model: model})
		}
	}
	for _, e := range listOAIWeights() {
		used[strings.ToLower(e.ID)] = true
		out = append(out, e)
	}
	return out
}

func resolveOAIModel(id string) (oaiEntry, bool) {
	id = strings.TrimSpace(id)
	if id == "" || oaiKeepCurrent(id) {
		return oaiEntry{}, false
	}
	cat := oaiCatalog()
	for _, e := range cat {
		if strings.EqualFold(strings.TrimSpace(e.ID), id) {
			return e, true
		}
	}
	for _, e := range cat {
		if e.Kind == "gguf" && oaiSame(id, e.Model) {
			return e, true
		}
	}
	for _, e := range cat {
		if e.Kind != "preset" {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(e.Preset), ".env")
		if oaiSame(id, base) || oaiSame(id, e.ID) || oaiSame(id, filepath.Base(e.Preset)) {
			return e, true
		}
	}
	for _, e := range cat {
		if oaiSame(id, e.Model) {
			return e, true
		}
	}
	return oaiEntry{}, false
}

func oaiLoadedIDs() []string {
	cfg := ReadConfig()
	var ids []string
	if m := strings.TrimSpace(cfg["MODEL"]); m != "" {
		ids = append(ids, m, filepath.Base(m))
	}
	if pid := strings.TrimSpace(getStr(bkState, "active_preset")); pid != "" {
		ids = append(ids, pid)
		if body, err := ReadPreset(pid); err == nil {
			ids = append(ids, presetDisplayName(body, pid))
		}
	}
	return ids
}

func oaiAlreadyLoaded(id string) bool {
	if oaiKeepCurrent(id) {
		return true
	}
	for _, cur := range oaiLoadedIDs() {
		if oaiSame(id, cur) {
			return true
		}
	}
	if e, ok := resolveOAIModel(id); ok {
		if e.Kind == "preset" {
			active := strings.TrimSpace(getStr(bkState, "active_preset"))
			want := strings.TrimSuffix(filepath.Base(e.Preset), ".env")
			return active != "" && (oaiSame(active, want) || oaiSame(active, e.ID))
		}
		return oaiSame(e.Model, ReadConfig()["MODEL"])
	}
	return false
}

func applyOAIEntry(e oaiEntry) error {
	if e.Kind == "preset" {
		return applyPresetFile(e.Preset)
	}
	return loadNakedModel(e.Model)
}

func ensureOAIModel(id string) error {
	if oaiAlreadyLoaded(id) {
		return nil
	}
	clearOAIRuntime()
	e, ok := resolveOAIModel(id)
	if !ok {
		return fmt.Errorf("unknown model: %s", id)
	}
	if err := applyOAIEntry(e); err != nil {
		return err
	}
	if err := restartLlamaForOAI(); err != nil {
		return err
	}
	return waitLlamaReady(10 * time.Minute)
}

func restartLlamaForOAI() error {
	if routerReachable() {
		return routerActivate()
	}
	if ownedLlamaManaged() {
		return restartOwnedLlama()
	}
	return serviceAction("restart")
}

func waitLlamaReady(budget time.Duration) error {
	deadline := time.Now().Add(budget)
	port := llamaBackendPort()
	if !ownedLlamaManaged() {
		port = LLMPort()
	}
	client := &http.Client{Timeout: 3 * time.Second}
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
		if err != nil {
			return err
		}
		localAuthHeader(req)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("health %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(400 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("the engine is not ready: %v", last)
}

func oaiModelsJSON() []byte {
	type item struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	out := struct {
		Object string `json:"object"`
		Data   []item `json:"data"`
	}{Object: "list"}
	now := time.Now().Unix()
	for _, e := range oaiCatalog() {
		out.Data = append(out.Data, item{ID: e.ID, Object: "model", Created: now, OwnedBy: "loom"})
	}
	b, _ := json.Marshal(out)
	return b
}

func oaiWriteJSON(w http.ResponseWriter, code int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func oaiError(w http.ResponseWriter, code int, typ, msg, param, errCode string) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    typ,
			"param":   param,
			"code":    errCode,
		},
	})
	oaiWriteJSON(w, code, payload)
}

func oaiBearer(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func oaiKeyOK(r *http.Request) bool {
	key := readAPIKey()
	if key == "" {
		key = strings.TrimSpace(ReadConfig()["API_KEY"])
	}
	if key == "" {
		return !lanExposed()
	}
	got := oaiBearer(r)
	if len(got) != len(key) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}

func rewriteOAIModel(body []byte, id string) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = id
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func peekOAIModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return strings.TrimSpace(req.Model)
}

func oaiNeedsEnsure(path, method string) bool {
	if method != http.MethodPost && method != http.MethodPut {
		return false
	}
	switch path {
	case "/v1/chat/completions", "/v1/completions", "/v1/embeddings",
		"/v1/responses", "/v1/messages", "/v1/messages/count_tokens", "/completion", "/v1/complete":
		return true
	}
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/completions")
}

func newOAIRouter(injectKey string) http.Handler {
	lp := httputil.NewSingleHostReverseProxy(llamaBackendURL())
	lp.FlushInterval = -1
	base := lp.Director
	lp.Director = func(req *http.Request) {
		base(req)
		req.Host = llamaBackendURL().Host
		req.URL.Host = llamaBackendURL().Host
		req.URL.Scheme = "http"
		if injectKey != "" && req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+injectKey)
		}
	}
	lp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		oaiError(w, http.StatusBadGateway, "api_error", "llama-server unreachable: "+e.Error(), "", "server_error")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/health" && p != "/props" && p != "/metrics" && !strings.HasPrefix(p, "/slots") && !strings.HasPrefix(p, "/v1") && p != "/completion" {
			http.Error(w, "not found (endpoint OpenAI: /v1/*)", http.StatusNotFound)
			return
		}
		router := routerModeCached()
		if router && (p == "/health" || p == "/v1/health") {
			routerHealth(w)
			return
		}
		if !oaiKeyOK(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="loom"`)
			oaiError(w, http.StatusUnauthorized, "invalid_request_error", "Invalid API key", "", "invalid_api_key")
			return
		}
		if r.Method == http.MethodGet && (p == "/v1/models" || p == "/v1/models/") {
			oaiWriteJSON(w, http.StatusOK, oaiModelsJSON())
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/models/") {
			id := strings.TrimPrefix(p, "/v1/models/")
			id = strings.TrimSuffix(id, "/")
			if oaiAlreadyLoaded(id) || func() bool { _, ok := resolveOAIModel(id); return ok }() {
				item, _ := json.Marshal(map[string]any{
					"id": id, "object": "model", "created": time.Now().Unix(), "owned_by": "loom",
				})
				oaiWriteJSON(w, http.StatusOK, item)
				return
			}
			oaiError(w, http.StatusNotFound, "invalid_request_error", "The model `"+id+"` does not exist", "model", "model_not_found")
			return
		}
		if oaiNeedsEnsure(p, r.Method) {
			body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
			if err != nil {
				oaiError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), "", "invalid_request")
				return
			}
			_ = r.Body.Close()
			adapted, split, aerr := adaptOAICompletion(body)
			if aerr == nil {
				body = adapted
			}
			want := peekOAIModel(body)
			if want != "" && !oaiAlreadyLoaded(want) {
				llamaOwner.SwitchLock().Lock()
				switchErr := ensureOAIModel(want)
				llamaOwner.SwitchLock().Unlock()
				if switchErr != nil {
					err = switchErr
					if strings.Contains(err.Error(), "unknown") {
						oaiError(w, http.StatusNotFound, "invalid_request_error", "The model `"+want+"` does not exist", "model", "model_not_found")
						return
					}
					oaiError(w, http.StatusServiceUnavailable, "api_error", err.Error(), "model", "model_unavailable")
					return
				}
				loaded := filepath.Base(strings.TrimSpace(ReadConfig()["MODEL"]))
				if loaded != "" && !router {
					body = rewriteOAIModel(body, loaded)
				}
			}
			if aerr == nil && (len(split.Run) > 0 || len(split.Extra) > 0) {
				llamaOwner.SwitchLock().Lock()
				ovErr := oaiRuntimeApply(split.Run, split.Extra)
				llamaOwner.SwitchLock().Unlock()
				if ovErr != nil {
					oaiError(w, http.StatusServiceUnavailable, "api_error", ovErr.Error(), "", "model_unavailable")
					return
				}
			}
			if router {
				// Le router choisit l'instance par le champ model : on y met la
				// section Loom qui sert maintenant (modèle choisi ou variante API).
				cur := routerCurrentName()
				if cur == "" {
					oaiError(w, http.StatusServiceUnavailable, "api_error", "no model loaded", "model", "model_unavailable")
					return
				}
				body = rewriteOAIModel(body, cur)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		}
		if router && r.Method == http.MethodGet && r.URL.Query().Get("model") == "" &&
			(p == "/props" || p == "/metrics" || strings.HasPrefix(p, "/slots")) {
			if cur := routerCurrentName(); cur != "" {
				q := r.URL.Query()
				q.Set("model", cur)
				r.URL.RawQuery = q.Encode()
			}
		}
		lp.ServeHTTP(w, r)
	})
}

// routerHealth garde le sens historique de /health : 200 seulement quand le
// modèle choisi est chargé et prêt, 503 pendant un chargement ou sans modèle.
func routerHealth(w http.ResponseWriter) {
	cur := routerCurrentName()
	if cur == "" {
		oaiWriteJSON(w, http.StatusServiceUnavailable, []byte(`{"status":"no model loaded"}`))
		return
	}
	m, ok := routerModelStatus(cur)
	switch {
	case ok && m.Status == "loaded":
		oaiWriteJSON(w, http.StatusOK, []byte(`{"status":"ok"}`))
	case ok && m.Failed:
		oaiWriteJSON(w, http.StatusServiceUnavailable, []byte(`{"status":"model failed to load"}`))
	default:
		oaiWriteJSON(w, http.StatusServiceUnavailable, []byte(`{"status":"loading model"}`))
	}
}

func oaiPublicHandler() http.Handler { return newOAIRouter("") }

func oaiListenAddr() string {
	return fmt.Sprintf("%s:%d", engineHost(), LLMPort())
}

func serveOAIFront(errc chan<- error) {
	addr := oaiListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		errc <- fmt.Errorf("OpenAI listen %s: %w", addr, err)
		return
	}
	fmt.Fprintf(os.Stderr, "[loom serve] /v1 on %s  → llama-server 127.0.0.1:%d\n", addr, llamaBackendPort())
	srv := &http.Server{
		Handler:           oaiPublicHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc <- srv.Serve(ln)
}
