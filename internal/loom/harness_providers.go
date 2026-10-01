package loom

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Loom's models inside harnesses. Each harness speaks one API format: Codex
// the OpenAI Responses API, Claude Code the Anthropic Messages API. A source
// fits a harness when it serves that format: Loom's local API (llama.cpp
// serves all of them) or a cloud provider known to expose it. Nothing is
// written to the harness's files: Loom passes the endpoint, model and key in
// the launch environment, only when such a model is picked.

const (
	acpLoomModelPrefix     = "loom:"     // loom:<local model id>
	acpProviderModelPrefix = "provider:" // provider:<provider id>:<model>
)

// harnessProtocol is the API format each env-configurable harness speaks.
var harnessProtocol = map[string]string{"codex": "responses", "claude-code": "anthropic"}

// providerProtocols returns the base URL a provider exposes for each format,
// derived from its OpenAI-compatible endpoint. Unknown hosts: Chat only.
func providerProtocols(endpoint string) map[string]string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	out := map[string]string{"chat": base}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return out
	}
	origin := u.Scheme + "://" + u.Host
	switch u.Hostname() {
	case "api.openai.com", "api.x.ai":
		out["responses"] = base
	case "openrouter.ai":
		out["responses"] = base
		out["anthropic"] = origin + "/api"
	case "api.anthropic.com":
		out["anthropic"] = origin
	case "api.deepseek.com":
		out["anthropic"] = origin + "/anthropic"
	case "api.moonshot.ai", "api.moonshot.cn", "api.minimax.io", "api.minimaxi.com":
		out["anthropic"] = origin + "/anthropic"
	case "api.z.ai":
		out["anthropic"] = origin + "/api/anthropic"
	case "open.bigmodel.cn":
		out["anthropic"] = origin + "/api/anthropic"
	}
	return out
}

// harnessSource is a resolved model source for one harness launch.
type harnessSource struct {
	Base, Model, Key string
	AnthropicKey     bool // the Anthropic API itself wants x-api-key
}

func (m *runtimeSessions) providerKey(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keys[id]
}

func providerByID(id string) (CloudProvider, bool) {
	var p CloudProvider
	ok := getStoreJSON(bkProviders, id, &p) && p.ID != ""
	return p, ok
}

// resolveHarnessSource maps a Loom model value to the endpoint the harness
// must call, or ok=false for the harness's own models.
func resolveHarnessSource(agentID, model string) (harnessSource, bool) {
	protocol := harnessProtocol[agentID]
	if protocol == "" {
		return harnessSource{}, false
	}
	if s, ok := modelSinkFor(agentID); !ok || s.Format != "env" {
		return harnessSource{}, false
	}
	if id, ok := strings.CutPrefix(model, acpLoomModelPrefix); ok && id != "" {
		key := loomAPIKey()
		if key == "" {
			key = "loom" // Loom's API accepts any key when none is required
		}
		base := fmt.Sprintf("http://127.0.0.1:%d", LLMPort())
		if protocol != "anthropic" {
			base += "/v1"
		}
		return harnessSource{Base: base, Model: id, Key: key}, true
	}
	rest, ok := strings.CutPrefix(model, acpProviderModelPrefix)
	if !ok {
		return harnessSource{}, false
	}
	pid, name, ok := strings.Cut(rest, ":")
	if !ok || pid == "" || name == "" {
		return harnessSource{}, false
	}
	p, found := providerByID(pid)
	base := providerProtocols(p.Endpoint)[protocol]
	key := workspaceSessions.providerKey(pid)
	if !found || base == "" || key == "" {
		return harnessSource{}, false
	}
	return harnessSource{Base: base, Model: name, Key: key, AnthropicKey: strings.Contains(base, "api.anthropic.com")}, true
}

// acpLoomModelEnv returns the launch environment that points the harness at
// the chosen Loom source, or nil for the harness's own models.
func acpLoomModelEnv(agentID, model string) []string {
	src, ok := resolveHarnessSource(agentID, model)
	if !ok {
		return nil
	}
	switch agentID {
	case "codex":
		cfg, _ := json.Marshal(map[string]any{
			"model_provider": "loom", "model": src.Model,
			"model_providers": map[string]any{"loom": map[string]any{
				"name": "Loom", "base_url": src.Base, "wire_api": "responses", "env_key": "LOOM_API_KEY"}},
		})
		return []string{"CODEX_CONFIG=" + string(cfg), "MODEL_PROVIDER=loom", "LOOM_API_KEY=" + src.Key}
	case "claude-code":
		env := []string{"ANTHROPIC_BASE_URL=" + src.Base, "ANTHROPIC_MODEL=" + src.Model,
			"ANTHROPIC_DEFAULT_OPUS_MODEL=" + src.Model, "ANTHROPIC_DEFAULT_SONNET_MODEL=" + src.Model,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + src.Model, "CLAUDE_CODE_SUBAGENT_MODEL=" + src.Model}
		if src.AnthropicKey {
			return append(env, "ANTHROPIC_API_KEY="+src.Key, "ANTHROPIC_AUTH_TOKEN=")
		}
		return append(env, "ANTHROPIC_AUTH_TOKEN="+src.Key, "ANTHROPIC_API_KEY=")
	}
	return nil
}

// harnessLoomChoices lists the Loom sources offered under an env-configured
// harness: local models, then the visible models of every connected cloud
// provider that serves the harness's format.
func harnessLoomChoices(d RuntimeDescriptor, ready bool) []ModelChoice {
	protocol := harnessProtocol[d.ID]
	if s, ok := modelSinkFor(d.ID); !ok || s.Format != "env" || protocol == "" || !modelSinkEnabled(d.ID) {
		return nil
	}
	out := []ModelChoice{}
	for _, id := range loomLocalModels() {
		out = append(out, ModelChoice{ID: d.ID + ":" + acpLoomModelPrefix + id, Name: strings.TrimSuffix(id, ".gguf"), Kind: "harness",
			ProviderName: d.Name, Model: acpLoomModelPrefix + id, RuntimeID: d.ID, Ready: ready, Via: "Loom · local"})
	}
	for _, p := range workspaceSessions.providers() {
		if !p.Ready || providerProtocols(p.Endpoint)[protocol] == "" {
			continue
		}
		models := p.Models
		if len(models) == 0 && p.Model != "" {
			models = []string{p.Model}
		}
		for _, name := range models {
			value := acpProviderModelPrefix + p.ID + ":" + name
			out = append(out, ModelChoice{ID: d.ID + ":" + value, Name: name, Kind: "harness", ProviderName: d.Name,
				Model: value, RuntimeID: d.ID, Ready: ready, Via: "Loom · " + p.Name})
		}
	}
	return out
}
