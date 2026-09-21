package loom

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// llm_oai_adapt.go — une app peut envoyer n'importe quel paramètre (OpenAI,
// Ollama `options`, camelCase, noms llama.cpp). Ce qui est une option de
// complétion reste dans CETTE requête. Ce qui est un drapeau de lancement
// (-np, -c, -ngl…) passe en overlay runtime : le process llama change, le
// fichier preset / le souvenir GGUF ne sont pas écrits. Les clés non envoyées
// restent celles du moteur chargé.

type oaiSplit struct {
	Body  map[string]any
	Run   map[string]string // clés config (NP, CTX, NGL…)
	Extra map[string]string // id de drapeau llama-server → valeur
}

var oaiBodyKeys = map[string]bool{
	"model": true, "messages": true, "prompt": true, "input": true,
	"stream": true, "stream-options": true, "stream_options": true,
	"temperature": true, "temp": true, "top-p": true, "top-k": true, "min-p": true,
	"typical-p": true, "tfs-z": true, "xtc-probability": true, "xtc-threshold": true,
	"dynatemp-range": true, "dynatemp-exponent": true,
	"max-tokens": true, "max-completion-tokens": true, "n-predict": true, "num-predict": true,
	"n": true, "stop": true, "seed": true, "num-seed": true,
	"presence-penalty": true, "frequency-penalty": true, "repeat-penalty": true,
	"repeat-last-n": true, "penalize-nl": true,
	"mirostat": true, "mirostat-tau": true, "mirostat-eta": true,
	"grammar": true, "json-schema": true, "response-format": true, "logit-bias": true,
	"cache-prompt": true, "n-keep": true, "n-discard": true, "n-indent": true,
	"samplers": true, "sampler-seq": true, "sampling-seq": true,
	"chat-template-kwargs": true, "reasoning": true, "reasoning-budget": true,
	"reasoning-effort": true, "reasoning-format": true, "enable-thinking": true,
	"tools": true, "tool-choice": true, "parallel-tool-calls": true, "functions": true,
	"id-slot": true, "slot-id": true, "lora": true, "image-data": true,
	"logprobs": true, "top-logprobs": true, "include-usage": true,
	"dry-multiplier": true, "dry-base": true, "dry-allowed-length": true, "dry-penalty-last-n": true,
	"system-prompt": true, "chat-template": true,
}

var oaiRuntimeAlias = map[string]string{
	"parallel": "NP", "n-parallel": "NP", "num-parallel": "NP",
	"np": "NP", "n-slots": "NP", "num-slots": "NP", "slots": "NP",
	"num-ctx": "CTX", "n-ctx": "CTX", "ctx-size": "CTX", "ctx": "CTX",
	"num-gpu": "NGL", "n-gpu-layers": "NGL", "n-gpu": "NGL", "ngl": "NGL",
	"gpu-layers": "NGL",
	"num-batch": "BATCH", "batch-size": "BATCH", "batch": "BATCH",
	"ubatch": "UBATCH", "ubatch-size": "UBATCH", "n-ubatch": "UBATCH",
	"num-thread": "THREADS", "num-threads": "THREADS", "threads": "THREADS",
	"threads-batch": "THREADS_BATCH",
	"cache-type-k": "KV_TYPE_K", "cache-type-v": "KV_TYPE_V",
	"ctk": "KV_TYPE_K", "ctv": "KV_TYPE_V",
}

var oaiNoOverlay = map[string]bool{
	"TEMP": true, "TOP_P": true, "TOP_K": true, "MIN_P": true,
	"PRESENCE_PENALTY": true, "REPEAT_PENALTY": true,
	"REASONING": true, "REASONING_BUDGET": true, "REASONING_EFFORT": true,
	"BIN": true, "HOST": true, "PORT": true, "MODEL": true,
	"API_KEY": true,
}

func oaiFlagID(k string) string {
	k = strings.TrimSpace(k)
	k = strings.TrimPrefix(k, "--")
	k = strings.TrimPrefix(k, "-")
	k = strings.ReplaceAll(k, "_", "-")
	return strings.ToLower(k)
}

