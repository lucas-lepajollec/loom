package loom

// Opt-in visual acceptance only: disposable state, synthetic model/caps/help,
// no inference, accounts or production services. Uses the real ParamsEditor.
import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEngineParamsBrowser(t *testing.T) {
	if os.Getenv("LOOM_TEST_ENGINE_PARAMS_BROWSER") != "1" {
		t.Skip("disposable parameter panel fixture")
	}
	home := testHome(t)
	bin := filepath.Join(home, "llama-server")
	help := fakeRouterHelp + `
--fit [on|off]                           fit model into memory
--batch-size N                          logical batch
--ubatch-size N                         physical batch
--threads N                             CPU threads
--threads-batch N                       prompt threads
--n-cpu-moe N                           CPU experts
--top-p N                               top p
--top-k N                               top k
--min-p N                               min p
--repeat-penalty N                      repeat penalty
--presence-penalty N                    presence penalty
--seed N                                seed (default: -1)
`
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat <<'EOF'\n"+help+"EOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SetConfigKey("BIN", bin); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	mux.HandleFunc("/fixture/params", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><html lang="fr" data-theme="dark"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Loom — paramètres synthétiques</title>
<link rel="stylesheet" href="/next/css/app.css"><link rel="stylesheet" href="/next/css/chat.css"><link rel="stylesheet" href="/next/css/pages.css">
<script type="importmap">{"imports":{"preact":"/next/vendor/preact.mjs","preact/hooks":"/next/vendor/preact-hooks.mjs","htm":"/next/vendor/htm.mjs"}}</script></head>
<body><aside class="insp open" style="height:100vh"><div class="insp-in"><div class="insp-scroll" id="fixture"></div></div></aside>
<script type="module">import { html,render } from '/next/js/core/lib.js';
import { ParamsEditor } from '/next/js/features/inspector/params.js';
localStorage.setItem('loom.next.tier',new URLSearchParams(location.search).get('tier')||'essential');
render(html`+"`"+`<${ParamsEditor} src=${{live:true,mode:'model',model:'synthetic.gguf',base:'MODEL=synthetic.gguf\nCTX=8192\nNGL=999\nTEMP=0.7\n'}} />`+"`"+`,document.getElementById('fixture'));</script></body></html>`)
	})
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/engine/params":
			if strings.Contains(r.Header.Get("Referer"), "extra=1") {
				c := readCuratedParams()
				c.Params = append(c.Params, ParamSpec{ID: "seed", Flag: "--seed", Label: "Graine de test", Tier: "advanced", Kind: "number", RequiresFlag: true})
				sendJSON(w, 200, map[string]any{"ok": true, "engine_id": "llama.cpp", "params": mergeEngineParams(c, llamaFlagsPublic(bin))})
			} else {
				mux.ServeHTTP(w, r)
			}
		case "/api/model-caps":
			sendJSON(w, 200, map[string]any{"ok": true, "native_ctx": 32768, "n_layers": 80, "thinks": true, "effort": []string{"low", "high"}, "effort_default": "low", "mmproj": []string{"synthetic-mmproj.gguf"}})
		case "/api/estimate":
			sendJSON(w, 200, map[string]any{"ok": true, "vram_total_mb": 24576, "gpu_mb": 12288, "kv_mb": 2048, "ctx": 8192})
		case "/fixture/shutdown":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			w.WriteHeader(204)
			go server.Close()
		default:
			mux.ServeHTTP(w, r)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:2596")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic ParamsEditor http://127.0.0.1:2596/fixture/params")
	if err := server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
