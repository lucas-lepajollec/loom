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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Remote engine: the models run on another machine (a GPU box) where a Loom
// owns the engine, while this Loom (a server or VM without GPU) is the one
// people use. This Loom then forwards every engine request (status, models,
// presets, loads, downloads, VRAM…) to that Loom's control API, and sends
// completions to its /v1 API. Discussions, projects, harnesses and providers
// stay here. The link holds the remote control key and /v1 key; they are
// stored with Loom's other local secrets and never returned to the browser.

const engineNodeState = "engine_node"

type engineNode struct {
	URL      string `json:"url"`            // control API of the remote Loom, e.g. http://tour:2510
	WebKey   string `json:"web_key"`        // its control key
	V1       string `json:"v1"`             // its /v1 base, e.g. http://tour:8080
	APIKey   string `json:"api_key"`        // its /v1 key
	Hostname string `json:"hostname"`       // for display
	Role     string `json:"role,omitempty"` // engine-node or legacy full Loom
	Version  string `json:"version"`        // remote Loom version
	LinkedAt int64  `json:"linked_at"`      // unix ms
	// Direct link (engine_direct.go): an inference server used by address,
	// without Loom on its machine.
	Direct bool   `json:"direct,omitempty"`
	Kind   string `json:"kind,omitempty"`  // llama.cpp, vllm, openai
	Model  string `json:"model,omitempty"` // served model used by discussions
	Ctx    int    `json:"ctx,omitempty"`
	Router bool   `json:"router,omitempty"`
}

var (
	engineNodeMu    sync.Mutex
	engineNodeCache *engineNode
	engineNodeRead  bool
)

// currentEngineNode returns the linked remote engine, or nil when the engine
// runs on this machine.
func currentEngineNode() *engineNode {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	if !engineNodeRead {
		var n engineNode
		if getStoreJSON(bkState, engineNodeState, &n) && n.URL != "" {
			engineNodeCache = &n
		}
		engineNodeRead = true
	}
	return engineNodeCache
}

func setEngineNode(n *engineNode) error {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	var err error
	if n == nil {
		err = putBytes(bkState, engineNodeState, nil)
	} else {
		err = putStoreJSON(bkState, engineNodeState, n)
	}
	if err == nil {
		engineNodeCache, engineNodeRead = n, true
		engineModelsCache.Lock()
		engineModelsCache.body, engineModelsCache.at = nil, time.Time{}
		engineModelsCache.Unlock()
	}
	return err
}

// engineBase is the origin of the engine's OpenAI-compatible API (no /v1).
func engineBase() string {
	if n := currentEngineNode(); n != nil {
		return strings.TrimRight(n.V1, "/")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", LLMPort())
}

// engineAPIKey is the key the engine's /v1 API expects.
func engineAPIKey() string {
	if n := currentEngineNode(); n != nil {
		return n.APIKey
	}
	return readAPIKey()
}

// engineModelsJSON is the engine's /v1/models: computed here, or read from
// the linked remote engine (cached 30 s).
var engineModelsCache struct {
	sync.Mutex
	at   time.Time
	body []byte
}

func engineModelsJSON() []byte {
	n := currentEngineNode()
	if n == nil {
		return oaiModelsJSON()
	}
	engineModelsCache.Lock()
	defer engineModelsCache.Unlock()
	if time.Since(engineModelsCache.at) < 30*time.Second && engineModelsCache.body != nil {
		return engineModelsCache.body
	}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(n.V1, "/")+"/v1/models", nil)
	if n.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.APIKey)
	}
	resp, err := nodeClient.Do(req)
	if err != nil {
		return engineModelsCache.body
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err == nil && resp.StatusCode == 200 {
		engineModelsCache.at, engineModelsCache.body = time.Now(), b
	}
	return engineModelsCache.body
}

// engineCurrentModel names the model the engine serves, for attribution.
func engineCurrentModel() string {
	n := currentEngineNode()
	if n == nil {
		return ReadConfig()["MODEL"]
	}
	if n.Direct {
		return n.Model
	}
	req, _ := http.NewRequest(http.MethodGet, n.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var st struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st)
	return st.Model
}

