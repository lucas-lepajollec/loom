package loom

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

// SGLang and MLX each own a Python environment and a loopback child server.
type pythonEngine struct {
	id, label           string
	mu                  sync.Mutex
	job, model, failure string
	servingModel        string
	log                 []string
	process             *exec.Cmd
	done                chan struct{}
	cancel              context.CancelFunc
	port                int
}

var (
	sglang = &pythonEngine{id: "sglang", label: "SGLang"}
	mlx    = &pythonEngine{id: "mlx", label: "MLX"}
)

func (e *pythonEngine) dir() string    { return filepath.Join(LoomHome(), "engines", e.id) }
func (e *pythonEngine) python() string { return filepath.Join(e.dir(), "bin", "python") }

func (e *pythonEngine) installPackage(rocm bool) (requirement, python string) {
	if e.id == "mlx" {
		return "mlx-lm", "3.12"
	}
	if rocm {
		return "amd-sglang[srt_hip,rocm720]", "3.10"
	}
	return "sglang", "3.12"
}

// Read installed package metadata without spawning Python or initializing GPU libraries.
func pythonPackageVersion(dir string, packages ...string) string {
	for _, pkg := range packages {
		prefix := strings.ReplaceAll(pkg, "-", "_") + "-"
		paths, _ := filepath.Glob(filepath.Join(dir, "lib", "python*", "site-packages", prefix+"*.dist-info"))
		for _, p := range paths {
			if regularFile(filepath.Join(p, "METADATA")) {
				return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), prefix), ".dist-info")
			}
		}
	}
	return ""
}

func (e *pythonEngine) version() string {
	if !regularFile(e.python()) {
		return ""
	}
	if e.id == "mlx" {
		return pythonPackageVersion(e.dir(), "mlx-lm")
	}
	return pythonPackageVersion(e.dir(), "sglang", "amd-sglang")
}

func (e *pythonEngine) requirements() string {
	item := catalogEntry{ID: e.id, Installed: e.version() != ""}
	host := catalogHost{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if e.id == "sglang" {
		host = detectCatalogHost()
	}
	catalogCompatibility(&item, host, vllmHasTool("python3"), vllmHasTool("uv"), vllmHasTool("python3.10"))
	return item.RequirementNote
}

func (e *pythonEngine) appendLog(line string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, line)
	if len(e.log) > 400 {
		e.log = e.log[len(e.log)-400:]
	}
}

