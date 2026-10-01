package loom

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Model source "Loom" for open harnesses: Loom declares its local models as a
// provider in the harness's own configuration, so the harness lists and uses
// them natively (and keeps working without Loom for everything else). Only
// the provider named "loom" is ever written or removed.

type modelSink struct {
	Harness string // runtime id
	File    string // provider file of the harness
	Format  string
}

// Format "env": nothing is written; the harness receives Loom as a provider
// through its launch environment only when a Loom model is chosen (Codex).
var modelSinks = []modelSink{
	{Harness: "pi", File: "~/.pi/agent/models.json", Format: "pi"},
	{Harness: "codex", Format: "env"},
	{Harness: "claude-code", Format: "env"},
	{Harness: "opencode", Format: "opencode"},
}

const modelSinkState = "model_sinks" // map[harness]bool

var modelSinkMu sync.Mutex

func modelSinkFor(id string) (modelSink, bool) {
	for _, s := range modelSinks {
		if s.Harness == id {
			return s, true
		}
	}
	return modelSink{}, false
}

func modelSinkEnabled(id string) bool {
	m := map[string]bool{}
	_ = getStoreJSON(bkState, modelSinkState, &m)
	return m[id]
}

// loomLocalModels returns the ids Loom's OpenAI-compatible API accepts.
func loomLocalModels() []string {
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(engineModelsJSON(), &list)
	ids := []string{}
	for _, m := range list.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func loomAPIKey() string {
	if key := engineAPIKey(); key != "" {
		return key
	}
	return strings.TrimSpace(ReadConfig()["API_KEY"])
}

// writePiProvider adds, refreshes or removes providers.loom in Pi's file,
// preserving everything else verbatim at the JSON level.
func writePiProvider(path string, enabled bool, models []string, baseURL, key string, cloud ...map[string]any) error {
	doc := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return errors.New("fichier de fournisseurs Pi illisible : Loom n’y touche pas")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if !enabled {
		return nil
	}
	providers, _ := doc["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	if !enabled {
		changed := false
		for name := range providers {
			if name == "loom" || strings.HasPrefix(name, "loom-") {
				delete(providers, name)
				changed = true
			}
		}
		if !changed {
			return nil
		}
	} else {
		list := []any{}
		for _, id := range models {
			list = append(list, map[string]any{"id": id, "name": strings.TrimSuffix(id, ".gguf"), "reasoning": false, "input": []string{"text"}})
		}
		if key == "" {
			key = "loom" // the API accepts any key when none is required
		}
		providers["loom"] = map[string]any{"baseUrl": baseURL, "api": "openai-completions", "apiKey": key,
			"compat": map[string]any{"supportsDeveloperRole": false, "supportsReasoningEffort": false}, "models": list}
		// Loom's cloud providers: keys are environment references, set by Loom
		// when it launches Pi; never written here.
		for name := range providers {
			if strings.HasPrefix(name, "loom-") {
				delete(providers, name)
			}
		}
		if len(cloud) > 0 {
			for name, p := range cloud[0] {
				providers[name] = p
			}
		}
	}
	doc["providers"] = providers
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".loom-tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// syncModelSinks applies the stored choices (called on toggle and when the
// local library changes).
// resyncHarnessSources follows a change of Loom's sources (models, cloud
// providers): rewrite file sinks and re-probe the harnesses that list them.
func resyncHarnessSources() {
	_ = syncModelSinks()
	reprobe := false
	for _, s := range modelSinks {
		if (s.Format == "pi" || s.Format == "opencode") && modelSinkEnabled(s.Harness) {
			acpProbeMu.Lock()
			_ = putBytes(bkState, acpProbeKey+s.Harness, nil)
			acpProbeMu.Unlock()
			reprobe = true
		}
	}
	if reprobe {
		go probeMissingACPAgents()
	}
}

func syncModelSinks() error {
	modelSinkMu.Lock()
	defer modelSinkMu.Unlock()
	var firstErr error
	for _, s := range modelSinks {
		on := modelSinkEnabled(s.Harness)
		var err error
		switch s.Format {
		case "pi":
			err = writePiProvider(expandHome(s.File), on, loomLocalModels(), engineBase()+"/v1", loomAPIKey(), piCloudProviders())
		case "env", "opencode":
			// Read at launch: nothing to write.
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GET ?id=: whether Loom models are offered to this harness. POST {id, enabled}.
func handleModelSink(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		s, ok := modelSinkFor(id)
		sendJSON(w, 200, map[string]any{"ok": true, "supported": ok, "enabled": ok && modelSinkEnabled(id), "file": s.File, "format": s.Format, "models": len(loomLocalModels())})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if _, ok := modelSinkFor(req.ID); !ok {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "ce harness ne peut pas encore utiliser les modèles de Loom"})
		return
	}
	m := map[string]bool{}
	_ = getStoreJSON(bkState, modelSinkState, &m)
	m[req.ID] = req.Enabled
	if err := putStoreJSON(bkState, modelSinkState, m); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := syncModelSinks(); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if s, _ := modelSinkFor(req.ID); s.Format == "env" {
		sendJSON(w, 200, map[string]any{"ok": true})
		return
	}
	// The harness's model list changed: probe it again.
	acpProbeMu.Lock()
	_ = putBytes(bkState, acpProbeKey+req.ID, nil)
	acpProbeMu.Unlock()
	go probeMissingACPAgents()
	sendJSON(w, 200, map[string]any{"ok": true})
}
