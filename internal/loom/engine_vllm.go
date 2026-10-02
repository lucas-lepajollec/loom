package loom

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// vLLM as a second local engine: Loom installs it in its own Python
// environment (LOOM_HOME/engines/vllm, with uv when available so the right
// Python version comes with it), starts `vllm serve <model>` on this machine
// and uses it like any directly linked engine. vLLM keeps working without
// Loom: the environment is an ordinary one and the command is shown.

type vllmState struct {
	mu           sync.Mutex
	job          string // "", "install", "update", "start", "stop"
	log          []string
	err          string
	cmd          *exec.Cmd
	done         chan struct{}
	startCtx     context.Context
	startCancel  context.CancelFunc
	model        string
	pendingModel string
	port         int
	started      time.Time
}

var vllm = &vllmState{}

func vllmDir() string { return filepath.Join(LoomHome(), "engines", "vllm") }

func vllmBin() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(vllmDir(), "Scripts", "vllm.exe")
	}
	return filepath.Join(vllmDir(), "bin", "vllm")
}

func vllmInstalled() bool { return regularFile(vllmBin()) }

func (v *vllmState) appendLog(line string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.log = append(v.log, line)
	if len(v.log) > 400 {
		v.log = v.log[len(v.log)-400:]
	}
}

func (v *vllmState) runLogged(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	v.appendLog("$ " + name + " " + strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		v.appendLog(sc.Text())
	}
	return cmd.Wait()
}

// vllmRequirements explains what is missing on this machine, or "".
func vllmServingRequirements() string {
	if runtime.GOOS != "linux" {
		return "local vLLM requires Linux and an NVIDIA (CUDA) or AMD (ROCm) GPU; use llama.cpp or a remote engine on macOS/Windows"
	}
	if len(liveGPUs()) == 0 {
		return "vLLM requires an NVIDIA (CUDA) or AMD (ROCm) GPU detected by nvidia-smi, amd-smi or rocm-smi"
	}
	return ""
}

func vllmRequirements() string {
	if missing := vllmServingRequirements(); missing != "" {
		return missing
	}
	if !vllmInstalled() && !hasTool("uv") && !hasTool("python3") {
		return "Python 3 (or uv) is required to install vLLM"
	}
	if vllmROCm() && !hasTool("uv") {
		return "AMD ROCm installation/update requires uv to select official ROCm wheels"
	}
	return ""
}

func vllmROCm() bool {
	gpus, err := detectGPUs()
	return (err != nil || len(gpus) == 0) && (hasTool("amd-smi") || hasTool("rocm-smi"))
}

func (v *vllmState) install() { _ = v.installOrUpdate(false) }

