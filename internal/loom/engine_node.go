package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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
	URL      string `json:"url"`       // control API of the remote Loom, e.g. http://tour:8091
	WebKey   string `json:"web_key"`   // its control key
	V1       string `json:"v1"`        // its /v1 base, e.g. http://tour:8080
	APIKey   string `json:"api_key"`   // its /v1 key
	Hostname string `json:"hostname"`  // for display
	Version  string `json:"version"`   // remote Loom version
	LinkedAt int64  `json:"linked_at"` // unix ms
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

// --- the remote side: what a Loom tells another Loom that links to it ---

// GET (control key required): what a linking Loom needs. Only served when this
// Loom is protected by a control key, since it hands out the /v1 key.
func handleNodeInfo(w http.ResponseWriter, r *http.Request) {
	if !webKeyConfigured() {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "ce Loom n’a pas de clé de pilotage : ouvre-le au réseau depuis Réglages › Accès réseau"})
		return
	}
	host, _ := os.Hostname()
	st := networkStatus()
	sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "version": Version, "llm_port": LLMPort(),
		"v1_exposed": st.Exposed, "api_key": readAPIKey(), "engine": currentEngineNode() == nil})
}

// --- the local side: linking and forwarding ---

var nodeClient = &http.Client{Timeout: 15 * time.Second}

func cleanNodeURL(raw string) (string, *url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", nil, errors.New("adresse invalide (ex. http://192.168.1.20:8091)")
	}
	if u.Scheme == "http" && !localNetworkHost(u.Hostname()) {
		return "", nil, errors.New("http n’est accepté que sur le réseau local ; utilise https au-delà")
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
		return nil, errors.New("clé de pilotage du Loom distant requise")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/node/info", nil)
	req.Header.Set("Authorization", "Bearer "+webKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, errors.New("Loom distant injoignable : vérifie l’adresse et qu’il est ouvert au réseau")
	}
	defer resp.Body.Close()
	var info struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
		LLMPort  int    `json:"llm_port"`
		Exposed  bool   `json:"v1_exposed"`
		APIKey   string `json:"api_key"`
		Engine   bool   `json:"engine"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info)
	switch {
	case resp.StatusCode == 401:
		return nil, errors.New("clé de pilotage refusée par le Loom distant")
	case resp.StatusCode == 404:
		return nil, errors.New("ce Loom distant est trop ancien : mets-le à jour")
	case !info.OK:
		if info.Error == "" {
			info.Error = "réponse inattendue du Loom distant (HTTP " + resp.Status + ")"
		}
		return nil, errors.New(info.Error)
	case !info.Engine:
		return nil, errors.New("ce Loom utilise lui-même un moteur distant : lie directement la machine qui a le moteur")
	case !info.Exposed:
		return nil, errors.New("l’API /v1 du Loom distant n’écoute pas sur le réseau : active « API /v1 sur le réseau » dans ses Réglages › Accès réseau")
	case info.LLMPort <= 0:
		return nil, errors.New("port /v1 du Loom distant inconnu")
	}
	v1 := fmt.Sprintf("%s://%s:%d", u.Scheme, u.Hostname(), info.LLMPort)
	n := &engineNode{URL: base, WebKey: webKey, V1: v1, APIKey: info.APIKey, Hostname: info.Hostname, Version: info.Version, LinkedAt: time.Now().UnixMilli()}
	// The /v1 API must answer with that key before the link is kept.
	hreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, v1+"/v1/models", nil)
	if n.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+n.APIKey)
	}
	hresp, err := nodeClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("l’API /v1 du Loom distant (%s) est injoignable : pare-feu ?", v1)
	}
	hresp.Body.Close()
	if hresp.StatusCode != 200 {
		return nil, fmt.Errorf("l’API /v1 du Loom distant refuse la connexion (HTTP %d)", hresp.StatusCode)
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
		target, err := url.Parse(n.URL)
		if err != nil {
			sendJSON(w, 502, map[string]any{"ok": false, "error": "lien moteur invalide"})
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
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
			sendJSON(w, 502, map[string]any{"ok": false, "error": "moteur distant injoignable (" + n.Hostname + ")"})
		}
		proxy.ServeHTTP(w, r)
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
		// Reachability, without secrets in the response.
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, n.URL+"/api/ping", nil)
		req.Header.Set("Authorization", "Bearer "+n.WebKey)
		reachable := false
		if resp, err := nodeClient.Do(req); err == nil {
			resp.Body.Close()
			reachable = resp.StatusCode == 200
		}
		sendJSON(w, 200, map[string]any{"ok": true, "remote": true, "url": n.URL, "v1": n.V1, "hostname": n.Hostname, "version": n.Version, "reachable": reachable})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		URL    string `json:"url"`
		Key    string `json:"key"`
		Unlink bool   `json:"unlink"`
	}
	if !workspaceDecode(w, r, &req) {
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
