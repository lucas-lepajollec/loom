package loom

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ModelChoice is a selectable model, independent from its provider and from the
// conversation using it. Enabled only controls visibility in the chat picker.
type ModelChoice struct {
	// Via: for a harness model served by Loom, where it comes from (local, a provider).
	Via              string   `json:"via,omitempty"`
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	ProviderID       string   `json:"provider_id,omitempty"`
	ProviderName     string   `json:"provider_name"`
	Model            string   `json:"model"`
	EngineValue      string   `json:"engine_value,omitempty"` // engine library alias, not a filename match
	Endpoint         string   `json:"endpoint,omitempty"`
	Enabled          bool     `json:"enabled"`
	Ready            bool     `json:"ready"`
	RuntimeID        string   `json:"runtime_id,omitempty"`
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
	DefaultEffort    string   `json:"default_effort,omitempty"`
}

func cloudChoiceID(provider, model string) string {
	return fmt.Sprintf("cloud:%x", sha256.Sum256([]byte(provider+"\x00"+model)))
}

func modelCatalog(providers []CloudProvider) []ModelChoice {
	return modelCatalogSources(providers, true)
}

// Bench needs only external native choices, without rescanning engine libraries
// or projecting Loom sources back through harnesses on every catalog poll.
func modelCatalogSources(providers []CloudProvider, includeEngineSources bool) []ModelChoice {
	choices := []ModelChoice{}
	seen := map[string]bool{}
	add := func(c ModelChoice) {
		if seen[c.ID] {
			return
		}
		seen[c.ID] = true
		c.Enabled = getStr(bkModelChoices, c.ID) != "hidden"
		choices = append(choices, c)
	}
	if includeEngineSources {
		if n := currentEngineNode(); n != nil {
			for _, c := range remoteEngineChoices(n) {
				add(c)
			}
		} else {
			cfg := ReadConfig()
			for _, dir := range modelDirs() {
				for _, file := range listGGUFFiles(dir) {
					if !ggufIsMmproj(file.Name) {
						value := file.Path
						if filepath.Dir(file.Path) == filepath.Clean(modelsDir()) || filepath.Dir(file.Path) == filepath.Clean(LoomHome()) {
							value = file.Name
						}
						add(ModelChoice{ID: "local:" + file.Path, Name: file.Name, Kind: "local", ProviderName: "llama.cpp", Model: file.Path, EngineValue: value, Ready: sameModelPath(cfg["MODEL"], file.Path)})
					}
				}
			}
			// Keep the configured model selectable even if it isn't in a managed folder.
			if cfg["MODEL"] != "" {
				model := cfg["MODEL"]
				listed := false
				for _, c := range choices {
					if c.Kind == "local" && sameModelPath(c.Model, model) {
						listed = true
						break
					}
				}
				if !listed {
					add(ModelChoice{ID: "local:" + model, Name: filepath.Base(model), Kind: "local", ProviderName: "llama.cpp", Model: model, Ready: true})
				}
			}
		}
	}
	for _, p := range providers {
		models := append([]string{p.Model}, p.Models...)
		for _, model := range models {
			if model != "" {
				add(ModelChoice{ID: cloudChoiceID(p.ID, model), Name: model, Kind: "cloud", ProviderID: p.ID, ProviderName: p.Name, Endpoint: p.Endpoint, Model: model, Ready: p.Ready})
			}
		}
	}
	for _, d := range runtimeCatalog() {
		adapter, _ := registeredRuntimes.lookup(d.ID)
		if acp, ok := adapter.(*acpAdapter); ok && harnessConnected(acp.agent) {
			ready := d.Available != nil && *d.Available
			probe, _ := loadACPProbe(d.ID)
			models := acpOptionValues(acpModelOption(probe.Config))
			if len(models) == 0 {
				add(ModelChoice{ID: d.ID + ":default", Name: d.Name, Kind: "harness", ProviderName: d.Name, RuntimeID: d.ID, Ready: ready})
			}
			// The agent's own model list, as announced in a probed session.
			for _, m := range models {
				name := m.Name
				if name == "" {
					name = m.Value
				}
				choice := ModelChoice{ID: d.ID + ":" + m.Value, Name: name, Kind: "harness", ProviderName: d.Name, Model: m.Value, RuntimeID: d.ID, Ready: ready}
				if d.ID == "codex" {
					for _, raw := range probe.NativeModels {
						var native struct {
							Model   string `json:"model"`
							Default string `json:"defaultReasoningEffort"`
							Efforts []struct {
								ID string `json:"reasoningEffort"`
							} `json:"supportedReasoningEfforts"`
						}
						if json.Unmarshal(raw, &native) == nil && native.Model == m.Value {
							choice.DefaultEffort = native.Default
							for _, e := range native.Efforts {
								choice.ReasoningEfforts = append(choice.ReasoningEfforts, e.ID)
							}
							break
						}
					}
				}
				add(choice)
			}
			// Loom's local models and compatible cloud providers, passed at launch.
			if includeEngineSources {
				for _, c := range harnessLoomChoices(d, ready) {
					add(c)
				}
			}
		}
	}

	// Harness models keep the order their agent announced (default first).
	sort.SliceStable(choices, func(i, j int) bool {
		if choices[i].Kind != choices[j].Kind {
			rank := map[string]int{"local": 0, "cloud": 1, "harness": 2}
			return rank[choices[i].Kind] < rank[choices[j].Kind]
		}
		if choices[i].Kind == "harness" {
			return false
		}
		return choices[i].Name < choices[j].Name
	})
	return choices
}

func sameModelPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	// Bare config filenames resolve in the standard models directory.
	if !filepath.IsAbs(a) {
		a = filepath.Join(modelsDir(), a)
	}
	if !filepath.IsAbs(b) {
		b = filepath.Join(modelsDir(), b)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

type HarnessProfile struct {
	ID        string   `json:"id"`
	RuntimeID string   `json:"runtime_id"`
	Name      string   `json:"name"`
	ModelIDs  []string `json:"model_ids"`
}

func harnessProfiles() []HarnessProfile {
	out := []HarnessProfile{}
	for id := range allKV(bkHarnessProfiles) {
		var p HarnessProfile
		if getStoreJSON(bkHarnessProfiles, id, &p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func saveHarnessProfile(p HarnessProfile) (HarnessProfile, error) {
	// Resolve providers before workspaceMu: discussion preparation takes the
	// session lock before workspaceMu, never the inverse.
	choices := modelCatalog(workspaceSessions.providers())
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	found := false
	for _, runtime := range runtimeCatalog() {
		if runtime.Kind == "harness" && runtime.ID == p.RuntimeID {
			found = true
		}
	}
	p.Name = strings.TrimSpace(p.Name)
	if !found || p.Name == "" || len(p.Name) > 100 || len(p.ModelIDs) > 32 {
		return p, fmt.Errorf("valid harness, name and maximum 32 models required")
	}
	available := map[string]bool{}
	for _, c := range choices {
		available[c.ID] = true
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range p.ModelIDs {
		if !available[id] {
			return p, fmt.Errorf("model not found")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	p.ModelIDs = ids
	if p.ID == "" {
		p.ID = newSessionID()
	} else {
		var old HarnessProfile
		if !getStoreJSON(bkHarnessProfiles, p.ID, &old) {
			return p, fmt.Errorf("configuration not found")
		}
	}
	return p, putStoreJSON(bkHarnessProfiles, p.ID, p)
}
