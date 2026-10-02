package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testAssets() Assets {
	return Assets{
		UI: fstest.MapFS{
			"ui/marked.min.js":          {Data: []byte("marked")},
			"ui/sw.js":                  {Data: []byte("worker")},
			"ui/manifest.webmanifest":   {Data: []byte("manifest")},
			"ui/offline.html":           {Data: []byte("offline")},
			"ui/fonts/geist.woff2":      {Data: []byte("geist")},
			"ui/fonts/geist-mono.woff2": {Data: []byte("mono")},
		},
		NextFS: fstest.MapFS{
			"ui/next/index.html":   {Data: []byte("index")},
			"ui/next/app.js":       {Data: []byte("script")},
			"ui/next/app.mjs":      {Data: []byte("module")},
			"ui/next/app.css":      {Data: []byte("style")},
			"ui/next/logo.svg":     {Data: []byte("logo")},
			"ui/next/note.txt":     {Data: []byte("note")},
			"ui/next/private.json": {Data: []byte("hidden")},
		},
		Icons: func() map[string][]byte {
			return map[string][]byte{
				"/icons/loom-180.png": []byte("180"),
				"/icons/loom-192.png": []byte("192"),
				"/icons/loom-512.png": []byte("512"),
			}
		},
	}
}

func TestPublicRouteWireAndCachePolicies(t *testing.T) {
	mux := http.NewServeMux()
	testAssets().Register(mux)
	for _, tc := range []struct {
		path, body, contentType, cache string
		guarded                        bool
	}{
		{"/", "index", "text/html; charset=utf-8", "no-cache", true},
		{"/index.html", "index", "text/html; charset=utf-8", "no-cache", true},
		{"/next/", "index", "text/html; charset=utf-8", "no-cache", true},
		{"/next/chat", "index", "text/html; charset=utf-8", "no-cache", true},
		{"/next/app.js", "script", "text/javascript; charset=utf-8", "no-cache", true},
		{"/next/app.mjs", "module", "text/javascript; charset=utf-8", "no-cache", true},
		{"/next/app.css", "style", "text/css; charset=utf-8", "no-cache", true},
		{"/next/logo.svg", "logo", "image/svg+xml", "no-cache", true},
		{"/next/note.txt", "note", "text/plain; charset=utf-8", "no-cache", true},
		{"/icons/loom-180.png", "180", "image/png", "public, max-age=86400", true},
		{"/icons/loom-192.png", "192", "image/png", "public, max-age=86400", true},
		{"/icons/loom-512.png", "512", "image/png", "public, max-age=86400", true},
		{"/fonts/geist.woff2", "geist", "font/woff2", "public, max-age=31536000, immutable", true},
		{"/fonts/geist-mono.woff2", "mono", "font/woff2", "public, max-age=31536000, immutable", true},
		{"/marked.min.js", "marked", "application/javascript", "public, max-age=86400", false},
		{"/sw.js", "worker", "application/javascript", "no-store, max-age=0", false},
		{"/manifest.webmanifest", "manifest", "application/manifest+json", "public, max-age=3600", false},
		{"/offline.html", "offline", "text/html; charset=utf-8", "public, max-age=3600", false},
	} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			t.Run(method+tc.path, func(t *testing.T) {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(method, tc.path, nil))
				if tc.guarded && method == "POST" {
					if w.Code != 405 || w.Body.Len() != 0 || w.Header().Get("Allow") != "" {
						t.Fatalf("method rejection: %d %v %q", w.Code, w.Header(), w.Body.String())
					}
					return
				}
				body := tc.body
				if tc.guarded && method == "HEAD" {
					body = ""
				}
				if w.Code != 200 || w.Body.String() != body || w.Header().Get("Content-Type") != tc.contentType || w.Header().Get("Cache-Control") != tc.cache {
					t.Fatalf("asset wire: %d %v %q", w.Code, w.Header(), w.Body.String())
				}
				if tc.guarded && w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("missing nosniff")
				}
				if tc.path == "/sw.js" && w.Header().Get("Service-Worker-Allowed") != "/" {
					t.Fatal("worker scope changed")
				}
			})
		}
	}
	for _, path := range []string{"/other", "/next/missing.js", "/next/private.json", "/next/../sw.js"} {
		w := httptest.NewRecorder()
		testAssets().Next(w, httptest.NewRequest("GET", path, nil))
		// /other is a SPA fallback when calling Next directly; root registration rejects it.
		if path == "/other" {
			continue
		}
		if w.Code != 404 {
			t.Fatalf("unexpected file disclosure: %s %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 404 {
		t.Fatal("root fallback changed")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/next", nil))
	if w.Code != 302 || w.Header().Get("Location") != "/next/" {
		t.Fatalf("redirect changed: %d %v", w.Code, w.Header())
	}
}

func TestAssetOwnersAndMissingFiles(t *testing.T) {
	a, b := testAssets(), testAssets()
	b.NextFS = fstest.MapFS{}
	for _, tc := range []struct {
		assets Assets
		path   string
		status int
	}{
		{a, "/next/chat", 200}, {b, "/next/chat", 404}, {b, "/next/index.html", 404},
	} {
		w := httptest.NewRecorder()
		tc.assets.Next(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("filesystem isolation: %d", w.Code)
		}
	}
	b.UI = fstest.MapFS{}
	mux := http.NewServeMux()
	b.Register(mux)
	for _, path := range []string{"/fonts/geist.woff2", "/offline.html", "/marked.min.js", "/sw.js", "/manifest.webmanifest"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		want := 200 // Legacy public assets ignore read errors; fonts return 404.
		if path == "/fonts/geist.woff2" {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("missing %s: %d", path, w.Code)
		}
	}
}
