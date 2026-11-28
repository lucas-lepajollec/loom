package loom

import (
	"context"
	"net/http"
	"runtime"
	"slices"
	"sync"
	"time"
)

const (
	ikBackendName             = "ik-llama-cpp"
	ikBackendURL              = "https://github.com/ikawrakow/ik_llama.cpp"
	basaltCUDAInstall         = "https://github.com/jesdga95/basalt/blob/main/docs/platforms.md"
	basaltMLXInstall          = "https://github.com/goodmai/basalt/blob/main/docs/install.md"
	engineCatalogProbeTimeout = 250 * time.Millisecond
)

type catalogEntry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Formats        []string `json:"formats"`
	Platforms      []string `json:"platforms"`
	Accelerators   []string `json:"accelerators"`
	Capabilities   []string `json:"capabilities"`
	DefaultPort    int      `json:"default_port"`
	InstallOptions []string `json:"install_options"`
	LinkOptions    []string `json:"link_options"`
	InstallGuide   string   `json:"install_guide,omitempty"`
	InstallCommand string   `json:"install_command,omitempty"`
	InstallHint    string   `json:"install_hint,omitempty"`
	HardwareBadge  string   `json:"hardware_badge,omitempty"`
	SourceURL      string   `json:"source_url,omitempty"`
	CustomName     string   `json:"custom_name,omitempty"`
	Detect         bool     `json:"-"`

	Status          string `json:"status"`
	DetectedURL     string `json:"detected_url"`
	Version         string `json:"version"`
	Compatible      bool   `json:"compatible"`
	RequirementNote string `json:"requirement_note"`
	Installed       bool   `json:"installed"`
	Job             string `json:"job,omitempty"`
	Binary          string `json:"binary,omitempty"`
}

