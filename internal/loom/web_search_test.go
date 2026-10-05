package loom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchSettingsDefaultPersistenceAndInvalidUpdate(t *testing.T) {
	testHome(t)
	request := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/internet/search", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handleSearchSettings(w, r)
		return w
	}
	if w := request(http.MethodGet, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"duckduckgo"`) {
		t.Fatal("fresh install search unavailable")
	}
	w := request(http.MethodPost, `{"provider":"searxng","url":"http://127.0.0.1:8080","key":"synthetic-secret-only"}`)
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("synthetic-secret-only")) {
		t.Fatal("credential leak or save failed")
	}
	settings := readSearchSettings()
	if settings.URL != "http://127.0.0.1:8080/search" {
		t.Fatal("base URL not normalized")
	}
	key, err := searchKey("searxng")
	if err != nil || key != "synthetic-secret-only" {
		t.Fatal("search credential not restored")
	}
	if w = request(http.MethodPost, `{"provider":"searxng","url":"http://169.254.169.254","key":"bad"}`); w.Code != 400 {
		t.Fatal("metadata endpoint accepted")
	}
	if readSearchSettings() != settings {
		t.Fatal("invalid update changed configuration")
	}
	key, _ = searchKey("searxng")
	if key != "synthetic-secret-only" {
		t.Fatal("invalid update replaced credential")
	}
	if w = request(http.MethodPost, `{"provider":"searxng","url":"http://127.0.0.1:8080","key":""}`); w.Code != 200 {
		t.Fatal("forget failed")
	}
	key, err = searchKey("searxng")
	if err != nil || key != "" {
		t.Fatal("credential not forgotten")
	}
}
func TestSearchURLsAndCloudAllowlist(t *testing.T) {
	testHome(t)
	for _, raw := range []string{"file:///etc/passwd", "http://public.example/search", "https://u:p@example.test", "https://example.test/?key=fixture", "http://[fe80::1]"} {
		if _, err := validateSearchSettings(searchSettings{Provider: "searxng", URL: raw}); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	data, _ := json.Marshal((cloudWebSearch{}).Definitions())
	if !bytes.Contains(data, []byte("web_search")) || bytes.Contains(data, []byte("web_open")) {
		t.Fatal("cloud tool scope expanded")
	}
	if _, err := (cloudWebSearch{}).Execute(t.Context(), "bash", map[string]any{}); err == nil {
		t.Fatal("unadvertised shell call accepted")
	}
}

func TestSearchEnabledWithoutPageRenderer(t *testing.T) {
	testHome(t)
	if err := setWebEngine(engineCrawl); err != nil {
		t.Fatal(err)
	}
	if err := setInternetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !globalCaps().Internet {
		t.Fatal("missing page renderer disabled independent search")
	}
	if err := setInternetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if globalCaps().Internet {
		t.Fatal("explicit opt-out ignored")
	}
}
