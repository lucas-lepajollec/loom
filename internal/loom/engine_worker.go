package loom

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func isEngineWorker() bool { return os.Getenv("LOOM_UI_SERVICE") == "loom-node" }

const defaultNodeListen = "127.0.0.1:2511"

// node runs before dotenv, owner adoption and ACP shutdown registration. Its
// data root deliberately ignores LOOM_HOME and /etc/default/loom.
func cmdNode(args []string) error {
	if runtime.GOOS != "linux" {
		return errors.New("loom node currently supports Linux; other platforms can link an inference server directly")
	}
	if os.Geteuid() == 0 {
		return errors.New("run loom node as the engine user, without sudo")
	}
	if len(args) == 0 {
		return errors.New("usage: loom node init|serve|install|update|capabilities [--home DIR] [--listen HOST:PORT] [--bin PATH] [--models DIR]")
	}
	action := args[0]
	if action == "capabilities" && len(args) == 1 {
		fmt.Println("engine-node-v1")
		return nil
	}
	flags := flag.NewFlagSet("loom node", flag.ContinueOnError)
	home := flags.String("home", defaultLoomHome()+"-node", "separate engine data root")
	listen := flags.String("listen", defaultNodeListen, "control and inference listener")
	bin := flags.String("bin", "", "existing llama-server binary (init only)")
	check := flags.Bool("check", false, "check node update without installing")
	models := flags.String("models", "", "existing model directory (init only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected node arguments")
	}
	if action != "init" && action != "serve" && action != "install" && action != "update" {
		return errors.New("unknown node action")
	}
	if action != "update" && *check {
		return errors.New("--check applies only to node update")
	}
	if action != "init" && (*bin != "" || *models != "") {
		return errors.New("--bin and --models apply only to node init")
	}
	abs, err := filepath.Abs(*home)
	if err != nil {
		return err
	}
	if abs == filepath.Clean(defaultLoomHome()) || abs == filepath.Clean(readEtcDefault()) {
		return errors.New("node data must be separate from the main Loom installation")
	}
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("invalid node listener: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("invalid node port")
	}
	if err := os.Setenv("LOOM_HOME", abs); err != nil {
		return err
	}
	_ = os.Setenv("LOOM_SERVICE", "loom-node-engine")
	_ = os.Setenv("LOOM_UI_SERVICE", "loom-node")
	explicitListen := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "listen" {
			explicitListen = true
		}
	})
	if !explicitListen {
		if saved := getStr(bkState, "node_listener"); saved != "" {
			*listen = saved
		}
	}
	switch action {
	case "init":
		return initEngineWorker(*bin, *models)
	case "install":
		return installEngineWorker(abs, *listen)
	case "update":
		if *check {
			return cmdUpdate([]string{"--check"})
		}
		return cmdUpdate(nil)
	default:
		return serveEngineWorker(*listen)
	}
}

func nodeTokenPath() string { return filepath.Join(LoomHome(), "node.token") }
func readNodeToken() (string, error) {
	st, err := os.Lstat(nodeTokenPath())
	if err != nil {
		return "", fmt.Errorf("node credential unavailable: run loom node init: %w", err)
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return "", errors.New("node credential must be a regular file with mode 0600")
	}
	b, err := os.ReadFile(nodeTokenPath())
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid node credential")
	}
	return token, nil
}
func newNodeToken() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