func engineCatalogEntries() []catalogEntry {
	desktop := []string{"linux", "darwin", "windows"}
	gpu := []string{"CUDA", "ROCm"}
	llamaGPU := []string{"CPU", "CUDA", "ROCm", "Vulkan", "Metal"}
	link := []string{"url"}
	return []catalogEntry{
		{ID: "llama.cpp", Name: "llama.cpp", Category: "managed", Formats: []string{"GGUF"}, Platforms: desktop, Accelerators: llamaGPU,
			Capabilities: []string{"router", "presets", "vram"}, DefaultPort: 8080, InstallOptions: []string{"prebuilt", "source"}, LinkOptions: []string{"url", "binary"}},
		{ID: "ik_llama.cpp", Name: "ik_llama.cpp", Category: "managed", Formats: []string{"GGUF", "IQ"}, Platforms: desktop, Accelerators: llamaGPU,
			Capabilities: []string{"moe", "iq"}, DefaultPort: 8080, InstallOptions: []string{"custom"}, LinkOptions: link, SourceURL: ikBackendURL, CustomName: ikBackendName},
		{ID: "vllm", Name: "vLLM", Category: "managed", Formats: []string{"Safetensors", "AWQ", "GPTQ", "FP8"}, Platforms: []string{"linux"}, Accelerators: gpu,
			Capabilities: []string{"paged", "multi_gpu", "prefix_cache", "params"}, DefaultPort: 8000, InstallOptions: []string{"python"}, LinkOptions: link},
		{ID: "sglang", Name: "SGLang", Category: "managed", Formats: []string{"Safetensors", "FP8", "AWQ"}, Platforms: []string{"linux"}, Accelerators: gpu,
			Capabilities: []string{"radix", "structured", "multi_gpu", "params"}, DefaultPort: 30000, InstallOptions: []string{"python"}, LinkOptions: link, Detect: true},
		{ID: "mlx", Name: "MLX", Category: "managed", Formats: []string{"MLX"}, Platforms: []string{"darwin/arm64"}, Accelerators: []string{"Metal"},
			Capabilities: []string{"unified"}, DefaultPort: 8080, InstallOptions: []string{"python"}, LinkOptions: link},
		{ID: "ollama", Name: "Ollama", Category: "runtime", Formats: []string{"GGUF", "Modelfile"}, Platforms: desktop, Accelerators: llamaGPU,
			Capabilities: []string{"pull", "unload"}, DefaultPort: 11434, InstallOptions: []string{"guide"}, LinkOptions: link, Detect: true, InstallGuide: "https://ollama.com/download"},
		{ID: "lmstudio", Name: "LM Studio", Category: "runtime", Formats: []string{"GGUF", "MLX"}, Platforms: desktop, Accelerators: llamaGPU,
			Capabilities: []string{"model_metadata"}, DefaultPort: 1234, InstallOptions: []string{"guide"}, LinkOptions: link, Detect: true, InstallGuide: "https://lmstudio.ai/download"},
		{ID: "strata", Name: "Strata", Category: "runtime", Formats: []string{"GGUF", ".strata"}, Platforms: []string{"linux", "windows", "darwin/arm64"}, Accelerators: []string{"CUDA", "ROCm", "Metal"},
			Capabilities: []string{"expert_streaming", "speculative", "openai", "anthropic"}, DefaultPort: 8080, InstallOptions: []string{"guide"}, LinkOptions: link,
			InstallGuide: "https://github.com/Niko1221/Strata/blob/main/docs/INSTALL.md", InstallHint: "strata"},
		{ID: "basalt", Name: "Basalt", Category: "runtime", Formats: []string{".basalt", "MLX"}, Platforms: []string{"linux", "windows", "darwin/arm64"}, Accelerators: []string{"CUDA", "Metal"},
			Capabilities: []string{"moe", "nvfp4", "vision_offload", "openai", "anthropic"}, DefaultPort: 8080, InstallOptions: []string{"guide"}, LinkOptions: link,
			InstallGuide: basaltCUDAInstall, InstallHint: "basalt_cuda"},
		{ID: "mistral.rs", Name: "mistral.rs", Category: "runtime", Formats: []string{"GGUF", "Safetensors", "ISQ"}, Platforms: desktop, Accelerators: []string{"CPU", "CUDA", "Metal"},
			Capabilities: []string{"paged", "isq", "vision_audio"}, DefaultPort: 1234, InstallOptions: []string{"guide"}, LinkOptions: link,
			InstallGuide: "https://github.com/EricLBuehler/mistral.rs/blob/master/docs/src/content/docs/reference/installer.md", InstallHint: "mistral.rs"},
		{ID: "tabbyapi", Name: "TabbyAPI", Category: "server", Formats: []string{"EXL2", "EXL3"}, Platforms: []string{"linux", "windows"}, Accelerators: []string{"CUDA"},
			Capabilities: []string{"exllama", "multi_gpu"}, DefaultPort: 5000, LinkOptions: link, Detect: true},
		{ID: "koboldcpp", Name: "KoboldCpp", Category: "server", Formats: []string{"GGUF"}, Platforms: desktop, Accelerators: llamaGPU,
			Capabilities: []string{"creative"}, DefaultPort: 5001, LinkOptions: link, Detect: true},
		{ID: "lemonade", Name: "Lemonade", Category: "server", Formats: []string{"ONNX", "GGUF"}, Platforms: desktop, Accelerators: []string{"CPU", "CUDA", "ROCm", "Vulkan", "Metal", "AMD NPU"},
			Capabilities: []string{"npu"}, DefaultPort: 8000, LinkOptions: link, Detect: true},
		{ID: "ktransformers", Name: "KTransformers", Category: "server", Formats: []string{"GGUF", "Safetensors"}, Platforms: []string{"linux", "windows"}, Accelerators: []string{"CPU", "CUDA", "ROCm"},
			Capabilities: []string{"moe", "heterogeneous"}, DefaultPort: 10002, InstallOptions: []string{"guide"}, LinkOptions: link,
			InstallGuide: "https://github.com/kvcache-ai/ktransformers/blob/main/doc/en/install.md", InstallHint: "ktransformers"},
		{ID: "exo", Name: "exo", Category: "server", Formats: []string{"MLX", "Safetensors"}, Platforms: []string{"linux", "darwin/arm64"}, Accelerators: []string{"CPU", "Metal"},
			Capabilities: []string{"lan_cluster", "model_sharding"}, DefaultPort: 52415, InstallOptions: []string{"guide"}, LinkOptions: link, Detect: true,
			InstallGuide: "https://github.com/exo-explore/exo", InstallHint: "exo"},
		{ID: "openai", Name: "OpenAI compatible", Category: "server", Formats: []string{}, Platforms: []string{"any"}, Accelerators: []string{},
			Capabilities: []string{"openai"}, LinkOptions: link},
	}
}

type catalogHost struct {
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Accelerators []string `json:"accelerators"`
	GPUCount     int      `json:"gpu_count"`
	BasaltGPU    bool     `json:"-"`
}

func (h catalogHost) supportsLinuxGPU() bool { return h.OS == "linux" && h.primaryGPU() != "" }
func (h catalogHost) primaryGPU() string {
	for _, accel := range []string{"CUDA", "ROCm"} {
		if slices.Contains(h.Accelerators, accel) {
			return accel
		}
	}
	return ""
}

func detectCatalogHost() catalogHost {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return detectCatalogHostWithRunner(runtime.GOOS, runtime.GOARCH, gpuRunner(ctx))
}

