package loom

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPWAPublicAssetsMatchManifest(t *testing.T) {
	page, _ := nextFS.ReadFile("ui/next/index.html")
	if strings.Count(string(page), `rel="apple-touch-icon"`) != 1 || !bytes.Contains(page, []byte(`href="/icons/loom-180.png"`)) || !bytes.Contains(page, []byte(`rel="icon"`)) {
		t.Fatal("missing or duplicate brand icon links")
	}
	mux := http.NewServeMux()
	registerPWAAssets(mux)
	for _, size := range []int{180, 192, 512} {
		b := loomIcon(size)
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width != size || cfg.Height != size {
			t.Fatal("invalid icon", size, err)
		}
	}
	manifest, _ := uiFS.ReadFile("ui/manifest.webmanifest")
	var parsed struct {
		ID       string
		StartURL string `json:"start_url"`
		Icons    []struct {
			Src     string
			Sizes   string
			Purpose string
		}
	}
	if json.Unmarshal(manifest, &parsed) != nil || parsed.ID != "/" || parsed.StartURL != "/#chat" || len(parsed.Icons) != 3 {
		t.Fatal("invalid manifest")
	}
	for _, icon := range parsed.Icons {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", icon.Src, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatal("missing manifest icon", icon.Src)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/offline.html", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Aucune conversation") || strings.Contains(w.Body.String(), "<script") {
		t.Fatal("invalid offline fallback")
	}
}
