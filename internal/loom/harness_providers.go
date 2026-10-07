package loom

import (
	"encoding/json"
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
		key := engineAPIKey()
		if key == "" {
			key = "loom" // Loom's API accepts any key when none is required
		}
		base := engineBase() // this machine, or the linked remote engine
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
		conf := map[string]any{
			"model_provider": "loom", "model": src.Model,
			"model_providers": map[string]any{"loom": map[string]any{
				"name": "Loom", "base_url": src.Base, "wire_api": "responses", "env_key": "LOOM_API_KEY"}},
		}
		// Codex does not know local models: tell it their real context size.
		if strings.HasPrefix(model, acpLoomModelPrefix) {
			if ctx := engineContextSize(); ctx > 0 {
				conf["model_context_window"] = ctx
			}
		}
		cfg, _ := json.Marshal(conf)
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

// providerKeyEnv names the variable carrying a cloud provider's key when Loom
// launches a harness that references it (Pi, OpenCode).
func providerKeyEnv(id string) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, id)
	return "LOOM_KEY_" + strings.ToUpper(safe)
}

func providerSlug(p CloudProvider) string { return "loom-" + skillDirSlug(p.Name) }

// chatSources lists the connected cloud providers usable through the OpenAI
// Chat Completions format, with their visible models.
func chatSources() []CloudProvider {
	out := []CloudProvider{}
	for _, p := range workspaceSessions.providers() {
		if !p.Ready || providerProtocols(p.Endpoint)["chat"] == "" {
			continue
		}
		if len(p.Models) == 0 && p.Model != "" {
			p.Models = []string{p.Model}
		}
		if len(p.Models) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// piCloudProviders: Pi provider entries for Loom's cloud providers.
func piCloudProviders() map[string]any {
	out := map[string]any{}
	for _, p := range chatSources() {
		// DeepSeek models think unless told otherwise: declare them as
		// reasoning models with DeepSeek's thinking switch, so Pi's "off"
		// level really sends thinking:{type:disabled}.
		deepseek := strings.Contains(strings.ToLower(p.Endpoint), "deepseek.com")
		models := []any{}
		for _, m := range p.Models {
			entry := map[string]any{"id": m, "name": m + " (via Loom)"}
			if deepseek {
				entry["reasoning"] = true
			}
			models = append(models, entry)
		}
		provider := map[string]any{"baseUrl": providerProtocols(p.Endpoint)["chat"], "api": "openai-completions",
			"apiKey": "$" + providerKeyEnv(p.ID), "models": models}
		if deepseek {
			provider["compat"] = map[string]any{"thinkingFormat": "deepseek"}
		}
		out[providerSlug(p)] = provider
	}
	return out
}

// openCodeConfig is passed in OPENCODE_CONFIG_CONTENT: Loom's local models and
// cloud providers as OpenCode providers, keys as {env:…} references.
func openCodeConfig() string {
	providers := map[string]any{}
	if local := loomLocalModels(); len(local) > 0 {
		models := map[string]any{}
		for _, id := range local {
			models[id] = map[string]any{"name": strings.TrimSuffix(id, ".gguf")}
		}
		providers["loom"] = map[string]any{"npm": "@ai-sdk/openai-compatible", "name": "Loom (local)",
			"options": map[string]any{"baseURL": engineBase() + "/v1", "apiKey": "{env:LOOM_API_KEY}"}, "models": models}
	}
	for _, p := range chatSources() {
		models := map[string]any{}
		for _, m := range p.Models {
			models[m] = map[string]any{"name": m}
		}
		providers[providerSlug(p)] = map[string]any{"npm": "@ai-sdk/openai-compatible", "name": p.Name + " (Loom)",
			"options": map[string]any{"baseURL": providerProtocols(p.Endpoint)["chat"], "apiKey": "{env:" + providerKeyEnv(p.ID) + "}"}, "models": models}
	}
	b, _ := json.Marshal(map[string]any{"provider": providers})
	return string(b)
}

// acpLaunchEnv is the whole environment Loom adds when it starts a harness:
// the chosen Loom source for harnesses configured per launch (Codex, Claude
// Code), or every Loom source for harnesses that list them (Pi, OpenCode)
// when the user enabled "Modèles de Loom" for them. Keys live only here.
func acpLaunchEnv(agentID, model string) []string {
	env := acpLoomModelEnv(agentID, model)
	s, ok := modelSinkFor(agentID)
	if !ok || (s.Format != "pi" && s.Format != "opencode") || !modelSinkEnabled(agentID) {
		return env
	}
	key := engineAPIKey()
	if key == "" {
		key = "loom"
	}
	env = append(env, "LOOM_API_KEY="+key)
	for _, p := range chatSources() {
		if k := workspaceSessions.providerKey(p.ID); k != "" {
			env = append(env, providerKeyEnv(p.ID)+"="+k)
		}
	}
	if s.Format == "opencode" {
		env = append(env, "OPENCODE_CONFIG_CONTENT="+openCodeConfig())
	}
	return env
}
