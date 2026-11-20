package loom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGlobalPreferencesHTTPAuthAndPageBounds(t *testing.T) {
	testHome(t)
	if err := storeWebKey("fixture-control"); err != nil {
		t.Fatal(err)
	}
	if err := MemSave("preferences", "", "Use clear answers."); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	webAPI(mux)("/api/context/preferences", handleSharedPreferences)
	call := func(method, body, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/context/preferences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	for _, method := range []string{"GET", "POST"} {
		if w := call(method, `{}`, ""); w.Code != 401 {
			t.Fatalf("unprotected preference policy: %d", w.Code)
		}
	}
	for _, body := range []string{`{"page":"../outside"}`, `{"page":"missing"}`, `{"unknown":true}`} {
		if w := call("POST", body, "fixture-control"); w.Code != 400 {
			t.Fatalf("invalid selection accepted: %s %d", body, w.Code)
		}
	}
	if w := call("POST", `{"page":"preferences","external":false}`, "fixture-control"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "", "fixture-control"); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"page":"preferences"`) {
		t.Fatal("selection missing or cacheable", w.Body.String())
	}
}
