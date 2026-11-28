package loom

import (
	"context"
	"net/http"
	"path"
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
		code, err := directGET(ctx, base, endpoint, key, out)
		return err == nil && code == http.StatusOK
	}

	kind, model, direct := n.Kind, n.Model, n.Direct
	if !direct {
		var status struct {
			Model  string
			Direct bool   `json:"engine_direct"`
			Kind   string `json:"engine_kind"`
		}
		if read(n.URL, "/api/status", n.WebKey, &status) {
			kind, model, direct = status.Kind, status.Model, status.Direct
		}
	}
	choices := []ModelChoice{}
	if direct {
		var models directModelList
		if read(n.V1, "/v1/models", n.APIKey, &models) {
			for _, m := range models.Data {
				if m.ID != "" {
					choices = append(choices, ModelChoice{ID: "local:" + m.ID, Kind: "local", Name: path.Base(m.ID), ProviderName: kind, Model: m.ID, EngineValue: m.ID, Ready: model == m.ID})
				}
			}
		}
	} else {
		var models []struct {
			Name, Path, Value string
			Mmproj            bool
		}
		if read(n.URL, "/api/models", n.WebKey, &models) {
			for _, m := range models {
				if m.Path != "" && !m.Mmproj && !ggufIsMmproj(m.Name) {
					choices = append(choices, ModelChoice{ID: "local:" + m.Path, Kind: "local", Name: m.Name, ProviderName: "llama.cpp", Model: m.Path, EngineValue: m.Value, Ready: model == m.Path || (m.Value != "" && model == m.Value)})
				}
			}
		}
	}
	remoteChoiceCache.node, remoteChoiceCache.at, remoteChoiceCache.choices = n, time.Now(), choices
	return append([]ModelChoice(nil), choices...)
}
