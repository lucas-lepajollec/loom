package loom

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// A direct engine serves inference without a Loom control plane.
type directEngine struct {
	Kind    string
	Models  []string
	Details map[string]directModel
	Ctx     int
	Router  bool
	Version string
}

type directModel struct {
	Size         int64  `json:"size"`
	Quantization string `json:"quantization,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	MaxContext   int    `json:"max_context_length,omitempty"`
	Loaded       *bool  `json:"loaded,omitempty"`
}

type directModelList struct {
	Data []struct {
		ID            string          `json:"id"`
		OwnedBy       string          `json:"owned_by"`
		MaxModelLen   int             `json:"max_model_len"`
		ModelType     string          `json:"model_type"`
		MLX           json.RawMessage `json:"mlx"`
		ContextLength int             `json:"context_length"`
		Meta          json.RawMessage `json:"meta"`
		Status        json.RawMessage `json:"status"`
	} `json:"data"`
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
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}
	return resp.StatusCode, err
}

// Probe known native APIs in parallel within one deadline. Each match requires
// a recognizable payload because some servers return HTTP 200 for unknown paths.
func probeDirectNative(ctx context.Context, base, key, kind string) directEngine {
	var result directEngine
	ok := func(endpoint string, out any) bool {
		code, err := directGET(ctx, base, endpoint, key, out)
		return code == http.StatusOK && err == nil
	}
	switch kind {
	case "ollama":
		var version struct {
			Version string `json:"version"`
		}
		var tags, running struct {
			Models []struct {
				Name    string `json:"name"`
				Model   string `json:"model"`
				Size    int64  `json:"size"`
				Context int    `json:"context_length"`
				Details struct {
					Family       string `json:"family"`
					Quantization string `json:"quantization_level"`
				} `json:"details"`
			} `json:"models"`
		}
		var wg sync.WaitGroup
		wg.Go(func() { ok("/api/version", &version) })
		wg.Go(func() { ok("/api/tags", &tags) })
		wg.Go(func() { ok("/api/ps", &running) })
		wg.Wait()
		if tags.Models == nil && version.Version == "" {
			break
		}
		result = directEngine{Kind: kind, Version: version.Version, Details: map[string]directModel{}}
		loaded := map[string]int{}
		for _, m := range running.Models {
			loaded[m.Name] = m.Context
			loaded[m.Model] = m.Context
		}
		for _, m := range tags.Models {
			id := m.Name
			if id == "" {
				id = m.Model
			}
			if id == "" {
				continue
			}
			ctx, active := loaded[id]
			entry := directModel{Size: m.Size, Quantization: m.Details.Quantization, Architecture: m.Details.Family, MaxContext: ctx}
			if running.Models != nil {
				entry.Loaded = &active
			}
			result.Models = append(result.Models, id)
			result.Details[id] = entry
		}
	case "lmstudio":
		var info struct {
			Data []struct {
				ID, State, Quantization, Arch, Type string
				Context                             int `json:"max_context_length"`
			} `json:"data"`
		}
		if ok("/api/v0/models", &info) && info.Data != nil {
			result = directEngine{Kind: kind, Details: map[string]directModel{}}
			for _, m := range info.Data {
				if m.ID == "" || m.Type == "embeddings" || m.Type == "embedding" {
					continue
				}
				item := directModel{Quantization: m.Quantization, Architecture: m.Arch, MaxContext: m.Context}
				if m.State != "" {
					loaded := m.State == "loaded"
					item.Loaded = &loaded
				}
				result.Models = append(result.Models, m.ID)
				result.Details[m.ID] = item
			}
		}
	case "sglang":
		var info struct {
			ModelPath string `json:"model_path"`
			Version   string `json:"version"`
			Context   int    `json:"context_length"`
		}
		if ok("/get_model_info", &info) && info.ModelPath != "" || ok("/model_info", &info) && info.ModelPath != "" {
			result = directEngine{Kind: kind, Version: info.Version, Ctx: info.Context}
		}
	case "tabbyapi":
		var info struct {
			Data   []json.RawMessage `json:"data"`
			Models []json.RawMessage `json:"models"`
		}
		if ok("/v1/model/list", &info) && (info.Data != nil || info.Models != nil) {
			result.Kind = kind
		}
	case "koboldcpp":
		var info struct{ Result, Version string }
		if ok("/api/extra/version", &info) && strings.EqualFold(info.Result, "KoboldCpp") {
			result.Kind, result.Version = kind, info.Version
		}
	case "lemonade":
		var info struct {
			Status, Version string
			ModelLoaded     json.RawMessage   `json:"model_loaded"`
			AllModelsLoaded []json.RawMessage `json:"all_models_loaded"`
			MaxContext      int               `json:"max_context_tokens"`
		}
		if ok("/api/v0/health", &info) && (info.ModelLoaded != nil || info.AllModelsLoaded != nil || info.MaxContext > 0) {
			result = directEngine{Kind: kind, Version: info.Version, Ctx: info.MaxContext}
		}
	case "strata", "basalt", "service-health":
		// Both runtimes provide llama.cpp-compatible /props. Their own
		// service identifier takes precedence over that compatibility API.
		var health struct {
			Service string `json:"service"`
			Context int    `json:"max_context"`
		}
		if ok("/health", &health) && (health.Service == "strata" || health.Service == "basalt") &&
			(kind == "service-health" || kind == health.Service) {
			result = directEngine{Kind: health.Service, Ctx: health.Context}
		}
	case "mistral.rs":
		var system struct {
			Devices []json.RawMessage `json:"devices"`
			Build   struct {
				Version string `json:"version"`
				CUDA    *bool  `json:"cuda"`
				Metal   *bool  `json:"metal"`
			} `json:"build"`
		}
		if ok("/v1/system/info", &system) && system.Devices != nil &&
			system.Build.Version != "" && system.Build.CUDA != nil && system.Build.Metal != nil {
			result = directEngine{Kind: kind, Version: system.Build.Version}
		}
	case "ktransformers":
		var schema struct {
			Info struct {
				Title string `json:"title"`
			} `json:"info"`
		}
		if ok("/openapi.json", &schema) && strings.EqualFold(schema.Info.Title, "ktransformers server") {
			result.Kind = kind
		}
	case "exo":
		var state struct {
			Topology  map[string]json.RawMessage `json:"topology"`
			Instances map[string]json.RawMessage `json:"instances"`
			Runners   map[string]json.RawMessage `json:"runners"`
		}
		if ok("/state", &state) && state.Topology != nil && state.Instances != nil && state.Runners != nil {
			result.Kind = kind
		}
	case "llama.cpp":
		var props struct {
			Ctx      int `json:"n_ctx"`
			Settings struct {
				Ctx int `json:"n_ctx"`
			} `json:"default_generation_settings"`
			Build string `json:"build_info"`
			Role  string `json:"role"`
			Slots int    `json:"total_slots"`
		}
		if ok("/props", &props) && (props.Ctx > 0 || props.Settings.Ctx > 0 || props.Build != "" || props.Role == "router" || props.Slots > 0) {
			result = directEngine{Kind: kind, Version: props.Build, Ctx: props.Ctx, Router: props.Role == "router"}
			if result.Ctx == 0 {
				result.Ctx = props.Settings.Ctx
			}
			if strings.Contains(strings.ToLower(props.Build), "ik_llama") || strings.Contains(strings.ToLower(props.Build), "ikawrakow") {
				result.Kind = "ik_llama.cpp"
			}
		}
	}
	return result
}

func probeDirectEngine(ctx context.Context, base, key string) (directEngine, error) {
	var models directModelList
	code, err := directGET(ctx, base, "/v1/models", key, &models)
	switch {
	case code == 401 || code == 403:
		return directEngine{}, fmt.Errorf("the engine rejected the key (HTTP %d)", code)
	case err != nil && code == 0:
		return directEngine{}, errors.New("engine unreachable: check the address and that the server is listening on the network")
	case code != 200 || err != nil:
		return directEngine{}, fmt.Errorf("this is not an OpenAI-compatible server (/v1/models: HTTP %d)", code)
	}
	e := directEngine{Kind: "openai", Models: []string{}, Details: map[string]directModel{}}
	mlxMetadata := false
	for _, m := range models.Data {
		if m.ID == "" || slices.Contains(e.Models, m.ID) {
			continue
		}
		e.Models = append(e.Models, m.ID)
		modelContext := cmp.Or(m.MaxModelLen, m.ContextLength)
		if modelContext == 0 && len(m.Meta) > 0 {
			var meta struct {
				Context int `json:"n_ctx"`
			}
			if json.Unmarshal(m.Meta, &meta) == nil {
				modelContext = meta.Context
			}
		}
		e.Details[m.ID] = directModel{MaxContext: modelContext}
		if len(e.Models) == 1 {
			e.Ctx = modelContext
		}
		// llama.cpp-style routers use a status object. mistral.rs uses a
		// string status on model cards, which does not imply that protocol.
		e.Router = e.Router || len(m.Status) > 0 && m.Status[0] == '{'
		owner := strings.ToLower(strings.TrimSpace(m.OwnedBy))
		switch owner {
		case "vllm", "sglang", "ollama", "lmstudio", "tabbyapi", "koboldcpp", "lemonade",
			"strata", "basalt", "mistral.rs", "ktransformers", "exo":
			e.Kind = owner
		case "mistralrs", "mistral-rs":
			e.Kind = "mistral.rs"
		case "mlx", "mlx-lm", "mlx-community":
			e.Kind = "mlx"
		case "ik_llama.cpp", "ik_llamacpp":
			e.Kind = "ik_llama.cpp"
		case "llama.cpp", "llamacpp":
			e.Kind = "llama.cpp"
		}
		mlxMetadata = mlxMetadata || m.MLX != nil && string(m.MLX) != "null" && string(m.MLX) != "false" ||
			strings.EqualFold(m.ModelType, "mlx") || strings.HasPrefix(strings.ToLower(m.ID), "mlx-community/")
	}
	kinds := []string{}
	switch e.Kind {
	case "openai":
		kinds = []string{"service-health", "mistral.rs", "ktransformers", "exo",
			"ollama", "lmstudio", "sglang", "tabbyapi", "koboldcpp", "lemonade", "llama.cpp"}
	case "ollama", "lmstudio", "sglang", "tabbyapi", "koboldcpp", "lemonade", "llama.cpp", "strata", "basalt", "mistral.rs":
		kinds = []string{e.Kind}
	}
	nativeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	results := make([]directEngine, len(kinds))
	var wg sync.WaitGroup
	for i, kind := range kinds {
		wg.Go(func() { results[i] = probeDirectNative(nativeCtx, base, key, kind) })
	}
	wg.Wait()
	for _, info := range results {
		if info.Kind == "" {
			continue
		}
		e.Kind, e.Version = info.Kind, info.Version
		e.Router = e.Router || info.Router
		if info.Ctx > 0 {
			e.Ctx = info.Ctx
		}
		for _, id := range info.Models {
			if !slices.Contains(e.Models, id) {
				e.Models = append(e.Models, id)
			}
			item := info.Details[id]
			if item.MaxContext == 0 {
				item.MaxContext = e.Details[id].MaxContext
			}
			e.Details[id] = item
		}
		break
	}
	if e.Kind == "openai" && mlxMetadata {
		e.Kind = "mlx"
	}
	if models.Data == nil && e.Kind == "openai" {
		return e, errors.New("the engine returned an invalid model list")
	}
	if len(e.Models) == 0 && e.Kind != "ollama" && e.Kind != "lmstudio" && e.Kind != "tabbyapi" && e.Kind != "exo" {
		return e, errors.New("the engine is not serving any models")
	}
	return e, nil
}

func cleanDirectURL(rawURL string) (string, string, error) {
	base, u, err := cleanNodeURL(strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(rawURL), "/"), "/v1"))
	if err != nil {
		return "", "", err
	}
	return base, u.Hostname(), nil
}

func probeEngineAddress(w http.ResponseWriter, ctx context.Context, rawURL, key string) {
	base, _, err := cleanDirectURL(rawURL)
	if err == nil {
		var e directEngine
		e, err = probeDirectEngine(ctx, base, strings.TrimSpace(key))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "kind": e.Kind, "models": e.Models, "ctx": e.Ctx, "router": e.Router, "version": e.Version, "model_details": e.Details})
			return
		}
	}
	sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
}

func prepareDirectEngine(ctx context.Context, rawURL, key, model string) (*engineNode, error) {
	base, host, err := cleanDirectURL(rawURL)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	e, err := probeDirectEngine(ctx, base, key)
	if err != nil {
		return nil, err
	}
	if model == "" && len(e.Models) > 0 {
		model = e.Models[0]
	}
	if model != "" && !slices.Contains(e.Models, model) {
		return nil, errors.New("model not served by this engine")
	}
	return &engineNode{Direct: true, Kind: e.Kind, V1: base, APIKey: key, Hostname: host, Model: model,
		Ctx: e.contextFor(model), Router: e.Router, Version: e.Version, LinkedAt: time.Now().UnixMilli()}, nil
}

func (e directEngine) contextFor(model string) int {
	if ctx := e.Details[model].MaxContext; ctx > 0 {
		return ctx
	}
	if len(e.Models) > 0 && model != e.Models[0] && e.Details[e.Models[0]].MaxContext > 0 && e.Ctx == e.Details[e.Models[0]].MaxContext {
		return 0
	}
	return e.Ctx
}

func linkDirectEngine(ctx context.Context, rawURL, key, model string) (*engineNode, error) {
	n, err := prepareDirectEngine(ctx, rawURL, key, model)
	if err == nil {
		err = setEngineNode(n)
	}
	return n, err
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
		name := ""
		if n.Model != "" {
			name = path.Base(n.Model)
		}
		sendJSON(w, 200, map[string]any{"active": healthy, "health": healthy, "state": map[bool]string{true: "active", false: "inactive"}[healthy],
			"model": n.Model, "model_name": name, "ctx": n.Ctx, "ctx_native": n.Ctx, "ctx_effective": nil, "hostname": directHostname(n),
			"port": 0, "version": Version, "boot": procBoot, "preset_id": "", "preset_name": "", "load_error": "", "warn": "",
			"engine_direct": true, "engine_kind": n.Kind, "engine_url": n.V1, "engine_managed": n.Managed})
	case "/api/models":
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		e, err := probeDirectEngine(ctx, n.V1, n.APIKey)
		list := []map[string]any{}
		if err == nil {
			for _, m := range e.Models {
				info := e.Details[m]
				list = append(list, map[string]any{"name": path.Base(m), "value": m, "path": m, "dir": n.Hostname, "size": info.Size,
					"quantization": info.Quantization, "loaded": info.Loaded, "architecture": info.Architecture, "max_context_length": info.MaxContext, "remote": true})
			}
		}
		sendJSON(w, 200, list)
	case "/api/stop", "/api/unload":
		if r.Method == http.MethodPost && stopManagedDirect(n) {
			sendJSON(w, 200, map[string]any{"ok": true})
			return
		}
		local(w, r)
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
		next := *n
		next.Model, next.Ctx = req.Model, e.contextFor(req.Model)
		if err := setEngineNodeIfCurrent(n, &next); err != nil {
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
