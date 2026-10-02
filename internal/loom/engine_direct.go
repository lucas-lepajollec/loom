package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Direct engine link: an inference server running on another machine
// (llama.cpp's llama-server, vLLM, or any OpenAI-compatible server) used by
// address, with nothing of Loom installed there. Loom sends discussions to it
// and reads its state; managing the server (models on disk, flags, updates)
// stays on its machine, so those Loom pages say so instead of acting.

// directEngine describes what Loom learned when linking.
type directEngine struct {
	Kind   string   // "llama.cpp", "vllm" or "openai"
	Models []string // served model ids
	Ctx    int      // context window when the server reports it
	Router bool     // llama.cpp router: models load on request
}

func directGET(ctx context.Context, base, path, key string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := nodeClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == 200 {
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}
	return resp.StatusCode, nil
}

// probeDirectEngine identifies the server and its models.
func probeDirectEngine(ctx context.Context, base, key string) (directEngine, error) {
	var models struct {
		Data []struct {
			ID          string `json:"id"`
			OwnedBy     string `json:"owned_by"`
			MaxModelLen int    `json:"max_model_len"`
			Status      *struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	code, err := directGET(ctx, base, "/v1/models", key, &models)
	switch {
	case err != nil:
		return directEngine{}, errors.New("engine unreachable: check the address and that the server is listening on the network")
	case code == 401 || code == 403:
		return directEngine{}, errors.New("the engine rejected the key (HTTP " + fmt.Sprint(code) + ")")
	case code != 200:
		return directEngine{}, fmt.Errorf("this is not an OpenAI-compatible server (/v1/models: HTTP %d)", code)
	}
	e := directEngine{Kind: "openai"}
	for _, m := range models.Data {
		if m.ID == "" {
			continue
		}
		e.Models = append(e.Models, m.ID)
		if m.OwnedBy == "vllm" {
			e.Kind = "vllm"
			if m.MaxModelLen > 0 && e.Ctx == 0 {
				e.Ctx = m.MaxModelLen
			}
		}
		if m.Status != nil {
			e.Router = true
		}
	}
	if len(e.Models) == 0 {
		return e, errors.New("the engine is not serving any models")
	}
	if e.Kind == "openai" {
		var props struct {
			Ctx      int `json:"n_ctx"`
			Settings struct {
				Ctx int `json:"n_ctx"`
			} `json:"default_generation_settings"`
			Build string `json:"build_info"`
			Role  string `json:"role"`
		}
		if code, _ := directGET(ctx, base, "/props", key, &props); code == 200 {
			e.Kind = "llama.cpp"
			e.Ctx = props.Settings.Ctx
			if props.Ctx > 0 {
				e.Ctx = props.Ctx
			}
			e.Router = e.Router || props.Role == "router"
		}
	}
	return e, nil
}

// linkDirectEngine stores a direct link after probing the server.
func linkDirectEngine(ctx context.Context, rawURL, key, model string) (*engineNode, error) {
	base, u, err := cleanNodeURL(strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(rawURL), "/"), "/v1"))
	if err != nil {
		return nil, err
	}
	e, err := probeDirectEngine(ctx, base, strings.TrimSpace(key))
	if err != nil {
		return nil, err
	}
	if model == "" {
		model = e.Models[0]
	}
	found := false
	for _, m := range e.Models {
		found = found || m == model
	}
	if !found {
		return nil, errors.New("model not served by this engine")
	}
	n := &engineNode{Direct: true, Kind: e.Kind, V1: base, APIKey: strings.TrimSpace(key), Hostname: u.Hostname(), Model: model,
		Ctx: e.Ctx, Router: e.Router, LinkedAt: time.Now().UnixMilli()}
	if err := setEngineNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

func directEngineHealthy(n *engineNode) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := directGET(ctx, n.V1, "/health", n.APIKey, nil)
	if err == nil && code == 200 {
		return true
	}
	code, err = directGET(ctx, n.V1, "/v1/models", n.APIKey, nil)
	return err == nil && code == 200
}

// engineRequestModel names the model in requests to the engine: Loom's own
// engine answers "loom" with the loaded model, a directly linked server needs
// the id it serves.
func engineRequestModel() string {
	if n := currentEngineNode(); n != nil && n.Direct && n.Model != "" {
		return n.Model
	}
	return "loom"
}

// serveDirectEngineRoute answers Loom's engine routes for a directly linked
// server: state and models are read from it, management actions are refused
// with an explanation.
func serveDirectEngineRoute(w http.ResponseWriter, r *http.Request, n *engineNode, local http.HandlerFunc) {
	switch r.URL.Path {
	case "/api/status":
		healthy := directEngineHealthy(n)
		sendJSON(w, 200, map[string]any{"active": healthy, "health": healthy, "state": map[bool]string{true: "active", false: "inactive"}[healthy],
			"model": n.Model, "model_name": path.Base(n.Model), "ctx": n.Ctx, "ctx_native": n.Ctx, "ctx_effective": nil, "hostname": directHostname(n),
			"port": 0, "version": Version, "boot": procBoot, "preset_id": "", "preset_name": "", "load_error": "", "warn": "",
			"engine_direct": true, "engine_kind": n.Kind, "engine_url": n.V1})
	case "/api/models":
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		e, err := probeDirectEngine(ctx, n.V1, n.APIKey)
		list := []map[string]any{}
		if err == nil {
			for _, m := range e.Models {
				list = append(list, map[string]any{"name": path.Base(m), "value": m, "path": m, "dir": n.Hostname, "size": 0, "remote": true})
			}
		}
		sendJSON(w, 200, list)
	case "/api/load-model", "/api/switch":
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		e, err := probeDirectEngine(ctx, n.V1, n.APIKey)
		if err != nil {
			sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		ok := false
		for _, m := range e.Models {
			ok = ok || m == req.Model
		}
		if !ok {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "model not served by this engine"})
			return
		}
		n.Model = req.Model
		if err := setEngineNode(n); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "model": req.Model})
	default:
		// Everything else is about this machine (its llama.cpp, model files,
		// presets, GPU): handled here as usual.
		local(w, r)
	}
}

// directHostname names the engine's machine; a server on this machine is
// shown with this machine's name.
func directHostname(n *engineNode) string {
	if n.Hostname == "127.0.0.1" || n.Hostname == "localhost" || n.Hostname == "::1" {
		h, _ := os.Hostname()
		return h
	}
	return n.Hostname
}
