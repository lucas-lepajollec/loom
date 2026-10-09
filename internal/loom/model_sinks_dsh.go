package loom

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeepSeek Harness composes its configuration from patch layers and accepts
// extra ones with --patch. Loom's models therefore travel in a Loom-owned
// layer (never in the user's files). A patch replaces the whole config of an
// entry, so providers already declared in the user's own layers for
// llm-pi-ai are carried over; Loom's entries are the loom-* ones.
func dshPatchPath() string { return filepath.Join(LoomHome(), "harness", "dsh-loom-models.yml") }

func dshHome() string {
	if h := strings.TrimSpace(os.Getenv("DSH_HOME")); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

// dshUserProviders reads llm-pi-ai providers from the profile and home layers
// (later layers win, as in DeepSeek Harness).
func dshUserProviders(profile string) map[string]any {
	out := map[string]any{}
	for _, file := range []string{filepath.Join(dshHome(), "profiles", profile, "cordis.patch.yml"), filepath.Join(dshHome(), "cordis.patch.yml")} {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var entries []map[string]any
		if yaml.Unmarshal(b, &entries) != nil {
			continue
		}
		for _, e := range entries {
			if e["id"] != "llm-pi-ai" {
				continue
			}
			cfg, _ := e["config"].(map[string]any)
			providers, _ := cfg["providers"].(map[string]any)
			for k, v := range providers {
				if !strings.HasPrefix(k, "loom") {
					out[k] = v
				}
			}
		}
	}
	return out
}

func dshLoomProviders() map[string]any {
	providers := map[string]any{}
	if local := loomLocalModels(); len(local) > 0 {
		models := []any{}
		for _, id := range local {
			models = append(models, map[string]any{"id": id, "name": strings.TrimSuffix(id, ".gguf") + " (via Loom)"})
		}
		providers["loom"] = map[string]any{"displayName": "Loom (local)", "api": "openai-completions", "baseURL": engineBase() + "/v1", "apiKeyEnv": "LOOM_API_KEY", "models": models}
	}
	for _, p := range chatSources() {
		models := []any{}
		for _, m := range p.Models {
			models = append(models, map[string]any{"id": m, "name": m + " (via Loom)"})
		}
		providers[providerSlug(p)] = map[string]any{"displayName": p.Name + " (Loom)", "api": "openai-completions", "baseURL": providerProtocols(p.Endpoint)["chat"], "apiKeyEnv": providerKeyEnv(p.ID), "models": models}
	}
	return providers
}

// writeDshPatch writes (or removes) Loom's layer; it holds no secret, only
// environment variable names filled at launch.
func writeDshPatch(enabled bool) error {
	path := dshPatchPath()
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	providers := dshUserProviders("acp")
	for k, v := range dshLoomProviders() {
		providers[k] = v
	}
	b, err := yaml.Marshal([]any{map[string]any{"id": "llm-pi-ai", "config": map[string]any{"providers": providers}}})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// dshPatchArgs adds Loom's layer to a local DeepSeek Harness launch.
func dshPatchArgs(agent acpAgent, args []string) []string {
	if agent.Remote || !deepseekACPAgent(agent) || !modelSinkEnabled(agent.ID) {
		return args
	}
	if _, err := os.Stat(dshPatchPath()); err != nil {
		return args
	}
	for i, a := range args {
		if a == "--profile" || strings.HasPrefix(a, "--profile=") {
			end := i + 1
			if a == "--profile" {
				end = i + 2
			}
			if end > len(args) {
				break
			}
			out := append(append([]string{}, args[:end]...), "--patch", dshPatchPath())
			return append(out, args[end:]...)
		}
	}
	return append(append([]string{}, args...), "--patch", dshPatchPath())
}
