package loom

import (
	"crypto/sha256"

	"fmt"
	"path/filepath"
)

// ModelChoice is a selectable model, independent from its provider and from the
// conversation using it. Enabled only controls visibility in the chat picker.
type ModelChoice struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	ProviderID   string `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	EngineValue  string `json:"engine_value,omitempty"` // engine library alias, not a filename match
	Endpoint     string `json:"endpoint,omitempty"`
	Enabled      bool   `json:"enabled"`
	Ready        bool   `json:"ready"`
	RuntimeID    string `json:"runtime_id,omitempty"`
}

func cloudChoiceID(provider, model string) string {
	return fmt.Sprintf("cloud:%x", sha256.Sum256([]byte(provider+"\x00"+model)))
}

func modelCatalog(providers []CloudProvider) []ModelChoice {
	return modelCatalogSources(providers, true)
}

// Bench reads cloud choices without rescanning engine libraries on every poll.
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
