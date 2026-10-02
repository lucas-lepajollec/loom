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
	endpoint, err := validateCloudEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("enter a valid API key")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, errors.New("invalid destination")
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
		return nil, errors.New("catalog unreachable or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog rejected (HTTP %d); check the key and URL, or enter models manually", resp.StatusCode)
	}
	const limit = 3 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(b) > limit {
		return nil, errors.New("catalog too large or interrupted")
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &catalog) != nil || catalog.Data == nil {
		return nil, errors.New("incompatible catalog: data[].id list expected")
	}
	if len(catalog.Data) > 2048 {
		return nil, errors.New("catalog too large (maximum 2048 entries)")
	}
	models := []string{}
	seen := map[string]bool{}
	for _, item := range catalog.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 200 || strings.ContainsAny(id, "\r\n\x00") {
			continue
		}
		if !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
	}
	sort.Strings(models)
	return models, nil
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
	models, err := cloudModels(r.Context(), req.Endpoint, req.Key, nil)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": models, "endpoint": req.Endpoint})
}