// engineContextSize is the context (tokens) the engine gives a request, as
// /api/status reports it; 0 when unknown.
func engineContextSize() int {
	n := currentEngineNode()
	if n == nil {
		return effectiveCtx(ReadConfig())
	}
	if n.Direct {
		return n.Ctx
	}
	req, _ := http.NewRequest(http.MethodGet, n.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var st struct {
		Ctx int `json:"ctx"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st)
	return st.Ctx
}

// --- the remote side: what a Loom tells another Loom that links to it ---

// GET (control key required): what a linking Loom needs. Only served when this
// Loom is protected by a control key, since it hands out the /v1 key.
func handleNodeInfo(w http.ResponseWriter, r *http.Request) {
	if !webKeyConfigured() {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "this Loom has no control key: enable network access from Settings › Network access"})
		return
	}
	host, _ := os.Hostname()
	st := networkStatus()
	sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "version": Version, "llm_port": LLMPort(),
		"v1_exposed": st.Exposed, "api_key": readAPIKey(), "engine": currentEngineNode() == nil})
}

// --- the local side: linking and forwarding ---

// Control credentials belong to the explicitly linked endpoint. Even redirects
// to a sibling host must not receive them or silently change the engine target.
var nodeClient = &http.Client{Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func cleanNodeURL(raw string) (string, *url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", nil, errors.New("invalid address (e.g. http://192.168.1.20:2510)")
	}
	if u.Scheme == "http" && !localNetworkHost(u.Hostname()) {
		return "", nil, errors.New("http is only accepted on the local network; use https elsewhere")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), u, nil
}

// linkEngineNode contacts the remote Loom, checks it can serve as engine and
// stores the link.
func linkEngineNode(ctx context.Context, rawURL, webKey string) (*engineNode, error) {
	base, u, err := cleanNodeURL(rawURL)
	if err != nil {
		return nil, err
	}
	webKey = strings.TrimSpace(webKey)
	if webKey == "" {
		return nil, errors.New("remote Loom control key required")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/node/info", nil)
	req.Header.Set("Authorization", "Bearer "+webKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, errors.New("Remote Loom unreachable: check the address and that network access is enabled")
	}
	defer resp.Body.Close()
	var info struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		Hostname   string `json:"hostname"`
		Version    string `json:"version"`
		LLMPort    int    `json:"llm_port"`
		SameOrigin bool   `json:"v1_same_origin"`
		Role       string `json:"role"`
		Exposed    bool   `json:"v1_exposed"`
		APIKey     string `json:"api_key"`
		Engine     bool   `json:"engine"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info)
	switch {
	case resp.StatusCode == 401:
		return nil, errors.New("control key rejected by remote Loom")
	case resp.StatusCode == 404:
		return nil, errors.New("this remote Loom is too old: update it")
	case !info.OK:
		if info.Error == "" {
			info.Error = "unexpected response from remote Loom (HTTP " + resp.Status + ")"
		}
		return nil, errors.New(info.Error)
	case !info.Engine:
		return nil, errors.New("this Loom itself uses a remote engine: link directly to the machine hosting the engine")
	case !info.Exposed:
		return nil, errors.New("remote Loom's /v1 API is not listening on the network: enable “API /v1 on the network” in its Settings › Network access")
	case !info.SameOrigin && info.LLMPort <= 0:
		return nil, errors.New("unknown remote Loom /v1 port")
	}
	v1 := u.Scheme + "://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(info.LLMPort))
	if info.SameOrigin {
		v1 = base
	}
	n := &engineNode{URL: base, WebKey: webKey, V1: v1, APIKey: info.APIKey, Hostname: info.Hostname, Version: info.Version, Role: info.Role, LinkedAt: time.Now().UnixMilli()}
	// The /v1 API must answer with that key before the link is kept.
	hreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, v1+"/v1/models", nil)
	if n.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+n.APIKey)
	}
	hresp, err := nodeClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("remote Loom's /v1 API (%s) is unreachable: firewall?", v1)
	}
	hresp.Body.Close()
	if hresp.StatusCode != 200 {
		return nil, fmt.Errorf("remote Loom's /v1 API refused the connection (HTTP %d)", hresp.StatusCode)
	}
	if n.Role == "engine-node" {
		for _, m := range loadRemoteMachines() {
			if u.Hostname() == m.Host {
				if err := putStoreJSON(bkState, machineNodePrefix+m.ID, n); err != nil {
					return nil, errors.New("machine maintenance access could not be saved")
				}
			}
		}
	}
	if err := setEngineNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// engineRoutes are the control API paths owned by the engine's machine.
