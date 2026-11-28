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

const (
	engineNodeState     = "engine_node"
	engineNodeWebKeyAAD = "loom/engine-node/web-key"
	engineNodeAPIKeyAAD = "loom/engine-node/api-key"
)

func sealEngineNode(n *engineNode) (*engineNode, error) {
	if n == nil {
		return nil, nil
	}
	cp := *n
	var err error
	if cp.WebKey != "" {
		cp.WebKey, err = sealInlineSecret(engineNodeWebKeyAAD, cp.WebKey)
		if err != nil {
			return nil, err
		}
	}
	if cp.APIKey != "" {
		cp.APIKey, err = sealInlineSecret(engineNodeAPIKeyAAD, cp.APIKey)
		if err != nil {
			return nil, err
		}
	}
	return &cp, nil
}

func unsealEngineNode(n *engineNode) (bool, error) {
	if n == nil {
		return false, nil
	}
	var migrated bool
	if n.WebKey != "" {
		plain, wasPlain, err := openInlineSecret(engineNodeWebKeyAAD, n.WebKey)
		if err != nil {
			return false, err
		}
		n.WebKey = plain
		migrated = migrated || wasPlain
	}
	if n.APIKey != "" {
		plain, wasPlain, err := openInlineSecret(engineNodeAPIKeyAAD, n.APIKey)
		if err != nil {
			return false, err
		}
		n.APIKey = plain
		migrated = migrated || wasPlain
	}
	return migrated, nil
}

type engineNode struct {
	NodeID    string   `json:"node_id,omitempty"`
	Modules   []string `json:"modules,omitempty"`
	Handshake int      `json:"handshake,omitempty"`
	URL       string   `json:"url"`            // control API of the remote Loom, e.g. http://tour:2510
	WebKey    string   `json:"web_key"`        // its control key
	V1        string   `json:"v1"`             // its /v1 base, e.g. http://tour:8080
	APIKey    string   `json:"api_key"`        // its /v1 key
	Hostname  string   `json:"hostname"`       // for display
	Role      string   `json:"role,omitempty"` // engine-node or legacy full Loom
	Version   string   `json:"version"`        // remote Loom version
	LinkedAt  int64    `json:"linked_at"`      // unix ms
	// Direct link (engine_direct.go): an inference server used by address,
	// without Loom on its machine.
	Managed string `json:"-"` // transient Loom-owned child process
	Direct  bool   `json:"direct,omitempty"`
	Kind    string `json:"kind,omitempty"`  // llama.cpp, vllm, openai
	Model   string `json:"model,omitempty"` // served model used by discussions
	Ctx     int    `json:"ctx,omitempty"`
	Router  bool   `json:"router,omitempty"`
}

var (
	engineNodeMu    sync.Mutex
	engineNodeCache *engineNode
	engineNodeRead  bool
	engineNodePath  string
)

// currentEngineNode returns the linked remote engine, or nil when the engine
// runs on this machine.
func currentEngineNode() *engineNode {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	return currentEngineNodeLocked()
}

func currentEngineNodeLocked() *engineNode {
	if path := dbPath(); engineNodePath != path {
		engineNodePath = path
		engineNodeCache, engineNodeRead = nil, false
		engineModelsCache.Lock()
		engineModelsCache.body, engineModelsCache.at = nil, time.Time{}
		engineModelsCache.Unlock()
	}
	if !engineNodeRead {
		var n engineNode
		if getStoreJSON(bkState, engineNodeState, &n) && (n.URL != "" || n.Direct && n.V1 != "") {
			if migrated, err := unsealEngineNode(&n); err == nil {
				engineNodeCache = &n
				if migrated {
					if sealed, serr := sealEngineNode(&n); serr == nil {
						_ = putStoreJSON(bkState, engineNodeState, sealed)
					}
				}
			}
		}
		engineNodeRead = true
	}
	return engineNodeCache
}

func setEngineNode(n *engineNode) error {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	return setEngineNodeLocked(n)
}

func setEngineNodeLocked(n *engineNode) error {
	var profile *storedEngineConnection
	if n != nil && n.Direct && n.Managed == "" {
		p, err := connectionFromNode(n, "")
		if err != nil {
			return err
		}
		profile = &p
	}
	return writeEngineSelectionLocked(n, true, profile, "")
}

