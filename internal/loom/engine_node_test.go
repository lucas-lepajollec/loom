package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// fakeNode is a remote Loom: control API protected by "web", /v1 by "v1key".
func fakeNode(t *testing.T, exposed bool) (*httptest.Server, *httptest.Server) {
	v1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer v1key" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, `{"object":"list","data":[{"id":"Distant.gguf"}]}`)
	}))
	port, _ := strconv.Atoi(strings.Split(strings.TrimPrefix(v1.URL, "http://127.0.0.1:"), "/")[0])
	ctl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer web" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/node/info":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "hostname": "tour", "version": "9", "llm_port": port, "v1_exposed": exposed, "api_key": "v1key", "engine": true})
		case "/api/status":
			io.WriteString(w, `{"hostname":"tour","health":true}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(func() { v1.Close(); ctl.Close() })
	return ctl, v1
}

func TestEngineNodeLinkAndForward(t *testing.T) {
	testHome(t)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	ctl, _ := fakeNode(t, true)
	if _, err := linkEngineNode(context.Background(), ctl.URL, "mauvaise"); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("mauvaise clé: %v", err)
	}
	n, err := linkEngineNode(context.Background(), ctl.URL, "web")
	if err != nil || n.Hostname != "tour" {
		t.Fatalf("%v %+v", err, n)
	}
	u, _ := url.Parse(ctl.URL)
	if !strings.HasPrefix(engineBase(), "http://"+u.Hostname()+":") || engineAPIKey() != "v1key" {
		t.Fatalf("base %s", engineBase())
	}
	if got := loomLocalModels(); len(got) != 1 || got[0] != "Distant.gguf" {
		t.Fatalf("modèles distants: %v", got)
	}
	// An engine route goes to the node with its key, whatever the browser sent.
	h := nodeAware("/api/status", func(w http.ResponseWriter, r *http.Request) { t.Fatal("handler local appelé") })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer cle-du-navigateur")
	h(rec, req)
	if !strings.Contains(rec.Body.String(), `"hostname":"tour"`) {
		t.Fatalf("proxy: %d %s", rec.Code, rec.Body.String())
	}
	// Other routes stay here.
	called := false
	nodeAware("/api/chat/send", func(http.ResponseWriter, *http.Request) { called = true })(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/chat/send", nil))
	if !called {
		t.Fatal("les discussions doivent rester sur cette machine")
	}
	_ = setEngineNode(nil)
	if currentEngineNode() != nil || !strings.HasPrefix(engineBase(), "http://127.0.0.1:") {
		t.Fatal("retour au moteur local")
	}
}

func TestEngineNodeRefusesUnexposedV1AndPublicHTTP(t *testing.T) {
	testHome(t)
	ctl, _ := fakeNode(t, false)
	if _, err := linkEngineNode(context.Background(), ctl.URL, "web"); err == nil || !strings.Contains(err.Error(), "API /v1") {
		t.Fatalf("v1 fermée: %v", err)
	}
	if _, _, err := cleanNodeURL("http://example.com:8091"); err == nil {
		t.Fatal("http public accepté")
	}
	if _, _, err := cleanNodeURL("http://user:pass@192.168.1.2:8091"); err == nil {
		t.Fatal("identifiants dans l'URL acceptés")
	}
}
