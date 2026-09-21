package ajean

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNakedRememberGETReturnsContent(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\n")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := rememberNakedFromContent("qwen.gguf", "MODEL=qwen.gguf\nCTX=4096\nNGL=40\n"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handleNakedRemember(rec, httptest.NewRequest("GET", "/api/naked/remember?model=qwen.gguf", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK         bool   `json:"ok"`
		Remembered bool   `json:"remembered"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.Remembered {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Content, "CTX=4096") || !strings.Contains(out.Content, "NGL=40") {
		t.Fatalf("content=%q", out.Content)
	}
}

func TestNakedRememberGETEmpty(t *testing.T) {
	testHome(t)
	rec := httptest.NewRecorder()
	handleNakedRemember(rec, httptest.NewRequest("GET", "/api/naked/remember?model=absent.gguf", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK         bool   `json:"ok"`
		Remembered bool   `json:"remembered"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Remembered || out.Content != "" {
		t.Fatalf("%+v", out)
	}
}