var engineRoutes = map[string]bool{
	"/api/status": true, "/api/service/log": true, "/api/vram": true, "/api/ram": true, "/api/config": true,
	"/api/reasoning": true, "/api/catalog": true, "/api/paths": true, "/api/server": true,
	"/api/models": true, "/api/models/delete": true, "/api/models/dirs": true, "/api/models/download": true,
	"/api/models/download/probe": true, "/api/models/download/status": true, "/api/models/download/cancel": true,
	"/api/hub/search": true, "/api/hub/model": true, "/api/hub/avatar": true,
	"/api/backends": true, "/api/backends/custom": true, "/api/backends/devices": true,
	"/api/llamacpp": true, "/api/llamacpp/check": true, "/api/llamacpp/install": true, "/api/llamacpp/install-custom": true,
	"/api/llamacpp/uninstall-custom": true, "/api/llamacpp/update": true, "/api/llamacpp/job": true,
	"/api/llamacpp/job/dismiss": true, "/api/llamacpp/prebuilt": true, "/api/llamacpp/prebuilt/check": true, "/api/llamacpp/use": true,
	"/api/presets": true, "/api/presets/order": true, "/api/preset": true, "/api/preset/save": true, "/api/preset/delete": true,
	"/api/switch": true, "/api/load-model": true, "/api/unload": true, "/api/apply": true,
	"/api/naked/remember": true, "/api/naked/defaults": true, "/api/llama-flags": true, "/api/engine/params": true,
	"/api/model-caps": true, "/api/estimate": true, "/api/start": true, "/api/stop": true, "/api/restart": true,
	"/api/apikey": true, "/api/network": true, "/api/engine/auto-update": true,
	"/api/engines/vllm": true, "/api/engines/vllm/params": true, "/api/engines/vllm/auto-update": true,
	"/api/engines/vllm/models": true, "/api/engines/vllm/models/delete": true, "/api/engines/vllm/hub/search": true,
	"/api/engines/vllm/download": true, "/api/engines/vllm/download/cancel": true,
	"/api/bench": true, "/api/bench/last": true, "/api/bench/tests": true, "/api/bench/tests/delete": true,
	"/api/bench/queue": true, "/api/bench/queue/cancel": true, "/api/bench/runs": true,
}

// nodeAware sends an engine route to the linked remote Loom, with its control
// key in place of this browser's one. Without a link, the local handler runs.
func nodeAware(path string, local http.HandlerFunc) http.HandlerFunc {
	if !engineRoutes[path] {
		return local
	}
	return func(w http.ResponseWriter, r *http.Request) {
		n := currentEngineNode()
		if n == nil {
			local(w, r)
			return
		}
		if n.Direct {
			serveDirectEngineRoute(w, r, n, local)
			return
		}

		proxyEngineNode(w, r, n)
	}
}

