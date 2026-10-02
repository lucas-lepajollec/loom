package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlJSONWireAndMethodGuard(t *testing.T) {
	w := httptest.NewRecorder()
	SendJSON(w, 202, map[string]any{"html": "<>&", "missing": nil, "empty": []string{}})
	if w.Code != 202 || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != "{\"empty\":[],\"html\":\"\\u003c\\u003e\\u0026\",\"missing\":null}\n" {
		t.Fatalf("JSON wire changed: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	for _, method := range []string{"GET", "POST", "HEAD"} {
		w := httptest.NewRecorder()
		ok := RequireMethod(w, httptest.NewRequest(method, "/api/test", nil), "POST")
		if ok != (method == "POST") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("method guard: %s %v %v", method, ok, w.Header())
		}
		if !ok && (w.Code != 405 || w.Header().Get("Allow") != "POST" || w.Body.String() != "{\"error\":\"method not allowed\",\"ok\":false}\n") {
			t.Fatalf("method rejection changed: %d %v %q", w.Code, w.Header(), w.Body.String())
		}
	}
}

func TestStrictJSONLimitsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
		errorText               string
	}{
		{"valid", "application/json; charset=utf-8", `{"text":"hello"}`, 200, ""},
		{"missing type", "", `{}`, 415, "Content-Type application/json required"},
		{"wrong type", "text/plain", `{}`, 415, "Content-Type application/json required"},
		// Historical decoding ignores parameter parse errors when the media type is recognized.
		{"malformed type", "application/json; broken", `{}`, 200, ""},
		{"unknown field", "application/json", `{"unknown":1}`, 400, "invalid or oversized request"},
		{"empty", "application/json", "", 400, "invalid or oversized request"},
		{"malformed", "application/json", `{"text":`, 400, "invalid or oversized request"},
		{"trailing object", "application/json", `{} {}`, 400, "expected a single JSON request"},
		{"trailing junk", "application/json", `{} broken`, 400, "expected a single JSON request"},
		{"null", "application/json", `null`, 200, ""},
		{"at limit", "application/json", `{"text":"` + strings.Repeat("a", (128<<10)-11) + `"}`, 200, ""},
		{"over limit", "application/json", `{"text":"` + strings.Repeat("a", (128<<10)-10) + `"}`, 400, "invalid or oversized request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/test", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			var target struct {
				Text string `json:"text"`
			}
			ok := DecodeJSON(w, r, &target)
			if ok != (tc.status == 200) || w.Code != tc.status {
				t.Fatalf("decode: ok=%v code=%d body=%q", ok, w.Code, w.Body.String())
			}
			if !ok && (w.Header().Get("Content-Type") != "application/json" || w.Body.String() != "{\"error\":\""+tc.errorText+"\",\"ok\":false}\n") {
				t.Fatalf("error wire changed: %v %q", w.Header(), w.Body.String())
			}
		})
	}
}