func (e *pythonEngine) install(ctx context.Context, update bool) error {
	dir := e.dir()
	if err := requireInstallWritable(dir, filepath.Join(dir, "bin"), filepath.Join(dir, "lib")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	rocm := e.id == "sglang" && detectCatalogHost().primaryGPU() == "ROCm"
	pkg, pyVersion := e.installPackage(rocm)
	create := !update && !regularFile(e.python())
	upgrade := []string{}
	if update {
		upgrade = append(upgrade, "--upgrade")
	}
	if uv, err := lifecycleLookPath("uv"); err == nil {
		if create {
			if err := runEngineLogged(ctx, e.appendLog, uv, "venv", "--python", pyVersion, dir); err != nil {
				return err
			}
		}
		args := []string{"pip", "install", "--python", e.python()}
		if e.id == "sglang" && !rocm {
			args = append(args, "--prerelease=allow")
		}
		if err := runEngineLogged(ctx, e.appendLog, uv, append(append(args, upgrade...), pkg)...); err != nil {
			return err
		}
	} else {
		python := "python3"
		if rocm {
			python = "python3.10"
		}
		if create {
			if err := runEngineLogged(ctx, e.appendLog, python, "-m", "venv", dir); err != nil {
				return err
			}
		}
		args := []string{"-m", "pip", "install"}
		if err := runEngineLogged(ctx, e.appendLog, e.python(), append(append(args, upgrade...), pkg)...); err != nil {
			return err
		}
	}
	if e.version() == "" {
		return fmt.Errorf("%s package metadata not found after installation", e.label)
	}
	return nil
}

func (e *pythonEngine) args(model string, port int, values map[string]string, gpus int) ([]string, error) {
	if e.id == "sglang" {
		return buildSGLangArgs(model, port, values, gpus)
	}
	if !validVLLMModel(model) || len(values) != 0 {
		return nil, errors.New("invalid MLX model or parameters")
	}
	return []string{"-m", "mlx_lm.server", "--model", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}, nil
}

func ownedEngineBase(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

func clearManagedEngineLink(kind string, port int) {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	if n := currentEngineNodeLocked(); n != nil && n.Managed == kind && n.V1 == ownedEngineBase(port) {
		// Its active selection was already removed from persistent storage.
		cacheEngineNodeLocked(nil)
	}
}

type pythonEngineLog struct {
	engine  *pythonEngine
	pending string
}

func (l *pythonEngineLog) Write(data []byte) (int, error) {
	lines := strings.Split(l.pending+string(data), "\n")
	for _, line := range lines[:len(lines)-1] {
		l.engine.appendLog(line)
	}
	l.pending = lines[len(lines)-1]
	if len(l.pending) >= 64<<10 {
		l.engine.appendLog(l.pending)
		l.pending = ""
	}
	return len(data), nil
}

func (e *pythonEngine) start(ctx context.Context, previous *engineNode, model string, values map[string]string, gpus int) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	args, err := e.args(model, port, values, gpus)
	if err != nil {
		return err
	}
	cmd := exec.Command(e.python(), args...)
	cmd.Env = append(os.Environ(), "HF_HUB_CACHE="+vllmHFCache(), "PYTHONUNBUFFERED=1", "PATH="+filepath.Dir(e.python())+string(os.PathListSeparator)+lifecycleLocalPath())
	log := &pythonEngineLog{engine: e}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = time.Second
	ownedProcessGroup(cmd)
	e.appendLog("$ python " + strings.Join(args, " "))
	e.mu.Lock()
	if err := ctx.Err(); err != nil {
		e.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		e.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	e.process, e.done, e.port, e.servingModel = cmd, done, port, model
	e.mu.Unlock()
	go func() {
		err := cmd.Wait()
		if log.pending != "" {
			e.appendLog(log.pending)
		}
		e.mu.Lock()
		if e.process == cmd {
			e.process, e.servingModel = nil, ""
			if err != nil && e.job == "" {
				e.failure = e.label + " stopped: " + err.Error()
			}
		}
		e.mu.Unlock()
		clearManagedEngineLink(e.id, port)
		close(done)
	}()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	base := ownedEngineBase(port)
	for {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		var n *engineNode
		code, err := directGET(probe, base, "/health", "", nil)
		if err == nil && (code == http.StatusOK || code == http.StatusNotFound || code == http.StatusMethodNotAllowed) {
			n, err = prepareDirectEngine(probe, base, "", "")
		}
		cancel()
		if err == nil && n != nil {
			n.Kind, n.Managed = e.id, e.id
			if n.Ctx == 0 && values["context-length"] != "" {
				n.Ctx, _ = strconv.Atoi(values["context-length"])
			}
			e.mu.Lock()
			if e.process == cmd && ctx.Err() == nil {
				err = setEngineNodeIfCurrent(previous, n)
			} else {
				err = errors.New(e.label + " stopped during loading")
			}
			e.mu.Unlock()
			if err == nil {
				return nil
			}
			e.stopProcess()
			return err
		}
		select {
		case <-ctx.Done():
			e.stopProcess()
			return ctx.Err()
		case <-done:
			return errors.New(e.label + " stopped during loading (see the log)")
		case <-ticker.C:
		}
	}
}

func (e *pythonEngine) stopProcess() {
	e.mu.Lock()
	cmd, done := e.process, e.done
	e.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		killOwnedProcessGroup(cmd)
		<-done
	}
}

func (e *pythonEngine) stop() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
	e.stopProcess()
}

