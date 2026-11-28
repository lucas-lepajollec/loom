package loom

import (
	"fmt"
	"net/http"
	"strconv"
)

func sglangParams(gpus int) []ParamSpec {
	base := func(id, label, tier, kind, def string) ParamSpec {
		return ParamSpec{ID: id, Key: id, Flag: "--" + id, Label: label, Tier: tier, Kind: kind, Default: def, Available: true, Supported: true, RequiresReload: true}
	}
	num := func(id, label, tier, def string, min, max, step float64) ParamSpec {
		p := base(id, label, tier, "number", def)
		p.Min, p.Max, p.Step, p.AffectsVRAM = &min, &max, &step, true
		return p
	}
	enum := func(id, label string, options ...string) ParamSpec {
		p := base(id, label, "advanced", "enum", "auto")
		for _, value := range options {
			p.Choices = append(p.Choices, []string{value, value})
		}
		return p
	}
	ps := []ParamSpec{
		num("context-length", "Maximum context", "essential", "", 1, 2147483647, 1),
		num("mem-fraction-static", "VRAM fraction", "essential", "0.85", 0.01, 1, 0.01),
	}
	if gpus > 1 {
		ps = append(ps, num("tp-size", "GPUs in parallel", "advanced", "1", 1, float64(gpus), 1))
	}
	ps = append(ps,
		enum("dtype", "Weight type", "auto", "half", "float16", "bfloat16", "float", "float32"),
		enum("quantization", "Quantization", "auto", "awq", "awq_marlin", "gptq", "gptq_marlin", "fp8", "compressed-tensors", "modelopt", "bitsandbytes"),
		enum("kv-cache-dtype", "KV cache type", "auto", "fp8_e5m2", "fp8_e4m3"),
		num("chunked-prefill-size", "Chunked prefill size", "advanced", "", -1, 2147483647, 1),
		base("disable-radix-cache", "Disable RadixAttention cache", "advanced", "bool", "off"),
		num("max-running-requests", "Maximum concurrent requests", "advanced", "", 1, 2147483647, 1),
		base("reasoning-parser", "reasoning-parser", "advanced", "textarea", ""),
		base("tool-call-parser", "tool-call-parser", "advanced", "textarea", ""),
	)
	trust := base("trust-remote-code", "Run the model's code", "expert", "bool", "off")
	trust.Dangerous = true
	trust.Tip = "Dangerous: allows running Python code from the Hugging Face repository with Loom's rights. Off by default."
	return append(ps, trust)
}

func validateSGLangParams(values map[string]string, gpus int) (map[string]string, error) {
	out, err := validatePythonEngineParams("SGLang", sglangParams(gpus), values)
	if err == nil && out["chunked-prefill-size"] == "0" {
		return nil, fmt.Errorf("chunked-prefill-size must be positive or -1")
	}
	return out, err
}

func buildSGLangArgs(model string, port int, values map[string]string, gpus int) ([]string, error) {
	if !validVLLMModel(model) || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid SGLang model or port")
	}
	clean, err := validateSGLangParams(values, gpus)
	if err != nil {
		return nil, err
	}
	if clean["mem-fraction-static"] == "" {
		clean["mem-fraction-static"] = "0.85"
	}
	args := []string{"-m", "sglang.launch_server", "--model-path", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	for _, p := range sglangParams(gpus) {
		v := clean[p.ID]
		if v == "" || p.ID == "quantization" && v == "auto" {
			continue
		}
		if p.Kind != "bool" {
			args = append(args, p.Flag, v)
		} else if v == "on" {
			args = append(args, p.Flag)
		}
	}
	return args, nil
}

func handleSGLangParams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, message string) { sendJSON(w, code, map[string]any{"ok": false, "error": message}) }
	gpus := len(liveGPUs())
	model := r.URL.Query().Get("model")
	var values map[string]string
	var err error
	switch r.Method {
	case http.MethodGet:
		if model != "" && !validVLLMModel(model) {
			fail(400, "invalid model")
			return
		}
		values, err = loadPythonEngineParams("sglang", model)
	case http.MethodPost:
		var req struct {
			Model  string
			Values map[string]string
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		model = req.Model
		if !validVLLMModel(model) {
			fail(400, "invalid model")
			return
		}
		values, err = validateSGLangParams(req.Values, gpus)
		if err != nil {
			fail(400, err.Error())
			return
		}
		err = putStoreJSON(bkState, "sglang_params:"+model, values)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(405, "method not allowed")
		return
	}
	if err != nil {
		fail(500, err.Error())
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "engine_id": "sglang", "model": model, "params": sglangParams(gpus), "values": values, "gpu_count": gpus})
}
