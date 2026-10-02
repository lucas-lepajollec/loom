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
	mu      sync.Mutex
	job     string // "", "install", "start"
	log     []string
	err     string
	cmd     *exec.Cmd
	model   string
	port    int
	started time.Time
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
func vllmRequirements() string {
	if runtime.GOOS == "darwin" {
		return "vLLM n’est pas pris en charge sur macOS (utilise llama.cpp)"
	}
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return "vLLM demande une carte graphique NVIDIA (nvidia-smi introuvable)"
	}
	if _, err := exec.LookPath("uv"); err == nil {
		return ""
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return "Python 3 (ou uv) est nécessaire pour installer vLLM"
	}
	return ""
}

func (v *vllmState) install() {
	defer func() { v.mu.Lock(); v.job = ""; v.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	dir := vllmDir()
	_ = os.MkdirAll(filepath.Dir(dir), 0o755)
	var err error
	if uv, e := exec.LookPath("uv"); e == nil {
		// uv brings a Python version vLLM supports, whatever the system has.
		if err = v.runLogged(ctx, uv, "venv", "--python", "3.12", dir); err == nil {
			err = v.runLogged(ctx, uv, "pip", "install", "--python", filepath.Join(dir, "bin", "python"), "vllm")
		}
	} else {
		if err = v.runLogged(ctx, "python3", "-m", "venv", dir); err == nil {
			err = v.runLogged(ctx, filepath.Join(dir, "bin", "pip"), "install", "vllm")
		}
	}
	v.mu.Lock()
	if err != nil {
		v.err = "installation échouée : " + err.Error()
	} else if !vllmInstalled() {
		v.err = "installation terminée mais la commande vllm est introuvable"
	} else {
		v.err = ""
	}
	v.mu.Unlock()
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
	args := []string{"serve", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	if gpuUtil > 0 && gpuUtil <= 1 {
		args = append(args, "--gpu-memory-utilization", strconv.FormatFloat(gpuUtil, 'f', 2, 64))
	}
	if maxLen > 0 {
		args = append(args, "--max-model-len", strconv.Itoa(maxLen))
	}
	// Reasoning models: let vLLM separate the reasoning from the answer.
	if p := vllmReasoningParser(model); p != "" {
		args = append(args, "--reasoning-parser", p)
	}
	return args
}

func (v *vllmState) start(model string, gpuUtil float64, maxLen int) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	cmd := exec.Command(vllmBin(), vllmArgs(model, port, gpuUtil, maxLen)...)
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
	if err := cmd.Start(); err != nil {
		return err
	}
	v.mu.Lock()
	v.cmd, v.model, v.port, v.started, v.err = cmd, model, port, time.Now(), ""
	v.log = append(v.log, "$ vllm "+strings.Join(vllmArgs(model, port, gpuUtil, maxLen), " "))
	v.mu.Unlock()
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			v.appendLog(sc.Text())
		}
		err := cmd.Wait()
		v.mu.Lock()
		if v.cmd == cmd {
			v.cmd = nil
			if err != nil && v.job == "" {
				v.err = "vLLM s’est arrêté : " + err.Error()
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
			return errors.New("vLLM s’est arrêté pendant le chargement (voir le journal)")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		code, err := directGET(ctx, base, "/health", "", nil)
		cancel()
		if err == nil && code == 200 {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := linkDirectEngine(ctx, base, "", "")
			return err
		}
		time.Sleep(3 * time.Second)
	}
	v.stop()
	return errors.New("vLLM n’a pas répondu en 20 minutes")
}

func (v *vllmState) stop() {
	v.mu.Lock()
	cmd := v.cmd
	v.cmd = nil
	v.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
	// The engine link pointed at this vLLM: back to this machine's llama.cpp.
	if n := currentEngineNode(); n != nil && n.Direct && strings.HasPrefix(n.V1, "http://127.0.0.1:") && n.Kind == "vllm" {
		_ = setEngineNode(nil)
	}
}

// GET: state. POST {action: install|start|stop, model, gpu_memory_utilization, max_model_len}.
func handleVLLM(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		vllm.mu.Lock()
		state := map[string]any{"ok": true, "installed": vllmInstalled(), "missing": vllmRequirements(), "job": vllm.job, "error": vllm.err,
			"running": vllm.cmd != nil, "model": vllm.model, "dir": vllmDir(), "log": strings.Join(vllm.log, "\n")}
		vllm.mu.Unlock()
		sendJSON(w, 200, state)
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Action  string  `json:"action"`
		Model   string  `json:"model"`
		GPUUtil float64 `json:"gpu_memory_utilization"`
		MaxLen  int     `json:"max_model_len"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	vllm.mu.Lock()
	busy := vllm.job != ""
	if !busy && (req.Action == "install" || req.Action == "start") {
		vllm.job, vllm.err = req.Action, ""
		vllm.log = nil
	}
	vllm.mu.Unlock()
	switch req.Action {
	case "install":
		if busy {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "une action vLLM est déjà en cours"})
			return
		}
		if m := vllmRequirements(); m != "" {
			vllm.mu.Lock()
			vllm.job = ""
			vllm.mu.Unlock()
			sendJSON(w, 400, map[string]any{"ok": false, "error": m})
			return
		}
		go vllm.install()
		sendJSON(w, 200, map[string]any{"ok": true})
	case "start":
		if busy {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "une action vLLM est déjà en cours"})
			return
		}
		fail := func(msg string) {
			vllm.mu.Lock()
			vllm.job = ""
			vllm.mu.Unlock()
			sendJSON(w, 400, map[string]any{"ok": false, "error": msg})
		}
		if !vllmInstalled() {
			fail("vLLM n’est pas installé")
			return
		}
		if !vllmModelID.MatchString(req.Model) || strings.Contains(req.Model, "..") {
			fail("identifiant de modèle invalide (ex. Qwen/Qwen3-8B)")
			return
		}
		vllm.stop()
		go func() {
			err := vllm.start(req.Model, req.GPUUtil, req.MaxLen)
			vllm.mu.Lock()
			vllm.job = ""
			if err != nil {
				vllm.err = err.Error()
			}
			vllm.mu.Unlock()
		}()
		sendJSON(w, 200, map[string]any{"ok": true})
	case "stop":
		vllm.stop()
		sendJSON(w, 200, map[string]any{"ok": true})
	default:
		sendJSON(w, 400, map[string]any{"ok": false, "error": "action inconnue"})
	}
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