// GET: link state. POST {url, key}: link. POST {unlink:true}: back to this machine.
func handleEngineNode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		n := currentEngineNode()
		if n == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "remote": false})
			return
		}
		if n.Direct {
			sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "direct": true, "kind": n.Kind, "url": n.V1, "hostname": directHostname(n),
				"model": n.Model, "ctx": n.Ctx, "reachable": directEngineHealthy(n)})
			return
		}
		// Reachability, without secrets in the response.
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, n.URL+"/api/ping", nil)
		req.Header.Set("Authorization", "Bearer "+n.WebKey)
		reachable := false
		if resp, err := nodeClient.Do(req); err == nil {
			resp.Body.Close()
			reachable = resp.StatusCode == 200
		}
		sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "url": n.URL, "v1": n.V1, "hostname": n.Hostname, "version": n.Version, "role": n.Role, "reachable": reachable})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		URL    string `json:"url"`
		Key    string `json:"key"`
		Unlink bool   `json:"unlink"`
		Direct bool   `json:"direct"` // link an inference server by address
		Probe  bool   `json:"probe"`  // only identify it
		Model  string `json:"model"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Direct {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		n, err := linkDirectEngine(ctx, req.URL, req.Key, req.Model)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "direct": true, "kind": n.Kind, "hostname": n.Hostname, "model": n.Model})
		return
	}
	if req.Probe {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		base, _, err := cleanNodeURL(strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(req.URL), "/"), "/v1"))
		if err == nil {
			var e directEngine
			if e, err = probeDirectEngine(ctx, base, strings.TrimSpace(req.Key)); err == nil {
				sendJSON(w, 200, map[string]any{"ok": true, "kind": e.Kind, "models": e.Models, "ctx": e.Ctx, "router": e.Router})
				return
			}
		}
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.Unlink {
		if err := setEngineNode(nil); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "remote": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	n, err := linkEngineNode(ctx, req.URL, req.Key)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "url": n.URL, "hostname": n.Hostname, "version": n.Version})
}

func proxyEngineNode(w http.ResponseWriter, r *http.Request, n *engineNode) {
	target, err := url.Parse(n.URL)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "invalid engine link"})
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	base := proxy.Director
	proxy.Director = func(req *http.Request) {
		base(req)
		req.Host = target.Host
		req.Header.Set("Authorization", "Bearer "+n.WebKey)
		req.Header.Del("Cookie")
		req.Header.Del("Origin")
		req.Header.Del("Referer")
	}
	if n.Role == "engine-node" && r.URL.Path == "/api/server" {
		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode != http.StatusOK {
				return nil
			}
			defer resp.Body.Close()
			var status map[string]any
			if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
				return err
			}
			// A TLS reverse proxy or SSH tunnel can differ from the worker listener.
			// Show the negotiated client endpoint, never a private backend address.
			endpoint := strings.TrimRight(n.V1, "/") + "/v1"
			status["url"], status["url_local"] = endpoint, endpoint
			body, err := json.Marshal(status)
			if err != nil {
				return err
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
			return nil
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "remote engine unreachable (" + n.Hostname + ")"})
	}
	proxy.ServeHTTP(w, r)
}

func handleEngineNodeUpdate(w http.ResponseWriter, r *http.Request) {
	n := currentEngineNode()
	if n == nil || n.Direct || n.Role != "engine-node" {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "Connect an engine node first"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/engine/node/update")
	switch path {
	case "":
		path = "/api/update"
	case "/apply":
		path = "/api/update/apply"
	case "/ping":
		path = "/api/ping"
	default:
		http.NotFound(w, r)
		return
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path, cloned.URL.RawPath = path, ""
	proxyEngineNode(w, cloned, n)
}

// A linked engine owns its sampling configuration. Never apply the VM's stale
// local preset to requests headed for the GPU machine. Fail visibly if that
// configuration cannot be read; do not silently substitute local defaults.
func engineExecutionConfig(ctx context.Context) (map[string]string, error) {
	n := currentEngineNode()
	if n == nil || n.Direct {
		return ReadConfig(), nil
	}
	path := "/api/config"
	if n.Role == "engine-node" {
		path = "/api/engine/execution-config"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.URL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, errors.New("remote engine configuration unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote engine configuration unavailable (HTTP %d)", resp.StatusCode)
	}
	var cfg map[string]string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cfg); err != nil {
		return nil, errors.New("invalid remote engine configuration")
	}
	return cfg, nil
}
