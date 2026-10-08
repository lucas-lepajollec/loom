package loom

// Model discovery is an explicit, credential-only request. It neither sends a
// discussion nor generates tokens, and never imports a catalog automatically.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

func cloudModels(ctx context.Context, endpoint, key string, client *http.Client) ([]string, error) {
	models, _, err := cloudModelCatalog(ctx, endpoint, key, client)
	return models, err
}

func cloudModelCatalog(ctx context.Context, endpoint, key string, client *http.Client) ([]string, map[string]int, error) {
	endpoint, err := validateCloudEndpoint(endpoint)
	if err != nil {
		return nil, nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
		return nil, nil, errors.New("enter a valid API key")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, nil, errors.New("invalid destination")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, errors.New("catalog unreachable or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("catalog rejected (HTTP %d); check the key and URL, or enter models manually", resp.StatusCode)
	}
	const limit = 3 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(b) > limit {
		return nil, nil, errors.New("catalog too large or interrupted")
	}
	var catalog struct {
		Data []struct {
			ID            string          `json:"id"`
			ContextWindow json.RawMessage `json:"context_window"`
			ContextLength json.RawMessage `json:"context_length"`
			TopProvider   json.RawMessage `json:"top_provider"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &catalog) != nil || catalog.Data == nil {
		return nil, nil, errors.New("incompatible catalog: data[].id list expected")
	}
	if len(catalog.Data) > 2048 {
		return nil, nil, errors.New("catalog too large (maximum 2048 entries)")
	}
	models := []string{}
	windows := map[string]int{}
	seen := map[string]bool{}
	for _, item := range catalog.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 200 || strings.ContainsAny(id, "\r\n\x00") {
			continue
		}
		var top struct {
			ContextLength json.RawMessage `json:"context_length"`
		}
		_ = json.Unmarshal(item.TopProvider, &top)
		window := max(catalogContext(item.ContextWindow), catalogContext(item.ContextLength), catalogContext(top.ContextLength))
		if window > 0 {
			windows[id] = window
		}
		if !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
	}
	sort.Strings(models)
	return models, windows, nil
}

func catalogContext(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return 0
	}
	return max(0, n)
}

func handleProviderModels(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID       string `json:"id"`
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
		Consent  bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !req.Consent {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "confirm sending the key to this destination to retrieve its catalog"})
		return
	}
	if req.ID != "" {
		workspaceSessions.mu.Lock()
		var p CloudProvider
		found := getStoreJSON(bkProviders, req.ID, &p)
		if req.Key == "" {
			req.Key = workspaceSessions.keys[req.ID]
		}
		workspaceSessions.mu.Unlock()
		if !found || (req.Endpoint != "" && req.Endpoint != p.Endpoint) {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "connection not found or destination differs"})
			return
		}
		req.Endpoint = p.Endpoint
	}
	models, windows, err := cloudModelCatalog(r.Context(), req.Endpoint, req.Key, nil)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.ID != "" {
		workspaceSessions.providerMu.Lock()
		var p CloudProvider
		if getStoreJSON(bkProviders, req.ID, &p) && p.Endpoint == req.Endpoint {
			p.ContextWindows = windows
			err = putStoreJSON(bkProviders, p.ID, p)
		}
		workspaceSessions.providerMu.Unlock()
		if err != nil {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "catalog context windows could not be saved"})
			return
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": models, "context_windows": windows, "endpoint": req.Endpoint})
}
