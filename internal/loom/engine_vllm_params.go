package loom

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// The same wire contract as llama.cpp; values are strings, as in its config
// editor. Missing values defer to vLLM. Saved values belong to a model ID.
func vllmParams(gpus int) []ParamSpec {
	num := func(id, label, tier, def string, min, max, step float64) ParamSpec {
		return ParamSpec{ID: id, Key: id, Flag: "--" + id, Label: label, Tier: tier, Kind: "number", Default: def, Min: &min, Max: &max, Step: &step, AffectsVRAM: true, RequiresReload: true, Available: true, Supported: true}
	}
	enum := func(id, label, def string, values ...string) ParamSpec {
		p := ParamSpec{ID: id, Key: id, Flag: "--" + id, Label: label, Tier: "advanced", Kind: "enum", Default: def, RequiresReload: true, Available: true, Supported: true}
		for _, s := range values {
			p.Choices = append(p.Choices, []string{s, s})
		}
		return p
	}
	boolean := func(id, label, def string) ParamSpec {
		return ParamSpec{ID: id, Key: id, Flag: "--" + id, Label: label, Tier: "advanced", Kind: "bool", Default: def, RequiresReload: true, Available: true, Supported: true}
	}
	ps := []ParamSpec{
		num("max-model-len", "Contexte maximal", "essential", "", 1, 2147483647, 1),
		num("gpu-memory-utilization", "Fraction de VRAM", "essential", "0.9", 0.01, 1, 0.01),
	}
	if gpus > 1 {
		ps = append(ps, num("tensor-parallel-size", "GPU en parallèle", "advanced", "1", 1, float64(gpus), 1))
	}
	ps = append(ps,
		enum("dtype", "Type des poids", "auto", "auto", "half", "float16", "bfloat16", "float", "float32"),
		enum("quantization", "Quantification", "auto", "auto", "awq", "awq_marlin", "gptq", "gptq_marlin", "fp8", "compressed-tensors", "bitsandbytes", "modelopt", "quark"),
		enum("kv-cache-dtype", "Type du cache KV", "auto", "auto", "fp8", "fp8_e4m3", "fp8_e5m2"),
		boolean("enable-prefix-caching", "Cache des préfixes", "on"),
		num("max-num-seqs", "Séquences simultanées", "advanced", "", 1, 2147483647, 1),
		boolean("enforce-eager", "Exécution eager", "off"),
		num("cpu-offload-gb", "Poids en RAM (Gio)", "advanced", "0", 0, 1048576, 0.1),
		num("swap-space", "Swap CPU par GPU (Gio)", "advanced", "4", 0, 1048576, 0.1),
	)
	for _, id := range []string{"reasoning-parser", "tool-call-parser"} {
		ps = append(ps, ParamSpec{ID: id, Key: id, Flag: "--" + id, Label: id, Tier: "advanced", Kind: "textarea", Default: "auto", Tip: "auto : famille connue ; none : désactivé ; sinon nom du parseur installé dans vLLM.", RequiresReload: true, Available: true, Supported: true})
	}
	ps = append(ps, boolean("enable-auto-tool-choice", "Choix automatique des outils", "off"), boolean("trust-remote-code", "Exécuter le code du modèle", "off"))
	ps[len(ps)-1].Dangerous = true
	ps[len(ps)-1].Tier = "expert"
	ps[len(ps)-1].Tip = "Dangereux : autorise l’exécution de code Python du dépôt Hugging Face avec les droits de Loom. Désactivé par défaut."
	return ps
}

