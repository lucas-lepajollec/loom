package loom

// Opt-in browser acceptance uses disposable state and a deterministic agent.
// Native account inspection and inference are never delegated to real CLIs.
import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWorkspaceFeaturesBrowser(t *testing.T) {
	if os.Getenv("LOOM_TEST_FEATURES_BROWSER") != "1" {
		t.Skip("disposable browser fixture")
	}
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOOM_PREVIEW_BIND", "127.0.0.1:0")
	t.Setenv("LOOM_COOKIE_SECURE", "0")
	a := fakeACPAdapter(t)
	a.agent.Name = "Synthetic harness"
	if _, err := a.Connect(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	isolateRuntimeRegistry(t, llamaRuntimeAdapter{}, cloudRuntimeAdapter{}, a,
		plannedRuntimeAdapter{RuntimeDescriptor{ID: "gemini", Name: "Gemini CLI", Kind: "harness", CLI: "gemini", Description: "Synthetic disconnected launcher"}})
	_, _ = saveWebPrefs(map[string]string{"lang": "fr", "onboarded": "1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appListener, err := net.Listen("tcp", "127.0.0.1:2598")
	if err != nil {
		t.Fatal(err)
	}
	appMux := http.NewServeMux()
	appMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><title>Synthetic application preview</title><h1>Application de test</h1><p id="asset">Chargement</p><p id="socket">Connexion</p><script src="/asset.js"></script>`)
	})
	appMux.HandleFunc("/asset.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, `document.getElementById('asset').textContent='Asset servi par le proxy'; const ws=new WebSocket(location.origin.replace(/^http/,'ws')+'/hmr'); ws.onopen=()=>ws.send('HMR connecté'); ws.onmessage=e=>document.getElementById('socket').textContent=e.data; ws.onclose=()=>document.getElementById('socket').textContent='Aperçu fermé';`)
	})
	appMux.HandleFunc("/hmr", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if err := ws.Write(ctx, typ, data); err != nil {
				return
			}
		}
	})
	appServer := &http.Server{Handler: appMux, ReadHeaderTimeout: 5 * time.Second}
	defer appServer.Close()
	go appServer.Serve(appListener)
	mux := newWebMux(ctx)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/status":
			sendJSON(w, 200, map[string]any{"hostname": "Loom QA", "active": false, "health": false, "model": "", "version": Version, "boot": "synthetic"})
		case r.URL.Path == "/api/gpu":
			sendJSON(w, 200, []any{})
		case r.URL.Path == "/api/ram":
			sendJSON(w, 200, map[string]any{"total": 16384, "used": 1024})
		case r.URL.Path == "/fixture/shutdown" && r.Method == "POST":
			w.WriteHeader(204)
			go server.Close()
		case strings.HasPrefix(r.URL.Path, "/api/usage/native"):
			sendJSON(w, 200, map[string]any{"ok": true, "runtimes": []any{}})
		case strings.HasSuffix(r.URL.Path, "/inspect") && strings.HasPrefix(r.URL.Path, "/api/runtimes/"):
			sendJSON(w, 200, map[string]any{"ok": true, "account": map[string]any{"status": "unknown"}, "skills": []any{}, "mcp": []any{}})
		default:
			mux.ServeHTTP(w, r)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:2597")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shutdownTerminals)
	t.Log("disposable feature UI http://127.0.0.1:2597/")
	if err := server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
