package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Assets serves the public UI from supplied filesystems and brand icon bytes.
// It owns no conversation data, application state or mutable package globals.
type Assets struct {
	UI     fs.FS
	NextFS fs.FS
	Icons  func() map[string][]byte
}

func nextContentType(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

func (a Assets) Next(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/next")), "/")
	name := "ui/next/" + rel
	if rel == "" || rel == "." {
		name = "ui/next/index.html"
	}
	b, err := fs.ReadFile(a.NextFS, name)
	ext := path.Ext(name)
	if err != nil || nextContentType(ext) == "" {
		// Routes de l'application (#/… n'arrive pas ici, mais /next/chat oui).
		if ext != "" {
			http.NotFound(w, r)
			return
		}
		b, err = fs.ReadFile(a.NextFS, "ui/next/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ext = ".html"
	}
	w.Header().Set("Content-Type", nextContentType(ext))
	if ext == ".html" {
		// Developer previews have their own origin and must not embed the
		// control plane to trick the owner into interacting with it.
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		_, _ = w.Write(b)
	}
}

// handleIndex sert l'interface à la racine.
func (a Assets) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/next/"
	a.Next(w, r2)
}

func (a Assets) RegisterPWA(mux *http.ServeMux) {
	for path, b := range a.Icons() {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		})
	}
	// Polices Geist (OFL) embarquées : Loom reste utilisable hors ligne et
	// n'appelle aucun CDN de polices.
	for path, file := range map[string]string{"/fonts/geist.woff2": "ui/fonts/geist.woff2", "/fonts/geist-mono.woff2": "ui/fonts/geist-mono.woff2"} {
		file := file
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			b, err := fs.ReadFile(a.UI, file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "font/woff2")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		})
	}
	mux.HandleFunc("/offline.html", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/offline.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(b)
	})
}

// Register installs every historical public UI route and its cache policy.
func (a Assets) Register(mux *http.ServeMux) {
	// Pages publiques : le HTML et le JS ne contiennent aucun secret. Toute la
	// donnée et toutes les actions passent par /api/* qui, lui, exige la clé.
	mux.HandleFunc("/", a.Index)
	mux.HandleFunc("/next/", a.Next)
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/next/", http.StatusFound) })
	a.RegisterPWA(mux)
	mux.HandleFunc("/marked.min.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/marked.min.js")
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(b)
	})
	// Service worker et manifeste PWA, avec notifications Web Push (push.go / sw.js).
	// PUBLICS (aucun secret) et servis en clair à la RACINE : un service worker doit
	// venir de l'origine même, et son scope est celui de son URL. no-store sur le SW
	// pour qu'une mise à jour du worker soit toujours reprise (pas de cache figé).
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/sw.js")
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Write(b)
	})
	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/manifest.webmanifest")
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(b)
	})
}
