package ajean

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHubValidRepo(t *testing.T) {
	ok := []string{"unsloth/Qwen2.5-7B-Instruct-GGUF", "TheBloke/Mistral-7B-Instruct-v0.2-GGUF", "ggml-org"}
	for _, id := range ok {
		if !hubValidRepo(id) {
			t.Errorf("attendu valide : %q", id)
		}
	}
	bad := []string{"", "../etc/passwd", "a/../../x", "unsloth/foo/bar", "has space/x", "ok/.."}
	for _, id := range bad {
		if hubValidRepo(id) {
			t.Errorf("attendu invalide : %q", id)
		}
	}
}

func TestHubParseParamsB(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"unsloth/Qwen2.5-7B-Instruct-GGUF", 7},
		{"Llama-3.1-8B-Instruct-Q4_K_M.gguf", 8},
		{"Qwen2.5-0.5B-Instruct", 0.5},
		{"gemma-3-27b-it-GGUF", 27},
		{"tiny-270M-GGUF", 0.27},
		{"Q4_K_M.gguf", 0},
		{"Mixtral-8x7B-Instruct", 7},
	}
	for _, c := range cases {
		if got := hubParseParamsB(c.in); got != c.want {
			t.Errorf("hubParseParamsB(%q)=%v ; attendu %v", c.in, got, c.want)
		}
	}
}

func TestHubParseQuant(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Qwen2.5-7B-Instruct-Q4_K_M.gguf", "Q4_K_M"},
		{"model.Q8_0.gguf", "Q8_0"},
		{"foo-IQ4_XS.gguf", "IQ4_XS"},
		{"bar-f16.gguf", "F16"},
		{"readme.md", ""},
	}
	for _, c := range cases {
		if got := hubParseQuant(c.in); got != c.want {
			t.Errorf("hubParseQuant(%q)=%q ; attendu %q", c.in, got, c.want)
		}
	}
}

func TestHubNeededAndVerdict(t *testing.T) {
	// ~4.7 Go Q4 7B @ 4k → doit tenir dans 16 Go, pas dans 4 Go.
	need := hubNeededMB(int64(4.7*1e9), 7, 4096)
	if need < 5000 || need > 9000 {
		t.Fatalf("estimation 7B Q4 hors fourchette : %.0f Mo", need)
	}
	if hubVerdict(need, 16384, 15000, 32000) != "fits" {
		t.Fatalf("16 Go de carte : attendu fits, got %s", hubVerdict(need, 16384, 15000, 32000))
	}
	if hubVerdict(need, 16384, 1000, 32000) != "fits" {
		t.Fatalf("16 Go occupés : attendu fits (capacité totale, pas le libre)")
	}
	if hubVerdict(need, 4096, 4000, 32000) != "offload" {
		t.Fatalf("4 Go VRAM : attendu offload")
	}
	if hubVerdict(15000, 16384, 1000, 32000) != "tight" {
		t.Fatalf("presque plein de la carte : attendu tight")
	}
	if hubVerdict(80000, 8192, 8000, 16000) != "no" {
		t.Fatalf("trop lourd : attendu no")
	}
}

func TestHubGroupGGUF(t *testing.T) {
	files, vision := hubGroupGGUF([]hfSibling{
		{Rfilename: "README.md", Size: 10},
		{Rfilename: "m-00001-of-00002.gguf", Size: 100},
		{Rfilename: "m-00002-of-00002.gguf", Size: 80},
		{Rfilename: "m-Q4_K_M.gguf", Size: 50},
		{Rfilename: "mmproj-F16.gguf", Size: 600},
	}, "org/mod")
	if len(files) != 2 {
		t.Fatalf("fichiers poids = %d ; attendu 2", len(files))
	}
	var shard hubFile
	for _, f := range files {
		if f.Parts == 2 {
			shard = f
		}
	}
	if shard.Size != 180 {
		t.Fatalf("taille famille = %d ; attendu 180", shard.Size)
	}
	if len(vision) != 1 || !vision[0].Vision {
		t.Fatalf("projecteur vision mal classé : %+v", vision)
	}
}

func TestHandleHubSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "gguf" {
			t.Errorf("filter=%q", r.URL.Query().Get("filter"))
		}
		if r.URL.Query().Get("search") != "qwen" {
			t.Errorf("search=%q", r.URL.Query().Get("search"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "unsloth/Qwen2.5-7B-Instruct-GGUF", "author": "unsloth", "downloads": 10, "likes": 2, "lastModified": "2026-01-01T00:00:00.000Z", "tags": []string{"gguf"}},
			{"id": "../evil", "downloads": 1},
		})
	}))
	defer srv.Close()
	old := hubAPIBase
	hubAPIBase = srv.URL
	defer func() { hubAPIBase = old }()

	req := httptest.NewRequest(http.MethodGet, "/api/hub/search?q=qwen", nil)
	rec := httptest.NewRecorder()
	handleHubSearch(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK     bool             `json:"ok"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Models) != 1 {
		t.Fatalf("models = %+v", out)
	}
	if out.Models[0]["id"] != "unsloth/Qwen2.5-7B-Instruct-GGUF" {
		t.Fatalf("id = %v", out.Models[0]["id"])
	}
}

func TestHubPrepareReadme(t *testing.T) {
	in := "---\nlicense: apache-2.0\n---\n# Model Card for Demo\n\n![x](pic.png)\nSee [doc](https://example.com)\n"
	got := hubPrepareReadme(in, "https://huggingface.co/org/mod/resolve/main/")
	if strings.Contains(got, "license:") || strings.Contains(got, "Model Card for") {
		t.Fatalf("frontmatter/chrome encore là : %q", got)
	}
	if !strings.Contains(got, "https://huggingface.co/org/mod/resolve/main/pic.png") {
		t.Fatalf("image relative non réécrite : %q", got)
	}
	if !strings.Contains(got, "https://example.com") {
		t.Fatalf("lien absolu perdu : %q", got)
	}
}

func TestHandleHubModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "README") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("---\nlicense: mit\n---\n# Model Card\n\nBonjour **7B**."))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "unsloth/Demo-7B-GGUF",
			"author":   "unsloth",
			"siblings": []map[string]any{{"rfilename": "Demo-7B-Q4_K_M.gguf", "size": 4700000000}},
			"cardData": map[string]any{"license": "apache-2.0"},
		})
	}))
	defer srv.Close()
	oldAPI, oldWeb := hubAPIBase, hubWebBase
	hubAPIBase, hubWebBase = srv.URL, srv.URL
	defer func() { hubAPIBase, hubWebBase = oldAPI, oldWeb }()

	req := httptest.NewRequest(http.MethodGet, "/api/hub/model?id=unsloth/Demo-7B-GGUF", nil)
	rec := httptest.NewRecorder()
	handleHubModel(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("%v", out)
	}
	files, _ := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", out["files"])
	}
	if out["params_b"] != 7.0 {
		t.Fatalf("params_b = %v", out["params_b"])
	}
	if !strings.Contains(fmt.Sprint(out["readme"]), "Bonjour") {
		t.Fatalf("readme = %v", out["readme"])
	}
}

func TestHandleHubModelRejectsTraversal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/hub/model?id=../secret", nil)
	rec := httptest.NewRecorder()
	handleHubModel(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandleHubAvatar(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/organizations/unsloth/avatar":
			_ = json.NewEncoder(w).Encode(map[string]string{"avatarUrl": srv.URL + "/img.png"})
		case "/img.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := hubWebBase
	hubWebBase = srv.URL
	defer func() { hubWebBase = old }()

	req := httptest.NewRequest(http.MethodGet, "/api/hub/avatar?a=unsloth", nil)
	rec := httptest.NewRecorder()
	handleHubAvatar(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != "PNG" {
		t.Fatalf("avatar = %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodGet, "/api/hub/avatar?a=unsloth/foo", nil)
	badRec := httptest.NewRecorder()
	handleHubAvatar(badRec, bad)
	if badRec.Code != 404 {
		t.Fatalf("slash author status %d", badRec.Code)
	}
}