var vllmParserName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,79}$`)

func validateVLLMParams(values map[string]string, gpus int) (map[string]string, error) {
	specs := map[string]ParamSpec{}
	for _, p := range vllmParams(gpus) {
		specs[p.ID] = p
	}
	out := map[string]string{}
	for k, s := range values {
		p, ok := specs[k]
		if !ok {
			return nil, fmt.Errorf("paramètre vLLM inconnu ou indisponible : %s", k)
		}
		if s == "" {
			continue
		}
		switch p.Kind {
		case "number":
			n, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < *p.Min || n > *p.Max || *p.Step == 1 && n != math.Trunc(n) {
				return nil, fmt.Errorf("valeur invalide pour %s", k)
			}
			s = strconv.FormatFloat(n, 'f', -1, 64)
		case "bool":
			switch s {
			case "on", "true":
				s = "on"
			case "off", "false":
				s = "off"
			default:
				return nil, fmt.Errorf("booléen invalide pour %s", k)
			}
		case "enum":
			found := false
			for _, c := range p.Choices {
				found = found || c[0] == s
			}
			if !found {
				return nil, fmt.Errorf("choix invalide pour %s", k)
			}
		default:
			if !vllmParserName.MatchString(s) {
				return nil, fmt.Errorf("nom de parseur invalide pour %s", k)
			}
		}
		out[k] = s
	}
	return out, nil
}

func validVLLMModel(model string) bool {
	return hubValidRepo(model) && model == strings.TrimSpace(model)
}

func buildVLLMArgs(model string, port int, values map[string]string, gpus int) ([]string, error) {
	if !validVLLMModel(model) || port < 1 || port > 65535 {
		return nil, fmt.Errorf("modèle ou port vLLM invalide")
	}
	values, err := validateVLLMParams(values, gpus)
	if err != nil {
		return nil, err
	}
	args := []string{"serve", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	for _, p := range vllmParams(gpus) {
		s := values[p.ID]
		if p.ID == "reasoning-parser" || p.ID == "tool-call-parser" {
			continue
		}
		if s == "" || p.ID == "quantization" && s == "auto" {
			continue
		}
		if p.Kind == "bool" {
			if s == "on" {
				args = append(args, p.Flag)
			} else if p.ID == "enable-prefix-caching" {
				args = append(args, "--no-enable-prefix-caching")
			}
		} else {
			args = append(args, p.Flag, s)
		}
	}
	parser := values["reasoning-parser"]
	if parser == "" || parser == "auto" {
		parser = vllmReasoningParser(model)
	}
	if parser != "" && parser != "none" {
		args = append(args, "--reasoning-parser", parser)
	}
	if values["enable-auto-tool-choice"] == "on" {
		parser = values["tool-call-parser"]
		if parser == "" || parser == "auto" {
			parser = vllmToolParser(model)
		}
		if parser == "" || parser == "none" {
			return nil, fmt.Errorf("choix automatique des outils : préciser tool-call-parser pour cette famille")
		}
		args = append(args, "--tool-call-parser", parser)
	}
	return args, nil
}

func vllmToolParser(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "qwen3-coder"):
		return "qwen3_xml"
	case strings.Contains(m, "qwen3") || strings.Contains(m, "qwen2.5") || strings.Contains(m, "hermes"):
		return "hermes"
	case strings.Contains(m, "llama-3.1") || strings.Contains(m, "llama-3.2") || strings.Contains(m, "llama-3.3"):
		return "llama3_json"
	case strings.Contains(m, "mistral") || strings.Contains(m, "mixtral"):
		return "mistral"
	case strings.Contains(m, "gpt-oss"):
		return "openai"
	case strings.Contains(m, "glm-4.5") || strings.Contains(m, "glm-4.6"):
		return "glm45"
	case strings.Contains(m, "deepseek-v3.1"):
		return "deepseek_v31"
	}
	return ""
}

func loadVLLMParams(model string) (map[string]string, error) {
	values := map[string]string{}
	key := "vllm_params:" + model
	raw, ok := getStoreBytes(bkState, key)
	if !ok {
		if len(getBytes(bkState, key)) > 0 {
			return nil, errStoreLocked
		}
		return values, nil
	}
	err := json.Unmarshal(raw, &values)
	if values == nil {
		values = map[string]string{}
	}
	return values, err
}

func handleVLLMParams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	gpus := len(liveGPUs())
	if r.Method == http.MethodGet {
		model := r.URL.Query().Get("model")
		values := map[string]string{}
		if model != "" {
			if !validVLLMModel(model) {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "modèle invalide"})
				return
			}
			var err error
			values, err = loadVLLMParams(model)
			if err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		sendJSON(w, 200, map[string]any{"ok": true, "engine_id": "vllm", "model": model, "params": vllmParams(gpus), "values": values, "gpu_count": gpus})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Model  string            `json:"model"`
		Values map[string]string `json:"values"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !validVLLMModel(req.Model) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "modèle invalide"})
		return
	}
	values, err := validateVLLMParams(req.Values, gpus)
	if err == nil {
		_, err = buildVLLMArgs(req.Model, 8000, values, gpus)
	}
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := putStoreJSON(bkState, "vllm_params:"+req.Model, values); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "model": req.Model, "values": values})
}
