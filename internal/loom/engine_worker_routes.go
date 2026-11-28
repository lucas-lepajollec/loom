package loom

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// Keep engine management on this machine, even while its vLLM inference link
// is active. The direct-server adapter otherwise projects only served models.
func workerEngineRoute(path string, local http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := currentEngineNode()
		nativeVLLM := n != nil && n.Direct && n.Kind == "vllm"
		var owned *pythonEngine
		if n != nil {
			switch n.Managed {
			case "sglang":
				owned = sglang
			case "mlx":
				owned = mlx
			}
		}
		if path == "/api/models" && (n == nil || nativeVLLM || owned != nil) {
			local(w, r)
			return
		}
		if r.Method == http.MethodPost {
			vllm.mu.Lock()
			vllmBusy := vllm.cmd != nil || vllm.job == "start"
			vllm.mu.Unlock()
			switch path {
			case "/api/stop", "/api/unload":
				if nativeVLLM && n.Managed == "" {
					if err := setEngineNodeIfCurrent(n, nil); err != nil {
						sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
					} else {
						sendJSON(w, 200, map[string]any{"ok": true})
					}
					return
				}
				if owned != nil {
					owned.stop()
					sendJSON(w, 200, map[string]any{"ok": true})
					return
				}
				if nativeVLLM || vllmBusy {
					workerVLLMAction(w, r, "stop", "")
					return
				}
			case "/api/start", "/api/restart":
				if owned != nil {
					workerEngineAction(w, r, owned.serveHTTP, "start", n.Model)
					return
				}
				if nativeVLLM {
					workerVLLMAction(w, r, "start", n.Model)
					return
				}
			case "/api/server":
				if n != nil && n.Direct {
					sendJSON(w, 409, map[string]any{"ok": false, "error": "Use the selected engine’s native parameters."})
					return
				}
			case "/api/load-model", "/api/switch", "/api/apply":
				// Selecting a GGUF uses the shared native llama.cpp path, not vLLM's
				// served-model selector. Stop only this worker's owned vLLM first.
				if owned != nil {
					owned.stop()
				}
				if nativeVLLM || vllmBusy {
					vllm.mu.Lock()
					if vllm.startCancel != nil {
						vllm.startCancel()
					}
					vllm.mu.Unlock()
					vllm.stop()
				}
				if currentEngineNode() == nil {
					local(w, r)
					return
				}
			}
		}
		if path == "/api/service/log" && owned != nil {
			owned.mu.Lock()
			log := strings.Join(owned.log, "\n")
			owned.mu.Unlock()
			sendJSON(w, 200, map[string]any{"ok": true, "log": log})
			return
		}
		if path == "/api/service/log" && nativeVLLM {
			vllm.mu.Lock()
			log := strings.Join(vllm.log, "\n")
			vllm.mu.Unlock()
			sendJSON(w, 200, map[string]any{"ok": true, "log": log})
			return
		}
		if path == "/api/paths" {
			exe, _ := os.Executable()
			p := loomPaths{Home: LoomHome(), Database: dbPath(), Exe: exe, Installed: exe, Presets: presetsDir(), Models: modelsDir(), Backends: backendsDir()}
			sendJSON(w, 200, p)
			return
		}
		if path == "/api/server" && r.Method == http.MethodGet {
			var out map[string]any
			if n != nil && n.Direct {
				out = map[string]any{"ok": true, "running": directEngineHealthy(n), "model": n.Model, "model_name": n.Model, "engine_kind": n.Kind, "engine_direct": true, "engine_managed": n.Managed, "engine_url": n.V1, "np_supported": false, "slots": map[string]any{"ok": false, "items": []any{}}, "recent": []any{}, "stats": serverStats(0, 0)}
			} else {
				captured := &engineResponseBuffer{header: make(http.Header)}
				local(captured, r)
				if err := json.Unmarshal(captured.Bytes(), &out); err != nil {
					sendJSON(w, 500, map[string]any{"error": "invalid engine status"})
					return
				}
			}
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			out["url"], out["url_local"] = scheme+"://"+r.Host+"/v1", scheme+"://"+r.Host+"/v1"
			out["node_managed"], out["key_required"], out["key_set"] = true, true, true
			out["lan"] = !localLoopbackHost(webBound.host)
			sendJSON(w, 200, out)
			return
		}
		nodeAware(path, local)(w, r)
	}
}

func workerVLLMAction(w http.ResponseWriter, r *http.Request, action, model string) {
	workerEngineAction(w, r, handleVLLM, action, model)
}

func workerEngineAction(w http.ResponseWriter, r *http.Request, handler http.HandlerFunc, action, model string) {
	body, _ := json.Marshal(map[string]string{"action": action, "model": model})
	cloned := r.Clone(r.Context())
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	cloned.Header.Set("Content-Type", "application/json")
	cloned.ContentLength = int64(len(body))
	handler(w, cloned)
}

type engineResponseBuffer struct {
	bytes.Buffer
	header http.Header
}

func (b *engineResponseBuffer) Header() http.Header { return b.header }
func (*engineResponseBuffer) WriteHeader(int)       {}

// Queue sweeps use local GGUF/preset handlers. vLLM supports the one-shot
// active-model benchmark, not this llama.cpp model-loading sweep.
func handleNodeBenchQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if n := currentEngineNode(); n != nil && n.Direct {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "Stop vLLM before a GGUF benchmark sweep; use /api/bench for its active model."})
			return
		}
	}
	handleBenchQueue(w, r)
}

func handleNodeExecutionConfig(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if n := currentEngineNode(); n != nil && n.Direct {
		// The previous GGUF's sampling/template settings do not belong to vLLM.
		sendJSON(w, 200, map[string]string{})
		return
	}
	sendJSON(w, 200, publicEngineConfig())
}
