package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSSEEnvelopeFlushAndCache(t *testing.T) {
	for _, cache := range []string{"no-cache, no-transform", "no-store, no-transform"} {
		w := httptest.NewRecorder()
		SSEHeaders(w, cache)
		if err := WriteSSE(w, w, &sync.Mutex{}, []byte(`{"choices":[{"delta":{"text":"hello"}}]}`)); err != nil {
			t.Fatal(err)
		}
		if w.Body.String() != "data: {\"choices\":[{\"delta\":{\"text\":\"hello\"}}]}\n\n" || !w.Flushed || w.Header().Get("Content-Type") != "text/event-stream" || w.Header().Get("Cache-Control") != cache || w.Header().Get("X-Accel-Buffering") != "no" {
			t.Fatalf("SSE wire changed: %v %q", w.Header(), w.Body.String())
		}
	}
	w := &failedWriter{header: http.Header{}}
	if WriteSSE(w, w, &sync.Mutex{}, nil) != w.err || w.flushed {
		t.Fatal("failed writes must not flush")
	}
}

type failedWriter struct {
	header  http.Header
	err     error
	flushed bool
}

func (w *failedWriter) Header() http.Header { return w.header }
func (w *failedWriter) WriteHeader(int)     {}
func (w *failedWriter) Write([]byte) (int, error) {
	if w.err == nil {
		w.err = errors.New("disconnected")
	}
	return 0, w.err
}
func (w *failedWriter) Flush() { w.flushed = true }

func TestSSEHeartbeatSharesWriterLock(t *testing.T) {
	w := httptest.NewRecorder()
	mu, stop := SSEHeartbeat(w, w)
	defer stop()
	if err := WriteSSE(w, w, mu, []byte(`{"caught_up":true}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("missing four-second heartbeat")
		case <-tick.C:
			mu.Lock()
			body := w.Body.String()
			mu.Unlock()
			if body == "data: {\"caught_up\":true}\n\n: ping\n\n" {
				return
			}
		}
	}
}