func cacheEngineNodeLocked(n *engineNode) {
	engineNodePath, engineNodeRead = dbPath(), true
	engineNodeCache = nil
	if n != nil {
		cp := *n
		engineNodeCache = &cp
	}
	engineModelsCache.Lock()
	engineModelsCache.body, engineModelsCache.at = nil, time.Time{}
	engineModelsCache.Unlock()
}

func localEngineBase() string { return ownedEngineBase(LLMPort()) }

// Capture the address, key and model together so a saved-engine switch
// cannot send one server's credential to another server.
func engineRequestTarget() (base, key, model string) {
	n := currentEngineNode()
	if n == nil {
		return localEngineBase(), readAPIKey(), "loom"
	}
	model = "loom"
	if n.Direct && n.Model != "" {
		model = n.Model
	}
	return strings.TrimRight(n.V1, "/"), n.APIKey, model
}

// engineBase is the origin of the engine's OpenAI-compatible API (no /v1).
func engineBase() string {
	if n := currentEngineNode(); n != nil {
		return strings.TrimRight(n.V1, "/")
	}
	return localEngineBase()
}

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
	resp, err := nodeControlClient(n).Do(req)
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
	resp, err := nodeControlClient(n).Do(req)
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
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	hash, err := webKeyHashErr()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	if hash == "" || !checkBearer(r, hash) || r.Header.Get("Origin") != "" {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "machine access requires the Loom control key"})
		return
	}
	host, _ := os.Hostname()
	st := networkStatus()
	sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "version": Version, "llm_port": LLMPort(),
		"handshake": nodeHandshake, "modules": []string{"engine"}, "v1_exposed": st.Exposed, "api_key": readAPIKey(), "engine": currentEngineNode() == nil})
}

// --- the local side: linking and forwarding ---

// Control credentials belong to the explicitly linked endpoint. Even redirects
// to a sibling host must not receive them or silently change the engine target.
// Transport stays nil so http.DefaultTransport (tuned at server startup and
// replaceable in tests) is used.
var nodeClient = &http.Client{Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func tuneDefaultHTTPTransport() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		if tr.MaxIdleConns < 64 {
			tr.MaxIdleConns = 64
		}
		if tr.MaxIdleConnsPerHost < 16 {
			tr.MaxIdleConnsPerHost = 16
		}
	}
}

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
		ID         string   `json:"id"`
		Modules    []string `json:"modules"`
		Handshake  int      `json:"handshake"`
		OK         bool     `json:"ok"`
		Error      string   `json:"error"`
		Hostname   string   `json:"hostname"`
		Version    string   `json:"version"`
		LLMPort    int      `json:"llm_port"`
		SameOrigin bool     `json:"v1_same_origin"`
		Role       string   `json:"role"`
		Exposed    bool     `json:"v1_exposed"`
		APIKey     string   `json:"api_key"`
		Engine     bool     `json:"engine"`
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
	case checkNodeHandshake(info.Handshake) != nil:
		return nil, checkNodeHandshake(info.Handshake)
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
	n := &engineNode{NodeID: info.ID, Modules: info.Modules, Handshake: info.Handshake, URL: base, WebKey: webKey, V1: v1, APIKey: info.APIKey, Hostname: info.Hostname, Version: info.Version, Role: info.Role, LinkedAt: time.Now().UnixMilli()}
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
				if _, err := savePairedMachine(m, n); err != nil {
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
	"/api/engine/service": true, "/api/engine/openai": true, "/api/engine/keys": true, "/api/engine/keys/update": true, "/api/engine/keys/delete": true, "/api/engine/keys/rotate": true,
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
	"/api/engines/harness-snippets": true, "/api/engines/catalog": true, "/api/engines/connections": true,
	"/api/engines/ollama/pull": true, "/api/engines/ollama/unload": true,
	"/api/engines/sglang": true, "/api/engines/sglang/params": true, "/api/engines/mlx": true,
	"/api/engines/vllm": true, "/api/engines/vllm/params": true, "/api/engines/vllm/auto-update": true,
	"/api/engines/vllm/models": true, "/api/engines/vllm/models/delete": true, "/api/engines/vllm/hub/search": true,
	"/api/engines/vllm/download": true, "/api/engines/vllm/download/cancel": true,
	"/api/bench": true, "/api/bench/last": true,
}

