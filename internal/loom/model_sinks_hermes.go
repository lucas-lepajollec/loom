package loom

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hermes reads custom providers from its single config.yaml. Loom adds only
// providers whose name starts with "loom" (keys stay in environment variables
// named by key_env) and leaves the rest of the document, comments included,
// untouched. Hermes's ACP adapter lists only its default provider's models, so
// Loom adds its own choices to the model option; session/set_model accepts
// "provider:model" for any declared provider.
func hermesConfigPath() string {
	home := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".hermes")
	}
	return filepath.Join(home, "config.yaml")
}

func hermesLoomProviders() map[string]map[string]any {
	out := map[string]map[string]any{}
	if local := loomLocalModels(); len(local) > 0 {
		out["loom"] = map[string]any{"name": "Loom (local)", "base_url": engineBase() + "/v1", "key_env": "LOOM_API_KEY", "models": local}
	}
	for _, p := range chatSources() {
		out[providerSlug(p)] = map[string]any{"name": p.Name + " (Loom)", "base_url": providerProtocols(p.Endpoint)["chat"], "key_env": providerKeyEnv(p.ID), "models": p.Models}
	}
	return out
}

func writeHermesProviders(path string, enabled bool, providers map[string]map[string]any) error {
	var doc yaml.Node
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return errors.New("Hermes config.yaml unreadable: Loom will not modify it")
		}
	case os.IsNotExist(err):
		if !enabled {
			return nil
		}
	default:
		return err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return errors.New("Hermes config.yaml is not a mapping: Loom will not modify it")
	}
	var section *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "providers" {
			section = root.Content[i+1]
		}
	}
	if section == nil {
		if !enabled {
			return nil
		}
		section = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "providers"}, section)
	}
	if section.Kind != yaml.MappingNode {
		return errors.New("Hermes providers section is not a mapping: Loom will not modify it")
	}
	kept := []*yaml.Node{}
	for i := 0; i+1 < len(section.Content); i += 2 {
		if !strings.HasPrefix(section.Content[i].Value, "loom") {
			kept = append(kept, section.Content[i], section.Content[i+1])
		}
	}
	section.Content = kept
	if enabled {
		for name, p := range providers {
			var v yaml.Node
			if err := v.Encode(p); err != nil {
				return err
			}
			section.Content = append(section.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &v)
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // Hermes's own style
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	_ = enc.Close()
	out := buf.Bytes()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".loom-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// withLoomModelOptions adds Loom's models to Hermes's model option.
func withLoomModelOptions(a acpAgent, options []map[string]any) []map[string]any {
	if a.ID != "hermes" || a.Remote || !modelSinkEnabled(a.ID) {
		return options
	}
	for _, o := range options {
		if o["category"] != "model" && o["id"] != "model" {
			continue
		}
		list, _ := o["options"].([]any)
		seen := map[string]bool{}
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				if v, ok := m["value"].(string); ok {
					seen[v] = true
				}
			}
		}
		for slug, p := range hermesLoomProviders() {
			models, _ := p["models"].([]string)
			for _, m := range models {
				if v := slug + ":" + m; !seen[v] {
					list = append(list, map[string]any{"value": v, "name": strings.TrimSuffix(m, ".gguf") + " (via Loom)"})
				}
			}
		}
		o["options"] = list
	}
	return options
}