func detectCatalogHostWithRunner(goos, arch string, run gpuCommandRunner) catalogHost {
	h := catalogHost{OS: goos, Arch: arch, Accelerators: []string{"CPU"}}
	if h.OS == "darwin" {
		if h.Arch == "arm64" {
			h.Accelerators = append(h.Accelerators, "Metal")
			h.GPUCount = 1
		}
		return h
	}
	var nvidia []gpuInfo
	var nvidiaCount int
	var amd []map[string]any
	var wg sync.WaitGroup
	wg.Go(func() {
		nvidia, _ = queryNvidiaGPUs(run)
		nvidiaCount = len(nvidia)
		if nvidiaCount == 0 {
			q := gpuQueries[0]
			data, err := run(q.name, q.args...)
			nvidiaCount = len(parseNvidiaGPUs(data, err))
		}
	})
	wg.Go(func() {
		amd = amdVramGPUsWithRunner(run)
		if len(amd) == 0 {
			amd = rocmVramGPUsWithRunner(run)
		}
	})
	wg.Wait()
	if nvidiaCount > 0 {
		h.Accelerators = append(h.Accelerators, "CUDA")
	}
	if len(amd) > 0 {
		h.Accelerators = append(h.Accelerators, "ROCm")
	}
	// Basalt ships Blackwell client and DGX Spark builds (SM 120/121).
	h.BasaltGPU = slices.ContainsFunc(nvidia, func(gpu gpuInfo) bool { return gpu.Cap == "12.0" || gpu.Cap == "12.1" })
	h.GPUCount = nvidiaCount + len(amd)
	return h
}

type activeCatalogEngine struct {
	Kind         string `json:"kind"`
	URL          string `json:"url"`
	Model        string `json:"model"`
	ConnectionID string `json:"connection_id,omitempty"`
	Managed      string `json:"managed,omitempty"`
}

func ikCatalogBinary() string {
	dir, err := backendDir(ikBackendName)
	if err != nil {
		return ""
	}
	return llamaServerBin(dir)
}

func configuredCatalogEngine(cfg map[string]string) string {
	if cfg["BIN"] != "" && samePath(cfg["BIN"], ikCatalogBinary()) {
		return "ik_llama.cpp"
	}
	return "llama.cpp"
}

func catalogActiveEngine(n *engineNode) activeCatalogEngine {
	if n == nil {
		cfg := ReadConfig()
		return activeCatalogEngine{Kind: configuredCatalogEngine(cfg), Model: cfg["MODEL"]}
	}
	if !n.Direct {
		return activeCatalogEngine{Kind: "loom-node", URL: n.URL}
	}
	out := activeCatalogEngine{Kind: n.Kind, URL: n.V1, Model: n.Model, Managed: n.Managed}
	if n.Managed == "" {
		out.ConnectionID = connectionID(n.V1)
	}
	return out
}

func catalogRequirementText(code string) string {
	switch code {
	case "linux_gpu":
		return "Local installation requires Linux and an NVIDIA (CUDA) or AMD (ROCm) GPU."
	case "apple_silicon":
		return "Local installation requires Apple Silicon. Link a running MLX server by address."
	case "python":
		return "Python 3 or uv is required for local installation."
	case "rocm_uv":
		return "uv is required to install vLLM with ROCm wheels."
	case "rocm_python":
		return "The managed ROCm build requires x86-64 and Python 3.10 or uv."
	case "platform":
		return "This engine is unavailable for local installation on this platform."
	}
	return ""
}

func catalogCompatibility(item *catalogEntry, h catalogHost, hasPython, hasUV, hasPython310 bool) {
	python := hasPython || hasUV || item.Installed
	item.Compatible = true
	switch item.ID {
	case "llama.cpp", "ik_llama.cpp":
		if !slices.Contains(item.Platforms, h.OS) || h.Arch != "amd64" && h.Arch != "arm64" {
			item.RequirementNote = "platform"
		}
	case "vllm", "sglang":
		if !h.supportsLinuxGPU() {
			item.RequirementNote = "linux_gpu"
		} else if !python {
			item.RequirementNote = "python"
		} else if item.ID == "vllm" && h.primaryGPU() == "ROCm" && !hasUV && !item.Installed {
			item.RequirementNote = "rocm_uv"
		} else if item.ID == "sglang" && h.primaryGPU() == "ROCm" && (h.Arch != "amd64" || !hasUV && !hasPython310 && !item.Installed) {
			item.RequirementNote = "rocm_python"
		}
	case "mlx":
		if h.OS != "darwin" || h.Arch != "arm64" {
			item.Category, item.InstallOptions, item.RequirementNote = "server", nil, "apple_silicon"
			return
		}
		if !python {
			item.RequirementNote = "python"
		}
	case "basalt":
		switch {
		case h.OS == "darwin" && h.Arch == "arm64":
			item.InstallGuide, item.InstallHint = basaltMLXInstall, "basalt_mlx"
			item.Formats, item.Accelerators = []string{"MLX", "GGUF"}, []string{"Metal"}
			item.Capabilities = []string{"moe", "unified", "openai", "anthropic"}
		case h.BasaltGPU && (h.OS == "linux" && (h.Arch == "amd64" || h.Arch == "arm64") || h.OS == "windows" && h.Arch == "amd64"):
			item.HardwareBadge = "blackwell"
			item.Formats, item.Accelerators = []string{".basalt"}, []string{"CUDA"}
		default:
			item.RequirementNote = "basalt_hardware"
		}
	case "strata", "mistral.rs", "ktransformers", "exo":
		if !slices.Contains(item.Platforms, h.OS) && !slices.Contains(item.Platforms, h.OS+"/"+h.Arch) {
			item.RequirementNote = "platform"
		}
	case "ollama":
		if h.OS == "linux" {
			item.InstallCommand = "curl -fsSL https://ollama.com/install.sh | sh"
		}
	}
	item.Compatible = item.RequirementNote == ""
}