func initEngineWorker(bin, models string) error {
	// Engine-only provisioning: no conversation, workspace, memory or skills dirs.
	if err := os.MkdirAll(LoomHome(), 0700); err != nil {
		return err
	}
	marker := filepath.Join(LoomHome(), "node.mode")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		if len(ReadConfig()) != 0 {
			return errors.New("existing Loom data: choose a new node home instead of migrating in place")
		}
		if err := os.WriteFile(marker, []byte("engine-node-v1\n"), 0600); err != nil {
			return err
		}
	}
	for _, d := range []string{backendsDir(), binDir(), presetsDir(), modelsDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	if len(ReadConfig()) == 0 {
		cfg := defaultConfig()
		cfg["PORT"] = "2512"
		cfg["HOST"] = "127.0.0.1"
		if err := WriteConfig(cfg); err != nil {
			return err
		}
	}
	if bin != "" {
		bin, err := filepath.Abs(bin)
		if err != nil {
			return err
		}
		st, err := os.Stat(bin)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			return errors.New("--bin must name an executable llama-server")
		}
		if err := SetConfigKey("BIN", bin); err != nil {
			return err
		}
	}
	if models != "" {
		abs, err := filepath.Abs(models)
		if err != nil {
			return err
		}
		if !isDir(abs) {
			return errors.New("--models must name an existing directory")
		}
		if err := saveExtraModelDirs(append(extraModelDirs(), abs)); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(nodeTokenPath()); os.IsNotExist(err) {
		token, err := newNodeToken()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(nodeTokenPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, token+"\n")
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if _, err := readNodeToken(); err != nil {
		return err
	}
	if readAPIKey() == "" {
		key, err := newNodeToken()
		if err != nil {
			return err
		}
		if err := writeAPIKey(key); err != nil {
			return err
		}
	}
	fmt.Printf("Loom node initialized in %s. Machine credential: %s (keep private).\n", LoomHome(), nodeTokenPath())
	return nil
}

func nodeAuth(hash string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Never accept browser cookies, human passwords or credentials in URLs.
		if hash == "" || !checkBearer(r, hash) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="loom-node"`)
			sendJSON(w, 401, map[string]any{"ok": false, "error": "node credential required"})
			return
		}
		if r.Header.Get("Origin") != "" {
			sendJSON(w, 403, map[string]any{"ok": false, "error": "node accepts machine requests only"})
			return
		}
		next(w, r)
	}
}

func newEngineWorkerMux(token string) *http.ServeMux {
	mux := http.NewServeMux()
	hash := ""
	if token != "" {
		hash = hashWebKey(token)
	}
	api := func(path string, h http.HandlerFunc) {
		if controlActionRequiresPost(path) {
			next := h
			h = func(w http.ResponseWriter, r *http.Request) {
				if workspaceMethod(w, r, http.MethodPost) {
					next(w, r)
				}
			}
		}
		mux.HandleFunc(path, nodeAuth(hash, controlRequests(path, h)))
	}
	registerEngineControlRoutes(func(path string, h http.HandlerFunc) {
		api(path, workerEngineRoute(path, h))
	})
	api("/api/ping", handlePing)
	api("/api/update", handleUpdateCheck)
	api("/api/update/apply", handleUpdateApply)
	api("/api/startup", handleStartup)
	api("/api/network", handleNodeNetwork)
	api("/api/engine/execution-config", handleNodeExecutionConfig)
	api("/api/bench", handleBench)
	api("/api/bench/last", handleBenchLast)
	api("/api/bench/tests", handleBenchTests)
	api("/api/bench/tests/delete", handleBenchTestsDelete)
	api("/api/bench/queue", handleNodeBenchQueue)
	api("/api/bench/queue/cancel", handleBenchQueueCancel)
	api("/api/bench/runs", handleBenchRuns)
	api("/api/node/info", func(w http.ResponseWriter, r *http.Request) {
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		host, _ := os.Hostname()
		sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "version": Version, "role": "engine-node", "engine": true, "v1_exposed": true, "v1_same_origin": true, "api_key": readAPIKey(), "capabilities": []string{"llama.cpp", "vllm", "models", "presets", "downloads", "engine-updates", "node-updates", "startup", "local-benchmarks"}})
	})
	// A node's inference credential is independent from its management token.
	inference := func(w http.ResponseWriter, r *http.Request) {
		if readAPIKey() == "" {
			sendJSON(w, 503, map[string]any{"ok": false, "error": "node inference credential unavailable"})
			return
		}
		if _, ok := oaiKeyOK(r); !ok {
			oaiError(w, 401, "invalid_request_error", "Invalid API key", "", "invalid_api_key")
			return
		}
		serveNodeInference(w, r)
	}
	mux.HandleFunc("/v1/", inference)
	for _, path := range []string{"/health", "/props", "/slots", "/metrics"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !workspaceMethod(w, r, http.MethodGet) {
				return
			}
			inference(w, r)
		})
	}
	return mux
}

func serveNodeInference(w http.ResponseWriter, r *http.Request) {
	if n := currentEngineNode(); n == nil && r.Method == http.MethodGet && (r.URL.Path == "/v1/models" || r.URL.Path == "/v1/models/") {
		oaiWriteJSON(w, 200, oaiModelsJSON())
		return
	}
	// Loom's alias names the selected model. vLLM expects its native ID;
	// leave every other request field and explicit model ID intact.
	if n := currentEngineNode(); n != nil && n.Direct && r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/") {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
		if err != nil {
			sendJSON(w, 413, map[string]any{"error": "inference request too large"})
			return
		}
		var request map[string]json.RawMessage
		if json.Unmarshal(body, &request) == nil {
			var model string
			_ = json.Unmarshal(request["model"], &model)
			if model == "loom" {
				request["model"], _ = json.Marshal(n.Model)
				body, _ = json.Marshal(request)
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	target := engineBase()
	key := engineAPIKey()
	u, err := url.Parse(target)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "invalid local engine"})
		return
	}
	// vLLM creates a local direct link. A worker may never forward to a remote
	// control plane or use its machine token as an upstream inference credential.
	if !localLoopbackHost(u.Hostname()) {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "node requires a loopback engine"})
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = u.Host
		// Le front llama.cpp résout la clé cliente et compte sa propre requête.
		// Un moteur direct, lui, ne connaît que sa credential native.
		if currentEngineNode() != nil {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Del("Cookie")
		req.Header.Del("Origin")
	}
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "engine unavailable; start an engine or load a model"})
	}
	proxy.ServeHTTP(w, r)
}
func localLoopbackHost(host string) bool {
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func serveEngineWorker(addr string) error {
	token, err := readNodeToken()
	if err != nil {
		return err
	}
	if readAPIKey() == "" {
		return errors.New("missing node inference credential: run loom node init")
	}
	if n := currentEngineNode(); n != nil && !n.Direct {
		return errors.New("a node cannot link another control plane")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	host, port, _ := net.SplitHostPort(addr)
	webBound.host, webBound.port = host, func() int { n, _ := strconv.Atoi(port); return n }()
	cleanStalePartFiles()
	lcRestoreOnce.Do(lcRestore)
	go engineAutoLoop()
	go vllmAutoLoop()
	// No newWebMux, Brain, MCP prewarm, provider keyring or harness probes/loops.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go startConfiguredEngine(ctx)
	srv := &http.Server{Handler: newEngineWorkerMux(token), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	defer func() {
		vllm.mu.Lock()
		if vllm.startCancel != nil {
			vllm.startCancel()
		}
		vllm.mu.Unlock()
		_ = serviceAction("stop")
		vllm.stop()
	}()
	fmt.Printf("[loom node] %s (engine API only)\n", addr)
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Model presets cannot relocate or expose a node's private inference front.
// Its public listener belongs to node serve; keep the native router reachable
// across model changes and unloads instead of falling back to the main port.
func preserveNodeFront(next, current map[string]string) {
	if !isEngineWorker() {
		return
	}
	next["HOST"] = hostLocalOnly
	next["PORT"] = current["PORT"]
	if next["PORT"] == "" {
		next["PORT"] = "2512"
	}
}
