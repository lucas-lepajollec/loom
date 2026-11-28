package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
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

const loomKeyPlaceholder = "<LOOM_API_KEY>"

type harnessSnippet struct {
	File    string `json:"file"`
	Content string `json:"content"`
	LAN     string `json:"lan,omitempty"`
	Note    string `json:"note,omitempty"`
}

func snippetJSON(value any) string {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
	return out.String()
}

func snippetQuote(value string) string {
	// JSON double-quoted strings are also valid YAML and TOML strings for
	// the settings here (URLs and model IDs).
	return strings.TrimSuffix(snippetJSON(value), "\n")
}
func snippetShell(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func makeHarnessSnippets(base, model string, required bool) map[string]harnessSnippet {
	key := "loom"
	if required {
		key = loomKeyPlaceholder
	}
	provider := map[string]any{
		"name":     "Loom",
		"package":  "@opencode/ai/providers/openai-compatible",
		"settings": map[string]string{"baseURL": base},
		"models":   map[string]any{model: map[string]string{"name": model}},
	}
	codexKey := ""
	if required {
		provider["env"] = []string{"LOOM_API_KEY"}
		codexKey = "env_key = \"LOOM_API_KEY\"\n"
	}
	jsonConfig := snippetJSON(map[string]any{
		"$schema":   "https://opencode.ai/config.json",
		"providers": map[string]any{"loom": provider},
		"model":     "loom/" + model,
	})
	codex := fmt.Sprintf("model = %s\nmodel_provider = \"loom\"\n\n[model_providers.loom]\nname = \"Loom\"\nbase_url = %s\nwire_api = \"responses\"\n%s", snippetQuote(model), snippetQuote(base), codexKey)
	pi := snippetJSON(map[string]any{"providers": map[string]any{"loom": map[string]any{
		"baseUrl": base, "api": "openai-completions", "apiKey": key,
		"models": []map[string]any{{"id": model, "name": model}},
	}}})
	continueModel := fmt.Sprintf("models:\n  - name: Loom\n    provider: openai\n    model: %s\n    apiBase: %s\n    apiKey: %s\n", snippetQuote(model), snippetQuote(base), snippetQuote(key))
	claude := fmt.Sprintf("export ANTHROPIC_BASE_URL=%s\nexport ANTHROPIC_AUTH_TOKEN=%s\nexport ANTHROPIC_MODEL=%s\nexport OPENAI_BASE_URL=%s\n", snippetShell(strings.TrimSuffix(base, "/v1")), snippetShell(key), snippetShell(model), snippetShell(base))
	generic := fmt.Sprintf("export OPENAI_BASE_URL=%s\nexport OPENAI_API_KEY=%s\nexport LOOM_MODEL=%s\n", snippetShell(base), snippetShell(key), snippetShell(model))
	return map[string]harnessSnippet{
		"opencode": {File: "opencode.json", Content: jsonConfig},
		"codex":    {File: "~/.codex/config.toml", Content: codex, Note: "responses"},
		"claude":   {Content: claude, Note: "messages"},
		"pi":       {File: "~/.pi/agent/models.json", Content: pi},
		"continue": {File: "config.yaml", Content: continueModel},
		"openai":   {Content: generic},
	}
}

func requestHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		return host
	}
	return strings.Trim(r.Host, "[]")
}

func handleHarnessSnippets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	n := currentEngineNode()
	required := legacyEngineKey() != "" || apiKeyRequired() || getBool(bkState, "engine_named_keys_required") || lanExposed() || isEngineWorker()
	engineKeysMu.Lock()
	keys, keyErr := loadEngineKeysLocked()
	engineKeysMu.Unlock()
	required = required || keyErr != nil || slices.ContainsFunc(keys, func(k engineKeyRecord) bool { return k.ID != "default" })

	local := fmt.Sprintf("http://127.0.0.1:%d/v1", LLMPort())
	lan := ""
	if n != nil || isEngineWorker() {
		port := webBound.port
		if port == 0 {
			port = 2510
			if _, p, err := net.SplitHostPort(r.Host); err == nil {
				if parsed, err := strconv.Atoi(p); err == nil {
					port = parsed
				}
			}
		}
		local = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
		if isEngineWorker() && !localLoopbackHost(webBound.host) || n != nil && lanExposed() && webBound.host != "" && !localLoopbackHost(webBound.host) {
			host := requestHost(r)
			if localLoopbackHost(host) || host == "" {
				host = localIP()
			}
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			lan = scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/v1"
		}
	} else if lanExposed() {
		lan = fmt.Sprintf("http://%s/v1", net.JoinHostPort(localIP(), strconv.Itoa(LLMPort())))
	}
	// "loom" selects the loaded GGUF regardless of its on-disk name. A
	// direct engine's model must be sent by its served ID.
	model := "loom"
	switch {
	case n != nil && n.Direct && n.Model != "":
		model = n.Model
	case n != nil && !n.Direct:
		ctx, cancel := context.WithTimeout(r.Context(), 750*time.Millisecond)
		defer cancel()
		var status struct {
			Model        string
			PresetName   string `json:"preset_name"`
			EngineDirect bool   `json:"engine_direct"`
		}
		if code, err := directGET(ctx, n.URL, "/api/status", n.WebKey, &status); err == nil && code == 200 {
			if status.EngineDirect && status.Model != "" {
				model = status.Model
			} else if status.PresetName != "" {
				model = status.PresetName
			}
		}
	case n == nil:
		if pid := strings.TrimSuffix(getStr(bkState, "active_preset"), ".env"); pid != "" {
			if active, ok := resolveOAIModel(pid); ok {
				model = active.ID
			}
		} else if configured := ReadConfig()["MODEL"]; configured != "" {
			if active, ok := resolveOAIModel(configured); ok {
				model = active.ID
			}
		}
	}
	modelLocal := makeHarnessSnippets(local, model, required)
	if lan != "" {
		for id, other := range makeHarnessSnippets(lan, model, required) {
			s := modelLocal[id]
			s.LAN = other.Content
			modelLocal[id] = s
		}
	}
	placeholder := ""
	if required {
		placeholder = loomKeyPlaceholder
	}
	sendJSON(w, 200, map[string]any{"ok": true, "url_local": local, "url_lan": lan, "model": model, "key_required": required, "key_placeholder": placeholder, "snippets": modelLocal})
}