func (e *pythonEngine) reserve(action, model string) (context.Context, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.job != "" || e.process != nil && (action == "install" || action == "update") {
		return nil, errors.New(e.label + " is running or an action is already in progress")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.job, e.failure = action, ""
	if action == "start" {
		e.model = model
	}
	if action != "stop" {
		e.log = nil
	}
	return ctx, nil
}

func (e *pythonEngine) complete(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancel()
	e.cancel, e.job = nil, ""
	if err != nil && !errors.Is(err, context.Canceled) {
		e.failure = err.Error()
	}
}

func (e *pythonEngine) cancelStart() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.job != "start" {
		return false
	}
	e.cancel()
	return true
}

func handleSGLang(w http.ResponseWriter, r *http.Request) { sglang.serveHTTP(w, r) }
func handleMLX(w http.ResponseWriter, r *http.Request)    { mlx.serveHTTP(w, r) }

func (e *pythonEngine) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		version, missing := e.version(), e.requirements()
		e.mu.Lock()
		state := map[string]any{"ok": true, "installed": version != "", "version": version, "requirement_note": missing, "job": e.job, "error": e.failure,
			"running": e.process != nil, "port": e.port, "model": e.model, "dir": e.dir(), "log": strings.Join(e.log, "\n")}
		e.mu.Unlock()
		sendJSON(w, 200, state)
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	fail := func(code int, err error) { sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()}) }
	var req struct {
		Action string
		Model  string
		Params map[string]string
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Action == "stop" && e.cancelStart() {
		e.stopProcess()
		sendJSON(w, 200, map[string]any{"ok": true})
		return
	}
	var values map[string]string
	var gpus int
	switch req.Action {
	case "install", "update", "start":
		if missing := e.requirements(); missing != "" {
			fail(400, errors.New(catalogRequirementText(missing)))
			return
		}
		if req.Action != "install" && e.version() == "" {
			fail(400, errors.New(e.label+" is not installed"))
			return
		}
		if req.Action == "start" {
			if !validVLLMModel(req.Model) {
				fail(400, errors.New("invalid model"))
				return
			}
			if e.id == "sglang" {
				gpus = len(liveGPUs())
				var err error
				values, err = loadPythonEngineParams(e.id, req.Model)
				if err != nil {
					fail(500, err)
					return
				}
			} else {
				values = map[string]string{}
			}
			for k, v := range req.Params {
				values[k] = v
			}
			if _, err := e.args(req.Model, 1, values, gpus); err != nil {
				fail(400, err)
				return
			}
		} else {
			if err := workspaceSessions.authorizePolicy(r.Context(), policy.Input{Subject: "node." + req.Action, MachineID: "local", Fallback: policy.Allow}, false); err != nil {
				fail(403, err)
				return
			}
		}
	case "stop":
	default:
		fail(400, errors.New("unknown action"))
		return
	}
	ctx, err := e.reserve(req.Action, req.Model)
	if err != nil {
		fail(409, err)
		return
	}
	switch req.Action {
	case "install", "update":
		go func() {
			ctx, cancel := context.WithTimeout(ctx, time.Hour)
			defer cancel()
			e.complete(e.install(ctx, req.Action == "update"))
		}()
	case "start":
		previous := currentEngineNode()
		go func() {
			e.stopProcess()
			if previous != nil && previous.Managed == e.id {
				previous = nil
			}
			e.complete(e.start(ctx, previous, req.Model, values, gpus))
		}()
	case "stop":
		e.stopProcess()
		e.complete(nil)
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func stopManagedDirect(n *engineNode) bool {
	switch n.Managed {
	case "sglang":
		sglang.stop()
	case "mlx":
		mlx.stop()
	case "vllm":
		vllm.cancelStartAndStop()
	default:
		return false
	}
	return true
}
