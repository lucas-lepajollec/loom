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
	"time"
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
		if r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials/origin forwarded to engine")
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
	req.Header.Set("Origin", "http://localhost:2510")
	req.Header.Set("Cookie", "loom_session=browser-fixture")
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
	if _, _, err := cleanNodeURL("http://example.com:2510"); err == nil {
		t.Fatal("http public accepté")
	}
	if _, _, err := cleanNodeURL("http://user:pass@192.168.1.2:2510"); err == nil {
		t.Fatal("identifiants dans l'URL acceptés")
	}
}

func TestEngineNodeObservationDeadlineDoesNotLimitMutations(t *testing.T) {
	observations := make(chan bool, 2)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Observe the client's imposed deadline through cancellation, using a short
		// caller deadline for the test. The endpoint exits as soon as it is canceled.
		if r.Method == http.MethodGet {
			<-r.Context().Done()
			observations <- true
			return
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer remote.Close()
	n := &engineNode{URL: remote.URL, Hostname: "test engine"}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/status", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	proxyEngineNode(rec, r, n)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	select {
	case <-observations:
	case <-time.After(time.Second):
		t.Fatal("remote observation was not canceled")
	}
	rec = httptest.NewRecorder()
	proxyEngineNode(rec, httptest.NewRequest(http.MethodPost, "/api/load-model", strings.NewReader(`{}`)), n)
	if rec.Code != http.StatusOK {
		t.Fatalf("mutation status %d", rec.Code)
	}
}

func TestEngineNodeFastObservationGetsOwnDeadline(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if r.Method == http.MethodGet {
			if !ok || time.Until(deadline) > 6*time.Second || time.Until(deadline) < 5*time.Second {
				t.Error("fast observation must have a six-second deadline")
			}
		} else if ok {
			t.Error("proxy imposed an observation deadline on a mutation")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})
	n := &engineNode{URL: "http://127.0.0.1:1", Hostname: "fixture"}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		proxyEngineNode(httptest.NewRecorder(), httptest.NewRequest(method, "/api/status", nil), n)
	}
}
