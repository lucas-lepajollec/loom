package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithCompressionCompressesEligiblePayloads(t *testing.T) {
	payload := strings.Repeat(`{"role":"assistant","text":"hello loom performance"},`, 40)
	h := WithCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(payload))
	}))

	// Without Accept-Encoding: gzip -> raw body.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/test", nil))
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != payload {
		t.Fatalf("unexpected uncompressed response: enc=%q len=%d", w.Header().Get("Content-Encoding"), w.Body.Len())
	}

	// With Accept-Encoding: gzip -> compressed smaller payload, decodes identically.
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip Content-Encoding, got %q", w.Header().Get("Content-Encoding"))
	}
	if w.Body.Len() >= len(payload)/2 {
		t.Fatalf("expected substantial compression: raw=%d gzip=%d", len(payload), w.Body.Len())
	}
	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil || string(got) != payload {
		t.Fatalf("roundtrip mismatch: err=%v len=%d", err, len(got))
	}
}

func TestWithCompressionSkipsSSEBinaryAndSmallResponses(t *testing.T) {
	// SSE must stream uncompressed immediately.
	sse := WithCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + strings.Repeat("a", 1024) + "\n\n"))
		w.(http.Flusher).Flush()
	}))
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	sse.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "" || !w.Flushed {
		t.Fatalf("SSE should bypass gzip and flush: enc=%q flushed=%v", w.Header().Get("Content-Encoding"), w.Flushed)
	}

	// Binary fonts must bypass gzip.
	font := WithCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "font/woff2")
		w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	w = httptest.NewRecorder()
	font.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("woff2 should bypass gzip, got %q", w.Header().Get("Content-Encoding"))
	}

	// Small payloads (< 512 B) bypass gzip.
	small := WithCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	w = httptest.NewRecorder()
	small.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != `{"ok":true}` {
		t.Fatalf("small payload should bypass gzip: enc=%q body=%q", w.Header().Get("Content-Encoding"), w.Body.String())
	}
}

func TestAssetsETagAndNotModified(t *testing.T) {
	mux := http.NewServeMux()
	testAssets().Register(mux)

	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, httptest.NewRequest("GET", "/next/app.js", nil))
	etag := w1.Header().Get("ETag")
	if w1.Code != 200 || etag == "" {
		t.Fatalf("expected 200 with ETag, got %d %q", w1.Code, etag)
	}

	req2 := httptest.NewRequest("GET", "/next/app.js", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified || w2.Body.Len() != 0 {
		t.Fatalf("expected 304 empty body on matching ETag, got %d len=%d", w2.Code, w2.Body.Len())
	}

	// Weak ETag from gzip middleware also matches on revalidation.
	req3 := httptest.NewRequest("GET", "/next/app.js", nil)
	req3.Header.Set("If-None-Match", "W/"+etag)
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNotModified {
		t.Fatalf("expected 304 on weak ETag match, got %d", w3.Code)
	}
}