func probeCatalogServices(ctx context.Context, items []catalogEntry, baseFor func(int) string) []directEngine {
	ctx, cancel := context.WithTimeout(ctx, engineCatalogProbeTimeout)
	defer cancel()
	probes := make([]directEngine, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		if !item.Detect {
			continue
		}
		wg.Go(func() {
			base := baseFor(item.DefaultPort)
			if item.ID == "sglang" || item.ID == "exo" {
				if info, err := probeDirectEngine(ctx, base, ""); err == nil && info.Kind == item.ID {
					probes[i] = info
				}
				return
			}
			probes[i] = probeDirectNative(ctx, base, "", item.ID)
		})
	}
	wg.Wait()
	return probes
}

func handleEngineCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	items := engineCatalogEntries()
	var probes []directEngine
	var hardware catalogHost
	var wg sync.WaitGroup
	wg.Go(func() { probes = probeCatalogServices(r.Context(), items, ownedEngineBase) })
	wg.Go(func() { hardware = detectCatalogHost() })
	connections, n, err := engineConnectionSnapshot()
	if err != nil {
		wg.Wait()
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	active := catalogActiveEngine(n)
	cfg := ReadConfig()
	ik := ikCatalogBinary()
	llamaInstalled := (isLlamaServerPath(cfg["BIN"]) && !samePath(cfg["BIN"], ik)) || prebuiltServerBin() != "" || llamaServerBin(llamacppRepoDir()) != ""
	llamaRunning := false
	if n == nil && (llamaInstalled || ik != "") {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(r.Context(), engineCatalogProbeTimeout)
			defer cancel()
			code, err := directGET(ctx, llamaBackendURL().String(), "/health", backendInferenceKey(""), nil)
			llamaRunning = err == nil && code == http.StatusOK
		})
	}
	wg.Wait()
	var llamaJob string
	if job := lcJobSnapshot(0, false); job["running"] == true {
		llamaJob, _ = job["action"].(string)
	}
	hasPython, hasUV, hasPython310 := vllmHasTool("python3"), vllmHasTool("uv"), vllmHasTool("python3.10")
	for i := range items {
		item := &items[i]
		switch item.ID {
		case "llama.cpp":
			item.Installed = llamaInstalled
			item.Version, _ = prebuiltVersion()
			if llamaJob != "install-custom" {
				item.Job = llamaJob
			}
		case "ik_llama.cpp":
			item.Installed, item.Binary = ik != "", ik
			if llamaJob == "install-custom" {
				item.Job = llamaJob
			}
		case "vllm":
			item.Installed = vllmInstalled()
			item.Version = pythonPackageVersion(vllmDir(), "vllm")
			vllm.mu.Lock()
			item.Job = vllm.job
			vllm.mu.Unlock()
		case "sglang", "mlx":
			e := sglang
			if item.ID == "mlx" {
				e = mlx
			}
			item.Version = e.version()
			item.Installed = item.Version != ""
			e.mu.Lock()
			item.Job = e.job
			e.mu.Unlock()
		}
		catalogCompatibility(item, hardware, hasPython, hasUV, hasPython310)
		if probes[i].Kind == item.ID {
			item.DetectedURL = ownedEngineBase(item.DefaultPort)
			if probes[i].Version != "" {
				item.Version = probes[i].Version
			}
		}
		switch {
		case active.Kind == item.ID && (n != nil || llamaRunning):
			item.Status = "active"
			if n != nil && n.Version != "" {
				item.Version = n.Version
			}
		case item.Installed:
			item.Status = "installed"
		case item.DetectedURL != "":
			item.Status = "detected"
		case !item.Compatible:
			item.Status = "unsupported"
		default:
			item.Status = "available"
		}
		if item.InstallOptions == nil {
			item.InstallOptions = []string{}
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "host": hardware, "engines": items, "connections": connections, "active_engine": active})
}
