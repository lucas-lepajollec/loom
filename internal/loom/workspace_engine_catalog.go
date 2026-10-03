package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// A discussion chooses models from the active engine's library, not from the
// control plane's filesystem. Short caching bounds catalog/Usage polling; a
// link change has a different identity and cannot reuse another engine's paths.
var remoteChoiceCache struct {
	sync.Mutex
	node    *engineNode
	at      time.Time
	choices []ModelChoice
}

func remoteEngineChoices(n *engineNode) []ModelChoice {
	remoteChoiceCache.Lock()
	defer remoteChoiceCache.Unlock()
	if remoteChoiceCache.node == n && time.Since(remoteChoiceCache.at) < 5*time.Second {
		return append([]ModelChoice(nil), remoteChoiceCache.choices...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	read := func(base, endpoint, key string, out any) bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+endpoint, nil)
		if err != nil {
			return false
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := nodeClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out) == nil
	}
	choices := []ModelChoice{}
	if n.Direct {
		var models struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if read(n.V1, "/v1/models", n.APIKey, &models) {
			for _, m := range models.Data {
				if m.ID != "" {
					choices = append(choices, ModelChoice{ID: "local:" + m.ID, Kind: "local", Name: path.Base(m.ID), ProviderName: n.Kind, Model: m.ID, EngineValue: m.ID, Ready: n.Model == m.ID})
				}
			}
		}
	} else {
		var models []struct {
			Name, Path, Value string
			Mmproj            bool
		}
		if read(n.URL, "/api/models", n.WebKey, &models) {
			var status struct{ Model string }
			read(n.URL, "/api/status", n.WebKey, &status)
			for _, m := range models {
				if m.Path != "" && !m.Mmproj && !ggufIsMmproj(m.Name) {
					choices = append(choices, ModelChoice{ID: "local:" + m.Path, Kind: "local", Name: m.Name, ProviderName: "llama.cpp", Model: m.Path, EngineValue: m.Value, Ready: status.Model == m.Path || (m.Value != "" && status.Model == m.Value)})
				}
			}
		}
	}
	remoteChoiceCache.node, remoteChoiceCache.at, remoteChoiceCache.choices = n, time.Now(), choices
	return append([]ModelChoice(nil), choices...)
}
