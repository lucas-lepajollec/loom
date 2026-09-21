package loom

import (
	"strings"
	"testing"
)

func TestParseModelLoadErrorOOM(t *testing.T) {
	log := `[loom serve] /bin/llama-server  model=qwen.gguf  port=8081
I srv    load_model: loading model '/tmp/qwen.gguf'
W common_fit_params: failed to fit params to free device memory: n_gpu_layers already set by user to 999, abort
E ggml_backend_cuda_buffer_type_alloc_buffer: allocating 122.28 MiB on device 0: cudaMalloc failed: out of memory
E llama_init_from_model: failed to initialize the context: failed to allocate compute pp buffers
E srv    load_model: failed to create_context with model '/tmp/qwen.gguf'
E srv  llama_server: exiting due to model loading error
`
	got := parseModelLoadError(log)
	if !strings.Contains(got, "VRAM") {
		t.Fatalf("attendu une erreur VRAM, got %q", got)
	}
}

func TestParseModelLoadErrorSuccessClears(t *testing.T) {
	log := `I srv    load_model: loading model '/tmp/qwen.gguf'
I srv  llama_server: model loaded
I srv  llama_server: listening on http://127.0.0.1:8081
`
	if got := parseModelLoadError(log); got != "" {
		t.Fatalf("chargement réussi : got %q", got)
	}
}

func TestParseEngineCrashDecodeOOM(t *testing.T) {
	log := `I srv  llama_server: model loaded
I slot launch_slot_: id  0 | task 1 | processing task, is_child = 0
E CUDA error: out of memory
E   cuMemCreate(&handle, reserve_size, &prop, 0)
`
	got := parseEngineCrash(log)
	if !strings.Contains(got, "VRAM") && !strings.Contains(got, "CUDA OOM") {
		t.Fatalf("attendu un plantage VRAM, got %q", got)
	}
}