func (v *vllmState) installOrUpdate(update bool) error {
	defer func() { v.mu.Lock(); v.job = ""; v.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	dir := vllmDir()
	err := os.MkdirAll(filepath.Dir(dir), 0o755)
	if err == nil {
		if uv, e := exec.LookPath("uv"); e == nil {
			if !update {
				err = v.runLogged(ctx, uv, "venv", "--python", "3.12", dir)
			}
			if err == nil {
				args := []string{"pip", "install", "--python", filepath.Join(dir, "bin", "python")}
				if update {
					args = append(args, "--upgrade")
				}
				if vllmROCm() {
					args = append(args, "--extra-index-url", "https://wheels.vllm.ai/rocm/")
				}
				err = v.runLogged(ctx, uv, append(args, "vllm")...)
			}
		} else {
			if !update {
				err = v.runLogged(ctx, "python3", "-m", "venv", dir)
			}
			if err == nil {
				args := []string{"-m", "pip", "install"}
				if update {
					args = append(args, "--upgrade")
				}
				err = v.runLogged(ctx, filepath.Join(dir, "bin", "python"), append(args, "vllm")...)
			}
		}
	}
	if err == nil && !vllmInstalled() {
		err = errors.New("vllm command not found after installation")
	}
	v.mu.Lock()
	if err != nil {
		v.err = err.Error()
	} else {
		v.err = ""
	}
	v.mu.Unlock()
	return err
}

var vllmModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// vllmArgs builds `vllm serve` for a model with optional limits.
func vllmArgs(model string, port int, gpuUtil float64, maxLen int) []string {
	values := map[string]string{}
	if gpuUtil != 0 {
		values["gpu-memory-utilization"] = strconv.FormatFloat(gpuUtil, 'f', -1, 64)
	}
	if maxLen != 0 {
		values["max-model-len"] = strconv.Itoa(maxLen)
	}
	args, _ := buildVLLMArgs(model, port, values, 1)
	return args
}

func (v *vllmState) start(ctx context.Context, model string, values map[string]string, gpus int) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	args, err := buildVLLMArgs(model, port, values, gpus)
	if err != nil {
		return err
	}
	cmd := exec.Command(vllmBin(), args...)
	// No usage reporting; the FlashInfer sampler compiles kernels with the
	// system CUDA compiler at first use, which fails on many machines: use
	// vLLM's built-in sampler instead.
	cmd.Env = append(os.Environ(), "VLLM_NO_USAGE_STATS=1", "VLLM_USE_FLASHINFER_SAMPLER=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	acpProcessGroup(cmd)
	v.mu.Lock()
	if err := ctx.Err(); err != nil {
		v.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		v.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	v.cmd, v.done, v.model, v.port, v.started, v.err = cmd, done, model, port, time.Now(), ""
	v.log = append(v.log, "$ vllm "+strings.Join(args, " "))
	v.mu.Unlock()
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			v.appendLog(sc.Text())
		}
		err := cmd.Wait()
		close(done)
		v.mu.Lock()
		if v.cmd == cmd {
			v.cmd = nil
			if err != nil && v.job == "" {
				v.err = "vLLM stopped: " + err.Error()
			}
		}
		v.mu.Unlock()
	}()
	// Loading a model takes time (download, compile): wait for /health.
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		v.mu.Lock()
		alive := v.cmd == cmd
		v.mu.Unlock()
		if !alive {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("vLLM stopped during loading (see the log)")
		}
		healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		code, err := directGET(healthCtx, base, "/health", "", nil)
		cancel()
		if err == nil && code == 200 {
			linkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			_, err := linkDirectEngine(linkCtx, base, "", "")
			if ctx.Err() != nil {
				v.stop()
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			v.stop()
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	v.stop()
	return errors.New("vLLM did not respond within 20 minutes")
}

func (v *vllmState) stop() {
	v.mu.Lock()
	cmd, done := v.cmd, v.done
	v.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			acpKillProcessGroup(cmd)
			<-done
		}
	}
	// The engine link pointed at this vLLM: back to this machine's llama.cpp.
	if n := currentEngineNode(); n != nil && n.Direct && strings.HasPrefix(n.V1, "http://127.0.0.1:") && n.Kind == "vllm" {
		_ = setEngineNode(nil)
	}
}

// reserve prevents installing/updating a live environment and serializes actions.
func (v *vllmState) reserve(action, model string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.job != "" || (action == "install" || action == "update") && v.cmd != nil {
		return errors.New("vLLM is running or an action is already in progress")
	}
	v.job, v.err = action, ""
	if action == "start" {
		v.pendingModel = model
		v.startCtx, v.startCancel = context.WithCancel(context.Background())
	}
	if action != "stop" {
		v.log = nil
	}
	return nil
}

