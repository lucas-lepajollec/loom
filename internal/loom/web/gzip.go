package web

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const gzipMinBytes = 512

var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

func acceptsGzip(r *http.Request) bool {
	ae := r.Header.Get("Accept-Encoding")
	if ae == "" {
		return false
	}
	for _, part := range strings.Split(ae, ",") {
		enc, params, _ := strings.Cut(part, ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			if strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0") &&
				!strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0.") {
				return false
			}
			return true
		}
	}
	return false
}

func compressibleContentType(ct string) bool {
	if ct == "" {
		return true
	}
	media, _, _ := strings.Cut(ct, ";")
	media = strings.ToLower(strings.TrimSpace(media))
	if media == "text/event-stream" {
		return false
	}
	if strings.HasPrefix(media, "text/") {
		return true
	}
	switch media {
	case "application/json", "application/javascript", "application/manifest+json",
		"application/xml", "application/xhtml+xml", "image/svg+xml":
		return true
	}
	return strings.HasSuffix(media, "+json") || strings.HasSuffix(media, "+xml")
}

// WithCompression compresses eligible HTTP responses with gzip when requested
// by the client. Streaming SSE responses, WebSocket upgrades, already-encoded
// payloads, binary media and tiny responses bypass compression cleanly.
func WithCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" || r.Header.Get("Upgrade") != "" || !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	buf         []byte
	status      int
	decided     bool
	compressing bool
	wroteHeader bool
}

func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.status = code
	g.wroteHeader = true
	if code < 200 || code == http.StatusNoContent || code == http.StatusNotModified ||
		g.Header().Get("Content-Encoding") != "" ||
		!compressibleContentType(g.Header().Get("Content-Type")) {
		g.decided = true
		g.compressing = false
		g.ResponseWriter.WriteHeader(code)
	}
}

func (g *gzipResponseWriter) start(compress bool) {
	g.decided = true
	g.compressing = compress
	code := g.status
	if code == 0 {
		code = http.StatusOK
	}
	if compress {
		g.Header().Del("Content-Length")
		g.Header().Set("Content-Encoding", "gzip")
		if etag := g.Header().Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
			g.Header().Set("ETag", "W/"+etag)
		}
		g.ResponseWriter.WriteHeader(code)
		zw := gzipWriterPool.Get().(*gzip.Writer)
		zw.Reset(g.ResponseWriter)
		g.gz = zw
		return
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(p))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.decided {
		if g.compressing {
			return g.gz.Write(p)
		}
		return g.ResponseWriter.Write(p)
	}
	g.buf = append(g.buf, p...)
	if len(g.buf) < gzipMinBytes {
		return len(p), nil
	}
	g.start(true)
	_, err := g.gz.Write(g.buf)
	g.buf = nil
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (g *gzipResponseWriter) Flush() {
	if !g.decided {
		// Streaming responses (SSE, chunked progress) flush early: bypass gzip
		// buffering so chunks reach the browser immediately.
		g.start(false)
		if len(g.buf) > 0 {
			_, _ = g.ResponseWriter.Write(g.buf)
			g.buf = nil
		}
	} else if g.compressing && g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := g.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (g *gzipResponseWriter) close() {
	if g.compressing && g.gz != nil {
		_ = g.gz.Close()
		g.gz.Reset(io.Discard)
		gzipWriterPool.Put(g.gz)
		g.gz = nil
		return
	}
	if !g.decided && (g.wroteHeader || len(g.buf) > 0) {
		g.start(false)
		if len(g.buf) > 0 {
			_, _ = g.ResponseWriter.Write(g.buf)
			g.buf = nil
		}
	}
}