func adaptOAICompletion(body []byte) ([]byte, oaiSplit, error) {
	var split oaiSplit
	if len(body) == 0 {
		return body, split, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, split, err
	}
	liftOAIMap(payload, "options")
	liftOAIMap(payload, "extra_body")
	liftOAIMap(payload, "parameters")
	aliasOAIGeneration(payload)
	split = splitOAIPayload(payload)
	out, err := json.Marshal(split.Body)
	if err != nil {
		return body, split, err
	}
	return out, split, nil
}

func liftOAIMap(dst map[string]any, key string) {
	raw, ok := dst[key]
	if !ok {
		return
	}
	src, ok := raw.(map[string]any)
	if !ok || src == nil {
		return
	}
	for k, v := range src {
		if _, exists := dst[k]; exists {
			continue
		}
		dst[k] = v
	}
	delete(dst, key)
}

func aliasOAIGeneration(m map[string]any) {
	renames := [][2]string{
		{"topP", "top_p"}, {"topK", "top_k"}, {"minP", "min_p"},
		{"repeatPenalty", "repeat_penalty"},
		{"presencePenalty", "presence_penalty"},
		{"frequencyPenalty", "frequency_penalty"},
		{"typicalP", "typical_p"},
		{"maxTokens", "max_tokens"},
		{"parallelToolCalls", "parallel_tool_calls"},
	}
	for _, pair := range renames {
		oaiPutIfAbsent(m, pair[1], m[pair[0]])
	}
	if !oaiHas(m, "max_tokens") {
		for _, k := range []string{"max_completion_tokens", "num_predict", "n_predict"} {
			if oaiHas(m, k) {
				m["max_tokens"] = m[k]
				break
			}
		}
	}
	if !oaiHas(m, "seed") {
		oaiPutIfAbsent(m, "seed", m["num_seed"])
	}
	if eff, ok := oaiString(m["reasoning_effort"]); ok && !oaiHas(m, "chat_template_kwargs") {
		m["chat_template_kwargs"] = map[string]any{"reasoning_effort": eff}
	}
}

func splitOAIPayload(m map[string]any) oaiSplit {
	out := oaiSplit{
		Body:  map[string]any{},
		Run:   map[string]string{},
		Extra: map[string]string{},
	}
	bin := strings.TrimSpace(ReadConfig()["BIN"])
	for k, v := range m {
		id := oaiFlagID(k)
		if oaiBodyKeys[id] || strings.HasPrefix(id, "parallel-tool") {
			out.Body[k] = v
			continue
		}
		if cfg, ok := oaiRuntimeAlias[id]; ok && !oaiNoOverlay[cfg] {
			if s, ok := oaiScalar(v); ok {
				out.Run[cfg] = s
			}
			continue
		}
		if cfg, ok := flagToConfigKey[id]; ok {
			if oaiNoOverlay[cfg] {
				out.Body[k] = v
				continue
			}
			if s, ok := oaiScalar(v); ok {
				out.Run[cfg] = s
			}
			continue
		}
		if bin != "" && llamaHasFlag(bin, id) {
			if s, ok := oaiScalar(v); ok {
				out.Extra[id] = s
			}
			continue
		}
		out.Body[k] = v
	}
	return out
}

func oaiHas(m map[string]any, k string) bool {
	v, ok := m[k]
	return ok && v != nil
}

func oaiPutIfAbsent(m map[string]any, k string, v any) {
	if v == nil || oaiHas(m, k) {
		return
	}
	m[k] = v
}

func oaiString(v any) (string, bool) {
	s, ok := v.(string)
	s = strings.TrimSpace(s)
	return s, ok && s != ""
}

func oaiScalar(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t), strings.TrimSpace(t) != ""
	case bool:
		if t {
			return "on", true
		}
		return "off", true
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case json.Number:
		return t.String(), t.String() != ""
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		return s, s != "" && s != "<nil>"
	}
}