// GET: state. POST: install|update|start|stop. Start overlays saved per-model
// settings in memory; legacy limits remain accepted without silently dropping errors.
func handleVLLM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		version, missing := vllmVersion(), vllmRequirements()
		vllm.mu.Lock()
		state := map[string]any{"ok": true, "installed": vllmInstalled(), "version": version, "missing": missing, "job": vllm.job, "error": vllm.err,
			"running": vllm.cmd != nil, "model": vllm.model, "dir": vllmDir(), "log": strings.Join(vllm.log, "\n"), "auto_update": loadVLLMAuto()}
		vllm.mu.Unlock()
		sendJSON(w, 200, state)
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Action  string            `json:"action"`
		Model   string            `json:"model"`
		GPUUtil *float64          `json:"gpu_memory_utilization"`
		MaxLen  *int              `json:"max_model_len"`
		Params  map[string]string `json:"params"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Action != "install" && req.Action != "update" && req.Action != "start" && req.Action != "stop" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown action"})
		return
	}
	if req.Action == "stop" {
		vllm.mu.Lock()
		loading := vllm.job == "start"
		if loading && vllm.startCancel != nil {
			vllm.startCancel()
		}
		vllm.mu.Unlock()
		if loading {
			vllm.stop()
			sendJSON(w, 200, map[string]any{"ok": true})
			return
		}
	}
	// Refuse first, even on unsupported hosts, and before clearing state/logs.
	vllm.mu.Lock()
	busy := vllm.job != "" || (req.Action == "install" || req.Action == "update") && vllm.cmd != nil
	vllm.mu.Unlock()
	if busy {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "vLLM is running or an action is already in progress"})
		return
	}
	fail := func(err error) { sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()}) }
	var values map[string]string
	gpus := 0
	if req.Action != "stop" {
		missing := ""
		if req.Action == "start" {
			missing = vllmServingRequirements()
		} else {
			missing = vllmRequirements()
		}
		if m := missing; m != "" {
			fail(errors.New(m))
			return
		}
		if req.Action != "install" && !vllmInstalled() {
			fail(errors.New("vLLM is not installed"))
			return
		}
	}
	if req.Action == "start" {
		if !validVLLMModel(req.Model) {
			fail(errors.New("invalid model ID (e.g. Qwen/Qwen3-8B)"))
			return
		}
		var err error
		values, err = loadVLLMParams(req.Model)
		if err != nil {
			fail(err)
			return
		}
		for k, value := range req.Params {
			values[k] = value
		}
		if req.GPUUtil != nil {
			values["gpu-memory-utilization"] = strconv.FormatFloat(*req.GPUUtil, 'f', -1, 64)
		}
		if req.MaxLen != nil {
			values["max-model-len"] = strconv.Itoa(*req.MaxLen)
		}
		gpus = len(liveGPUs())
		if _, err := buildVLLMArgs(req.Model, 8000, values, gpus); err != nil {
			fail(err)
			return
		}
	}
	if err := vllm.reserve(req.Action, req.Model); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	switch req.Action {
	case "install":
		go vllm.install()
	case "update":
		go func() { _ = vllm.installOrUpdate(true) }()
	case "start":
		vllm.mu.Lock()
		ctx, cancel := vllm.startCtx, vllm.startCancel
		vllm.mu.Unlock()
		vllm.stop()
		go func() {
			defer cancel()
			err := vllm.start(ctx, req.Model, values, gpus)
			vllm.mu.Lock()
			vllm.job = ""
			vllm.pendingModel = ""
			if err != nil && !errors.Is(err, context.Canceled) {
				vllm.err = err.Error()
			}
			vllm.mu.Unlock()
		}()
	case "stop":
		vllm.stop()
		vllm.mu.Lock()
		vllm.job = ""
		vllm.mu.Unlock()
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// vllmReasoningParser picks vLLM's reasoning parser for known model families.
func vllmReasoningParser(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "qwen3") || strings.Contains(m, "qwq"):
		return "qwen3"
	case strings.Contains(m, "deepseek-r1"):
		return "deepseek_r1"
	case strings.Contains(m, "gpt-oss"):
		return "openai_gptoss"
	case strings.Contains(m, "glm-4.5") || strings.Contains(m, "glm-4.6"):
		return "glm45"
	}
	return ""
}