// nodeAware sends an engine route to the linked remote Loom, with its control
// key in place of this browser's one. Without a link, the local handler runs.
func nodeAware(path string, local http.HandlerFunc) http.HandlerFunc {
	if !engineRoutes[path] || path == "/api/engines/harness-snippets" {
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

		if allowControlAction(w, r, path, n) {
			proxyEngineNode(w, r, n)
		}
	}
}

// GET: link state. POST {url, key}: link. POST {unlink:true}: back to this machine.
func handleEngineNode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		n := currentEngineNode()
		if n == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "remote": false, "degraded": degradedCapabilities.Snapshot("engine:local")})
			return
		}
		if n.Direct {
			sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "direct": true, "kind": n.Kind, "url": n.V1, "hostname": directHostname(n), "degraded": degradedCapabilities.Snapshot(currentEngineCapabilityOwner()),
				"model": n.Model, "ctx": n.Ctx, "managed": n.Managed, "reachable": directEngineHealthy(n)})
			return
		}
		// Reachability, without secrets in the response.
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, n.URL+"/api/ping", nil)
		req.Header.Set("Authorization", "Bearer "+n.WebKey)
		reachable := false
		if resp, err := nodeControlClient(n).Do(req); err == nil {
			resp.Body.Close()
			reachable = resp.StatusCode == 200
			if next := currentEngineNode(); next != nil && next.NodeID == n.NodeID && next.WebKey == n.WebKey {
				n = next
			}
		}
		sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "url": n.URL, "v1": n.V1, "hostname": n.Hostname, "version": n.Version, "role": n.Role, "reachable": reachable, "degraded": degradedCapabilities.Snapshot(currentEngineCapabilityOwner())})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		URL     string `json:"url"`
		Key     string `json:"key"`
		Unlink  bool   `json:"unlink"`
		Direct  bool   `json:"direct"` // link an inference server by address
		Probe   bool   `json:"probe"`  // only identify it
		Model   string `json:"model"`
		Machine string `json:"machine"` // a paired machine: reuse its saved credential
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Machine != "" {
		var saved *engineNode
		for _, m := range loadRemoteMachines() {
			if m.ID == req.Machine {
				saved = savedMachineNode(m)
			}
		}
		if saved == nil || saved.WebKey == "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "this machine is not paired; pair it with a code first"})
			return
		}
		req.URL, req.Key = saved.URL, saved.WebKey
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
		probeEngineAddress(w, ctx, req.URL, req.Key)
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
	// Fast dashboard observations must not occupy browser connections indefinitely
	// when an engine accepts the request but stops responding. Mutations and long
	// jobs retain their own lifecycle timeouts.
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/api/status", "/api/vram", "/api/ram", "/api/config", "/api/presets", "/api/models/download/status", "/api/llamacpp/job":
			ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
			defer cancel()
			r = r.Clone(ctx)
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.Transport = nodeRecoveryTransport{nodeClient.Transport, n}
	base := proxy.Director
	proxy.Director = func(req *http.Request) {
		base(req)
		req.Host = target.Host
		req.Header.Set("Authorization", "Bearer "+n.WebKey)
		req.Header.Del("Cookie")
		req.Header.Del("Origin")
		req.Header.Del("Referer")
		req.Header.Del("Accept-Encoding")
	}
	path := r.URL.Path
	showNewKey := false
	if path == "/api/apikey" && r.Method == http.MethodPost && r.Body != nil {
		// The control middleware has already bounded and validated the body.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		r.Body.Close()
		if err != nil {
			sendJSON(w, 400, map[string]any{"error": "invalid key action"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var action struct{ Action string }
		showNewKey = json.Unmarshal(body, &action) == nil && action.Action == "generate"
	}
	if n.Role == "engine-node" && path == "/api/server" || path == "/api/config" || path == "/api/apikey" || path == "/api/preset" || path == "/api/naked/remember" {
		proxy.ModifyResponse = func(resp *http.Response) error {
			if r.Method == http.MethodHead || resp.StatusCode == http.StatusNoContent {
				return nil
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body := []byte(`{"ok":false,"error":"upstream settings unavailable"}`)
				resp.Body = io.NopCloser(bytes.NewReader(body))
				resp.ContentLength = int64(len(body))
				resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
				resp.Header.Set("Content-Type", "application/json")
				return nil
			}
			var status map[string]any
			if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
				return err
			}
			if status == nil {
				return errors.New("invalid upstream settings")
			}
			switch path {
			case "/api/server":
				endpoint := strings.TrimRight(n.V1, "/") + "/v1"
				status["url"], status["url_local"] = endpoint, endpoint
			case "/api/config":
				delete(status, "API_KEY")
				delete(status, "CRAWL4AI_KEY")
			case "/api/apikey":
				if key, ok := status["key"].(string); ok {
					status["masked"] = maskAPIKey(key)
				}
				if !showNewKey {
					delete(status, "key")
				}
			default:
				if content, ok := status["content"].(string); ok {
					status["content"] = presetWithoutCredentials(content)
				}
			}
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
	if proxy.ModifyResponse == nil && strings.HasPrefix(r.URL.Path, "/api/engine/") {
		// An older remote Loom lacks newer engine controls: report it plainly.
		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode != http.StatusNotFound || strings.Contains(resp.Header.Get("Content-Type"), "json") {
				return nil
			}
			resp.Body.Close()
			body, _ := json.Marshal(map[string]any{"ok": false, "error_code": "engine_outdated", "machine": n.Hostname, "error": "the engine on " + n.Hostname + " runs an older Loom; update it from Machines"})
			resp.StatusCode, resp.Status = http.StatusConflict, "409 Conflict"
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
			resp.Header.Set("Content-Type", "application/json")
			return nil
		}
	}
	translate := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != http.StatusNotModified {
			return errors.New("engine control request redirected")
		}
		headers := make(http.Header)
		allowed := []string{"Content-Type", "Content-Length", "Content-Disposition", "Content-Encoding", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag", "Allow", "Retry-After", "Vary", "X-Accel-Buffering"}
		if resp.StatusCode == http.StatusSwitchingProtocols {
			// The voice relay uses this reverse proxy for its WebSocket handshake.
			allowed = append(allowed, "Connection", "Upgrade", "Sec-WebSocket-Accept", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions")
		}
		for _, name := range allowed {
			if values := resp.Header.Values(name); len(values) != 0 {
				headers[http.CanonicalHeaderKey(name)] = values
			}
		}
		resp.Header = headers
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("X-Content-Type-Options", "nosniff")
		resp.Header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		if translate != nil {
			return translate(resp)
		}
		return nil
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
	method := http.MethodGet
	if path == "/api/update/apply" {
		method = http.MethodPost
	}
	if !workspaceMethod(w, r, method) || !allowControlAction(w, r, r.URL.Path, n) {
		return
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path, cloned.URL.RawPath, cloned.URL.RawQuery = path, "", "channel="+updateChannel()
	proxyEngineNode(w, cloned, n)
}

func engineExecutionConfigFor(ctx context.Context, n *engineNode) (map[string]string, error) {
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
	resp, err := nodeControlClient(n).Do(req)
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

// refreshEngineNodeKey re-reads a linked Loom node's current inference key with
// the machine credential (which does not change when the user rotates or
// recreates the node's API key) and stores it when it changed. Direct engines
// and pairings without a machine credential cannot be refreshed.
func refreshEngineNodeKey(ctx context.Context, n *engineNode) (*engineNode, bool) {
	if n == nil || n.Direct || n.URL == "" || n.WebKey == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.URL+"/api/node/info", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var info struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		APIKey string `json:"api_key"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info) != nil || !info.OK {
		return nil, false
	}
	if info.APIKey == "" || info.APIKey == n.APIKey || (n.NodeID != "" && info.ID != n.NodeID) {
		return nil, false
	}
	fresh := *n
	fresh.APIKey = info.APIKey
	if setEngineNodeIfCurrent(n, &fresh) != nil {
		return nil, false
	}
	return &fresh, true
}