// The web process owns the active linked-engine selection, including engines
// on ephemeral ports. Expose the same inference wire protocols on its /v1
// listener, with Loom's inference credentials and model/key limits.
func serveLinkedEngineInference(w http.ResponseWriter, r *http.Request) {
	n := currentEngineNode()
	if n == nil {
		http.NotFound(w, r)
		return
	}
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	if r.Header.Get("Origin") != "" || (!localLoopbackHost(peer) || !localLoopbackHost(requestHost(r))) && !lanExposed() {
		oaiError(w, 403, "invalid_request_error", "Inference is unavailable on this origin", "", "origin_not_allowed")
		return
	}
	key, ok := oaiKeyOK(r)
	if !ok {
		oaiError(w, 401, "invalid_request_error", "Invalid API key", "", "invalid_api_key")
		return
	}
	proxyLinkedInference(w, r, n, key)
}

func proxyLinkedInference(w http.ResponseWriter, r *http.Request, n *engineNode, key engineAPIIdentity) {
	p := r.URL.Path
	if path.Clean(p) != strings.TrimSuffix(p, "/") {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && (p == "/v1/models" || p == "/v1/models/" || strings.HasPrefix(p, "/v1/models/")):
	case r.Method == http.MethodPost && (p == "/v1/chat/completions" || p == "/v1/completions" || p == "/v1/responses" ||
		p == "/v1/embeddings" || p == "/v1/messages" || p == "/v1/messages/count_tokens"):
	default:
		http.NotFound(w, r)
		return
	}
	base, err := url.Parse(n.V1)
	if err != nil || base.Host == "" || base.Scheme != "http" && base.Scheme != "https" {
		oaiError(w, 502, "api_error", "Invalid engine address", "", "server_error")
		return
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(base)
			p.Out.Header.Del("Cookie")
			p.Out.Header.Del("Authorization")
			p.Out.Header.Del("X-API-Key")
			if n.APIKey != "" {
				p.Out.Header.Set("Authorization", "Bearer "+n.APIKey)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			oaiError(w, 502, "api_error", "Linked engine unavailable", "", "server_error")
		},
	}
	if r.Method == http.MethodGet {
		proxy.ServeHTTP(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		oaiError(w, 413, "invalid_request_error", "Inference request too large", "", "invalid_request")
		return
	}
	want := peekOAIModel(body)
	resolved := want
	if n.Direct && (want == "loom" || want == "") {
		resolved = n.Model
		if resolved != "" {
			body = rewriteOAIModel(body, resolved)
		}
	}
	// Native GGUF aliases must not be resolved when the inference server
	// advertises its own model IDs.
	if len(key.Allowed) != 0 && !slices.Contains(key.Allowed, resolved) {
		oaiError(w, 403, "invalid_request_error", "Model not allowed for this key", "model", "model_not_allowed")
		return
	}
	finish, err := beginEngineKey(key)
	if err != nil {
		var limit *engineKeyLimit
		if errors.As(err, &limit) {
			w.Header().Set("Retry-After", strconv.Itoa(limit.retry))
			oaiError(w, 429, "rate_limit_error", limit.code, "", limit.code)
		} else {
			oaiError(w, 503, "api_error", "Key unavailable", "", "key_unavailable")
		}
		return
	}
	var tap *engineUsageTap
	defer func() {
		if tap == nil {
			finish(0, 0, "engine_unreachable")
			return
		}
		finish(tap.prompt, tap.completion, tap.lastError)
	}()
	proxy.ModifyResponse = func(resp *http.Response) error {
		tap = &engineUsageTap{body: resp.Body, stream: strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")}
		if resp.StatusCode >= 400 {
			tap.lastError = fmt.Sprintf("engine_http_%d", resp.StatusCode)
		}
		resp.Body = tap
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	proxy.ServeHTTP(w, r)
}
